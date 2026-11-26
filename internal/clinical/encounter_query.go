package clinical

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

// Clinical aggregate reads share a repeatable snapshot, as in the reference service.
func (s *Service) readSnapshot(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.beginClinicalTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() {
		if err := rollbackClinicalTx(ctx, tx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Warn("clinical read rollback failed")
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func dashboardAge(birth, today time.Time) string {
	months := ageMonths(birth, today)
	if months == nil {
		return ""
	}
	years, remainder := *months/12, *months%12
	if years == 0 {
		if *months <= 1 {
			return "1 mes"
		}
		return fmt.Sprintf("%d meses", *months)
	}
	label := fmt.Sprintf("%d años", years)
	if years == 1 {
		label = "1 año"
	}
	if years < 3 && remainder > 0 {
		label += fmt.Sprintf(", %d m", remainder)
	}
	return label
}

type recentEncounter struct {
	Encounter  EncounterSummary `json:"encounter"`
	Age        string           `json:"patientAgeLabel"`
	NationalID *string          `json:"patientNationalId"`
	Date       string           `json:"encounterLocalDate"`
}

type dashboardPatient struct {
	ID       int64  `json:"patientId"`
	FullName string `json:"patientFullName"`
}

type dashboardPatientMonth struct {
	Month    string `json:"month"`
	Patients int64  `json:"patients"`
}

type dashboardPatientActivity struct {
	MonthlyPatients          [3]dashboardPatientMonth `json:"monthlyPatients"`
	FirstVisitsThisMonth     int64                    `json:"firstVisitsThisMonth"`
	WithoutEncounters        int64                    `json:"withoutEncounters"`
	PatientsWithoutEncounter []dashboardPatient       `json:"patientsWithoutEncounter"`
}

type encounterDashboard struct {
	Patients        int64                    `json:"totalPatients"`
	Month           int64                    `json:"encountersThisMonth"`
	Today           int64                    `json:"encountersToday"`
	Date            string                   `json:"asOfDate"`
	Recent          []recentEncounter        `json:"recentEncounters"`
	Upcoming        []upcomingFollowUp       `json:"upcomingFollowUps"`
	PatientActivity dashboardPatientActivity `json:"patientActivity"`
}

const dashboardPatientScope = `p.user_id=$1 AND p.annulled_at IS NULL AND (
	p.specialty_context_id=$2 OR $2::bigint=0 AND (
		SELECT count(*) FROM doctor_specialty_contexts scope_c
		JOIN doctor_workspaces scope_w ON scope_w.id=scope_c.workspace_id
		WHERE scope_w.user_id=$1
	)=1)`

func (activity *dashboardPatientActivity) readWithoutEncounters(ctx context.Context, tx pgx.Tx, owner string, specialtyContextID int64) error {
	rows, err := tx.Query(ctx, `SELECT p.id, p.full_name, count(*) OVER()
		FROM patients p WHERE `+dashboardPatientScope+`
		AND NOT EXISTS (
			SELECT 1 FROM clinical_encounters e WHERE e.patient_id=p.id AND e.annulled_at IS NULL
		)
		ORDER BY p.id DESC LIMIT 4`, owner, specialtyContextID)
	if err != nil {
		return err
	}
	defer rows.Close()
	activity.PatientsWithoutEncounter = []dashboardPatient{}
	for rows.Next() {
		var patient dashboardPatient
		if err := rows.Scan(&patient.ID, &patient.FullName, &activity.WithoutEncounters); err != nil {
			return err
		}
		activity.PatientsWithoutEncounter = append(activity.PatientsWithoutEncounter, patient)
	}
	return rows.Err()
}

func (s *Service) encounterQueryRoutes(mux *http.ServeMux) {
	s.handle(mux, "GET /api/doctorchart/encounters", func(w http.ResponseWriter, r *http.Request) {
		page, err := pageFor(r.URL.Query(), map[string]string{"encounterDate": "encounter_date", "id": "id"}, "e", "encounter_date", "DESC")
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		if err == nil && api.JavaLength(search) > 200 {
			err = listError("La búsqueda admite hasta 200 caracteres.")
		}
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search)) + "%"
		var result any
		if err == nil {
			err = s.readSnapshot(r.Context(), func(tx pgx.Tx) error {
				where := `p.user_id=$1 AND ($3::bigint=0 AND (SELECT count(*) FROM doctor_specialty_contexts scope_c JOIN doctor_workspaces scope_w ON scope_w.id=scope_c.workspace_id WHERE scope_w.user_id=$1)=1 OR p.specialty_context_id=$3) AND (lower(p.full_name) LIKE $2 ESCAPE '!' OR lower(e.chief_complaint) LIKE $2 ESCAPE '!' OR lower(e.primary_diagnosis_description) LIKE $2 ESCAPE '!' OR lower(e.primary_diagnosis_code) LIKE $2 ESCAPE '!')`
				var total int64
				args := []any{api.User(r.Context()).UserID, pattern, api.SpecialtyContextID(r.Context())}
				if err := tx.QueryRow(r.Context(), "SELECT count(*)"+encounterFrom+"WHERE "+where, args...).Scan(&total); err != nil {
					return err
				}
				args = append(args, page.size, page.number*page.size)
				content, err := s.encounterList(r.Context(), tx, where, args, page.order+" LIMIT $4 OFFSET $5")
				if err != nil {
					return err
				}
				result = map[string]any{"content": content, "page": map[string]any{"size": page.size, "number": page.number, "totalElements": total, "totalPages": (total + page.size - 1) / page.size}}
				return nil
			})
		}
		clinicalReply(w, result, err, 200)
	})
	s.handle(mux, "GET /api/doctorchart/encounters/dashboard", func(w http.ResponseWriter, r *http.Request) {
		today := time.Now().In(s.viewZone(r.Context()))
		start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, s.viewZone(r.Context()))
		month := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, s.viewZone(r.Context()))
		previousMonth := month.AddDate(0, -1, 0)
		earliestMonth := month.AddDate(0, -2, 0)
		result := encounterDashboard{Date: today.Format("2006-01-02"), Recent: []recentEncounter{}}
		activity := &result.PatientActivity
		activity.MonthlyPatients = [3]dashboardPatientMonth{
			{Month: earliestMonth.Format("2006-01")},
			{Month: previousMonth.Format("2006-01")},
			{Month: month.Format("2006-01")},
		}
		err := s.readSnapshot(r.Context(), func(tx pgx.Tx) error {
			owner := api.User(r.Context()).UserID
			specialtyContextID := api.SpecialtyContextID(r.Context())
			if err := tx.QueryRow(r.Context(), `SELECT
				(SELECT count(*) FROM patients p WHERE `+dashboardPatientScope+`),
				count(*) FILTER (WHERE e.encounter_date >= $3 AND e.encounter_date < $4),
				count(*) FILTER (WHERE e.encounter_date >= $5 AND e.encounter_date < $6),
				count(DISTINCT e.patient_id) FILTER (WHERE e.encounter_date >= $7 AND e.encounter_date < $8),
				count(DISTINCT e.patient_id) FILTER (WHERE e.encounter_date >= $8 AND e.encounter_date < $3),
				count(DISTINCT e.patient_id) FILTER (WHERE e.encounter_date >= $3 AND e.encounter_date < $4),
				count(*) FILTER (WHERE e.encounter_date >= $3 AND e.encounter_date < $4 AND e.consultation_type='FIRST_VISIT')
				FROM clinical_encounters e JOIN patients p ON p.id=e.patient_id
				WHERE e.annulled_at IS NULL AND `+dashboardPatientScope+`
				AND e.encounter_date >= LEAST($7, $5) AND e.encounter_date < GREATEST($4, $6)`,
				owner, specialtyContextID, month, month.AddDate(0, 1, 0), start, start.AddDate(0, 0, 1), earliestMonth, previousMonth,
			).Scan(&result.Patients, &result.Month, &result.Today,
				&activity.MonthlyPatients[0].Patients, &activity.MonthlyPatients[1].Patients,
				&activity.MonthlyPatients[2].Patients, &activity.FirstVisitsThisMonth); err != nil {
				return err
			}
			if err := activity.readWithoutEncounters(r.Context(), tx, owner, specialtyContextID); err != nil {
				return err
			}
			var err error
			result.Upcoming, err = dashboardFollowUps(r.Context(), tx, owner, result.Date, start.AddDate(0, 0, 3).Format("2006-01-02"))
			if err != nil {
				return err
			}
			// Read the additional patient fields in the same query and snapshot.
			rows, err := tx.Query(r.Context(), "SELECT "+encounterColumns+`,p.birth_date::text,p.national_id`+encounterFrom+"WHERE "+dashboardPatientScope+" ORDER BY e.encounter_date DESC,e.id DESC LIMIT 8", owner, specialtyContextID)
			if err != nil {
				return err
			}
			defer rows.Close()
			day, err := time.Parse("2006-01-02", result.Date)
			if err != nil {
				return err
			}
			for rows.Next() {
				var birthText string
				var national *string
				e, err := scanEncounter(rows, &birthText, &national)
				if err != nil {
					return err
				}
				birth, err := time.Parse("2006-01-02", birthText)
				if err != nil {
					return err
				}
				result.Recent = append(result.Recent, recentEncounter{e.EncounterSummary, dashboardAge(birth, day), national, e.instant.In(s.viewZone(r.Context())).Format("2006-01-02")})
			}
			return rows.Err()
		})
		clinicalReply(w, result, err, 200)
	})
}
