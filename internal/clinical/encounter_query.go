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
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
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
type encounterDashboard struct {
	Patients int64             `json:"totalPatients"`
	Month    int64             `json:"encountersThisMonth"`
	Today    int64             `json:"encountersToday"`
	Date     string            `json:"asOfDate"`
	Recent   []recentEncounter `json:"recentEncounters"`
}

func (s *Service) encounterQueryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/doctorchart/encounters", func(w http.ResponseWriter, r *http.Request) {
		page, err := pageFor(r.URL.Query(), map[string]string{"encounterDate": "encounter_date", "id": "id"}, "e", "encounter_date", "DESC")
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		if err == nil && api.JavaLength(search) > 200 {
			err = listError("La búsqueda admite hasta 200 caracteres.")
		}
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search)) + "%"
		var result any
		if err == nil {
			err = s.readSnapshot(r.Context(), func(tx pgx.Tx) error {
				where := `p.user_id=$1 AND (lower(p.full_name) LIKE $2 ESCAPE '!' OR lower(e.chief_complaint) LIKE $2 ESCAPE '!' OR lower(e.primary_diagnosis_description) LIKE $2 ESCAPE '!' OR lower(e.primary_diagnosis_code) LIKE $2 ESCAPE '!')`
				var total int64
				args := []any{api.User(r.Context()).UserID, pattern}
				if err := tx.QueryRow(r.Context(), "SELECT count(*)"+encounterFrom+"WHERE "+where, args...).Scan(&total); err != nil {
					return err
				}
				args = append(args, page.size, page.number*page.size)
				content, err := s.encounterList(r.Context(), tx, where, args, page.order+" LIMIT $3 OFFSET $4")
				if err != nil {
					return err
				}
				result = map[string]any{"content": content, "page": map[string]any{"size": page.size, "number": page.number, "totalElements": total, "totalPages": (total + page.size - 1) / page.size}}
				return nil
			})
		}
		clinicalReply(w, result, err, 200)
	})
	mux.HandleFunc("GET /api/doctorchart/encounters/dashboard", func(w http.ResponseWriter, r *http.Request) {
		today := time.Now().In(s.viewZone(r.Context()))
		start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, s.viewZone(r.Context()))
		month := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, s.viewZone(r.Context()))
		result := encounterDashboard{Date: today.Format("2006-01-02"), Recent: []recentEncounter{}}
		err := s.readSnapshot(r.Context(), func(tx pgx.Tx) error {
			owner := api.User(r.Context()).UserID
			if err := tx.QueryRow(r.Context(), `SELECT (SELECT count(*) FROM patients WHERE user_id=$1),count(*) FILTER(WHERE e.encounter_date>=$2 AND e.encounter_date<$3),count(*) FILTER(WHERE e.encounter_date>=$4 AND e.encounter_date<$5) FROM clinical_encounters e JOIN patients p ON p.id=e.patient_id WHERE p.user_id=$1`, owner, month, month.AddDate(0, 1, 0), start, start.AddDate(0, 0, 1)).Scan(&result.Patients, &result.Month, &result.Today); err != nil {
				return err
			}
			// Read the additional patient fields in the same query and snapshot.
			rows, err := tx.Query(r.Context(), "SELECT "+encounterColumns+`,p.birth_date::text,p.national_id`+encounterFrom+"WHERE p.user_id=$1 ORDER BY e.encounter_date DESC,e.id DESC LIMIT 8", owner)
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
