package clinical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type SummaryEncounter struct {
	ID           int64   `json:"id"`
	Date         string  `json:"date"`
	Complaint    *string `json:"complaint"`
	Diagnosis    *string `json:"diagnosis"`
	FollowUp     *string `json:"followUp"`
	FollowUpDate *string `json:"followUpDate"`
}

type ClinicalSummary struct {
	PatientID         int64               `json:"patientId"`
	PatientVersion    int64               `json:"patientVersion"`
	AsOf              string              `json:"asOfDate"`
	Profile           ClinicalProfile     `json:"profile"`
	Historical        []ClinicalEntry     `json:"historical"`
	LastEncounter     *SummaryEncounter   `json:"lastEncounter"`
	Vaccinations      *Schedule           `json:"vaccinations"`
	RecentMedications []SummaryMedication `json:"recentMedications"`
}

type SummaryMedication struct {
	RecordID  int64  `json:"recordId"`
	Date      string `json:"date"`
	Name      string `json:"name"`
	Dosage    string `json:"dosage"`
	Frequency string `json:"frequency"`
}

func (s *Service) ClinicalSummary(ctx context.Context, user string, id int64) (ClinicalSummary, error) {
	result := ClinicalSummary{}
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		p, err := ownedPatient(ctx, tx, user, id)
		if err != nil {
			return err
		}
		profile, _, err := readClinicalProfile(p.Personal)
		if err != nil {
			return err
		}
		historical, err := historicalClinicalEntries(p.Allergies, p.Chronic, p.Personal)
		if err != nil {
			return err
		}
		result = ClinicalSummary{PatientID: id, PatientVersion: p.Version, AsOf: time.Now().In(s.viewZone(ctx)).Format("2006-01-02"), Profile: profile, Historical: historical, RecentMedications: []SummaryMedication{}}
		var encounter SummaryEncounter
		var date time.Time
		err = tx.QueryRow(ctx, `SELECT id,encounter_date,chief_complaint,primary_diagnosis_description,follow_up_instructions,follow_up_date::text FROM clinical_encounters WHERE patient_id=$1 ORDER BY encounter_date DESC,id DESC LIMIT 1`, id).Scan(&encounter.ID, &date, &encounter.Complaint, &encounter.Diagnosis, &encounter.FollowUp, &encounter.FollowUpDate)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			encounter.Date = date.In(s.viewZone(ctx)).Format("2006-01-02")
			result.LastEncounter = &encounter
		}
		if err := s.workspaces.Feature(ctx, tx, user, "PRESCRIPTIONS"); err == nil {
			rows, err := tx.Query(ctx, `SELECT r.id,r.created_at,m.medication_name,m.dosage,m.frequency FROM patient_prescription_records r JOIN prescription_medications m ON m.record_id=r.id WHERE r.patient_id=$1 AND r.author_user_id=$2 ORDER BY r.created_at DESC,r.id DESC,m.item_position LIMIT 8`, id, user)
			if err != nil {
				return err
			}
			for rows.Next() {
				var item SummaryMedication
				var date time.Time
				if err := rows.Scan(&item.RecordID, &date, &item.Name, &item.Dosage, &item.Frequency); err != nil {
					rows.Close()
					return err
				}
				item.Date = date.In(s.viewZone(ctx)).Format("2006-01-02")
				result.RecentMedications = append(result.RecentMedications, item)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		} else {
			var failure *api.Error
			if !errors.As(err, &failure) || failure.Code != "FEATURE_UNAVAILABLE" {
				return err
			}
		}
		// Summary obeys the same specialty availability as the vaccination module.
		if err := s.workspaces.Feature(ctx, tx, user, "PATIENT_VACCINATION"); err != nil {
			var failure *api.Error
			if errors.As(err, &failure) && failure.Code == "FEATURE_UNAVAILABLE" {
				return nil
			}
			return err
		}
		cal, err := loadCalendar(ctx, tx)
		if err != nil {
			return err
		}
		schedule, err := s.schedule(ctx, tx, p, cal)
		if err != nil {
			return err
		}
		result.Vaccinations = &schedule
		return nil
	})
	return result, err
}

func (s *Service) SaveClinicalProfile(ctx context.Context, user string, id int64, request map[string]any) (Patient, error) {
	var result Patient
	expected, err := api.Version(request, false)
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := lockPatient(ctx, tx, user, id); err != nil {
			return err
		}
		p, err := ownedPatient(ctx, tx, user, id)
		if err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, user, "EDIT_PATIENT"); err != nil {
			return err
		}
		if p.Version != *expected {
			return api.Failure(409, "VERSION_CONFLICT", "La ficha cambió. Actualizá antes de guardar.")
		}
		profile, personal, err := readClinicalProfile(p.Personal)
		if err != nil {
			return err
		}
		historical, err := historicalClinicalEntries(p.Allergies, p.Chronic, p.Personal)
		if err != nil {
			return err
		}
		now := time.Now()
		if err := updateClinicalProfile(&profile, historical, request, p.BirthDate, now.In(s.viewZone(ctx)).Format("2006-01-02"), now.UTC().Format(time.RFC3339Nano), user); err != nil {
			return err
		}
		personal["clinicalProfile"], err = json.Marshal(profile)
		if err != nil {
			return err
		}
		p.Personal, err = json.Marshal(personal)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `UPDATE patients SET personal_history=$2,version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND version=$3 RETURNING version`, id, p.Personal, *expected).Scan(&p.Version); err != nil {
			return err
		}
		if err := s.detail(ctx, tx, &p); err != nil {
			return err
		}
		result = p
		return nil
	})
	return result, err
}

func (s *Service) summaryRoutes(mux *http.ServeMux) {
	for _, method := range []string{"GET", "PUT"} {
		path := "/clinical-summary"
		if method == "PUT" {
			path = "/clinical-profile"
		}
		mux.HandleFunc(method+" /api/doctorchart/patients/{id}"+path, func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if err != nil || id <= 0 {
				api.WriteError(w, api.Invalid("El paciente no es válido."))
				return
			}
			if r.Method == "GET" {
				result, err := s.ClinicalSummary(r.Context(), api.User(r.Context()).UserID, id)
				if err != nil {
					api.WriteError(w, err)
					return
				}
				api.WriteJSON(w, 200, result)
				return
			}
			body, err := api.Decode(w, r)
			if err != nil {
				api.WriteError(w, err)
				return
			}
			result, err := s.SaveClinicalProfile(r.Context(), api.User(r.Context()).UserID, id, body)
			if err != nil {
				api.WriteError(w, err)
				return
			}
			api.WriteJSON(w, 200, result)
		})
	}
}
