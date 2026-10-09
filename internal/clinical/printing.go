package clinical

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type printPatient struct {
	ID         int64           `json:"id"`
	Name       string          `json:"fullName"`
	NationalID *string         `json:"nationalId"`
	BirthDate  string          `json:"birthDate"`
	Age        *string         `json:"ageAtIssue"`
	Insurance  json.RawMessage `json:"insuranceInfo"`
}
type printAuthor struct {
	UserID    string  `json:"userId"`
	Specialty *string `json:"specialtyName"`
}
type printDocument struct {
	Source        string          `json:"source"`
	ID            int64           `json:"id"`
	Code          string          `json:"code"`
	Issued        string          `json:"issuedAt"`
	Type          string          `json:"type"`
	Patient       printPatient    `json:"patient"`
	Author        printAuthor     `json:"author"`
	DiagnosisCode *string         `json:"primaryDiagnosisCode"`
	Diagnosis     *string         `json:"primaryDiagnosisDescription"`
	Instructions  *string         `json:"patientInstructions"`
	Medications   json.RawMessage `json:"prescriptions"`
	Orders        []string        `json:"diagnosticOrders"`
	at            time.Time
}

func printAge(birth, reference time.Time) *string {
	months := ageMonths(birth, reference)
	if months == nil {
		return nil
	}
	var result string
	if *months == 0 {
		result = fmt.Sprintf("%d días", int(reference.Sub(birth).Hours()/24))
	} else if *months < 24 {
		result = fmt.Sprintf("%d meses", *months)
	} else {
		result = fmt.Sprintf("%d años", *months/12)
		if *months%12 != 0 {
			result += fmt.Sprintf(" %dm", *months%12)
		}
	}
	return &result
}
func (s *Service) printable(ctx context.Context, user string, patientID, id int64, encounter bool) (printDocument, error) {
	var doc printDocument
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		patient, err := ownedPatient(ctx, tx, user, patientID)
		if err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, user, "PRINT_DOCUMENT"); err != nil {
			return err
		}
		if encounter {
			record, err := scanEncounter(tx.QueryRow(ctx, "SELECT "+encounterColumns+encounterFrom+"WHERE e.id=$1 AND e.patient_id=$2", id, patientID))
			if errors.Is(err, pgx.ErrNoRows) {
				return api.Failure(404, "ENCOUNTER_NOT_FOUND", "No se encontró el documento del paciente.")
			}
			if err != nil {
				return err
			}
			doc.Source = "ENCOUNTER"
			doc.ID = record.ID
			doc.Code = fmt.Sprintf("HC-%d-%d", id, patientID)
			doc.at = record.instant
			doc.Author = printAuthor{UserID: patient.owner, Specialty: &record.Specialty.Name}
			doc.DiagnosisCode = record.DiagnosisCode
			doc.Diagnosis = record.Diagnosis
			doc.Instructions = record.FollowUp
			if len(record.Prescriptions) == 0 || string(record.Prescriptions) == "null" {
				doc.Medications = json.RawMessage("[]")
			} else {
				var medications []map[string]any
				if err := json.Unmarshal(record.Prescriptions, &medications); err != nil {
					return err
				}
				normalized := []map[string]any{}
				for _, item := range medications {
					fields := map[string]any{}
					for _, key := range []string{"medicationName", "dosage", "administrationRoute", "frequency", "durationDays", "instructions"} {
						fields[key] = item[key]
					}
					normalized = append(normalized, fields)
				}
				doc.Medications, err = json.Marshal(normalized)
				if err != nil {
					return err
				}
			}
			doc.Orders = []string{}
			if len(record.Orders) > 0 && string(record.Orders) != "null" {
				if err := json.Unmarshal(record.Orders, &doc.Orders); err != nil {
					return err
				}
			}
			doc.Type = "PRESCRIPTION"
			if string(doc.Medications) == "[]" {
				doc.Type = "STUDY_ORDERS"
			}
		} else {
			if err := s.workspaces.Feature(ctx, tx, user, "PRESCRIPTIONS"); err != nil {
				return err
			}
			record, err := ownedPrescription(ctx, tx, user, patientID, id)
			var failure *api.Error
			if errors.As(err, &failure) && failure.Code == "PRESCRIPTION_NOT_FOUND" {
				return api.Failure(404, "PRESCRIPTION_NOT_FOUND", "No se encontró el documento del paciente.")
			}
			if err != nil {
				return err
			}
			doc.Source = "PRESCRIPTION_RECORD"
			doc.ID = record.ID
			doc.Code = fmt.Sprintf("RX-%d-%d", id, patientID)
			doc.at, err = time.Parse(time.RFC3339Nano, record.Created)
			if err != nil {
				return err
			}
			doc.Type = record.Type
			doc.Diagnosis = &record.Diagnosis
			doc.Instructions = &record.Instructions
			doc.Orders = record.Orders
			doc.Author.UserID = record.Author
			var specialty string
			err = tx.QueryRow(ctx, "SELECT s.display_name FROM doctor_workspaces w JOIN specialties s ON s.id=w.specialty_id WHERE w.user_id=$1", record.Author).Scan(&specialty)
			if err == nil {
				doc.Author.Specialty = &specialty
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			doc.Medications, err = json.Marshal(record.Medications)
			if err != nil {
				return err
			}
		}
		if string(doc.Medications) == "[]" && len(doc.Orders) == 0 {
			return api.Failure(400, "EMPTY_DOCUMENT", "El documento no tiene medicamentos ni estudios registrados para imprimir.")
		}
		if err := s.detail(ctx, tx, &patient); err != nil {
			return err
		}
		birth, err := time.Parse("2006-01-02", patient.BirthDate)
		if err != nil {
			return err
		}
		local := doc.at.In(s.zone)
		reference := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
		doc.Patient = printPatient{patient.ID, patient.FullName, patient.NationalID, patient.BirthDate, printAge(birth, reference), patient.Insurance}
		doc.Issued = api.FormatInstant(doc.at)
		return nil
	})
	return doc, err
}
func (s *Service) PrintRoutes(mux *http.ServeMux) {
	handler := func(encounter, pdf bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var err error
			if !encounter {
				err = prescriptionRole(r)
			}
			patientID, parseErr := pathID(r, "patientId")
			if err == nil {
				err = parseErr
			}
			id, parseErr := pathID(r, "recordId")
			if err == nil {
				err = parseErr
			}
			var doc printDocument
			if err == nil {
				doc, err = s.printable(r.Context(), api.User(r.Context()).UserID, patientID, id, encounter)
			}
			if err != nil {
				api.WriteError(w, err)
				return
			}
			if !pdf {
				api.WriteJSON(w, 200, doc)
				return
			}
			result, err := s.printPDF(doc, r.URL.Query())
			if err != nil {
				api.WriteError(w, err)
				return
			}
			filename := fmt.Sprintf("receta-%d.pdf", id)
			if encounter {
				filename = fmt.Sprintf("orden-consulta-%d.pdf", id)
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", "inline; filename=\""+filename+"\"")
			w.Header().Set("Cache-Control", "private, no-cache, no-store, max-age=0, must-revalidate")
			w.Header().Set("Content-Length", fmt.Sprint(len(result)))
			w.WriteHeader(200)
			if _, err := w.Write(result); err != nil {
				return
			}
		}
	}
	mux.HandleFunc("GET /api/doctorchart/patients/{patientId}/prescriptions/{recordId}/print", handler(false, false))
	mux.HandleFunc("GET /api/doctorchart/patients/{patientId}/prescriptions/{recordId}/pdf", handler(false, true))
	mux.HandleFunc("GET /api/doctorchart/patients/{patientId}/encounters/{recordId}/print", handler(true, false))
	mux.HandleFunc("GET /api/doctorchart/patients/{patientId}/encounters/{recordId}/pdf", handler(true, true))
}
