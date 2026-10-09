package clinical

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type PrescriptionItem struct {
	Name         string `json:"medicationName"`
	Dosage       string `json:"dosage"`
	Route        string `json:"administrationRoute"`
	Frequency    string `json:"frequency"`
	Days         *int64 `json:"durationDays"`
	Instructions string `json:"instructions"`
}
type Prescription struct {
	ID           int64              `json:"id"`
	PatientID    int64              `json:"patientId"`
	Author       string             `json:"authorUserId"`
	Created      string             `json:"createdAt"`
	Type         string             `json:"type"`
	Diagnosis    string             `json:"diagnosisDescription"`
	Instructions string             `json:"generalInstructions"`
	Medications  []PrescriptionItem `json:"prescriptions"`
	Orders       []string           `json:"diagnosticOrders"`
}

func prescriptionText(body map[string]any, key string, limit int) (string, error) {
	value, err := api.OptionalText(body, key)
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", nil
	}
	if api.JavaLength(*value) > limit {
		return "", api.Invalid("El campo " + key + " supera el tamaño permitido.")
	}
	return api.JavaTrim(*value), nil
}
func prescriptionList(body map[string]any, key string) ([]any, error) {
	values, ok := body[key].([]any)
	if !ok || len(values) > 100 {
		return nil, api.Invalid("El campo " + key + " debe ser una lista de hasta 100 elementos.")
	}
	return values, nil
}
func parsePrescription(body map[string]any) (Prescription, error) {
	p := Prescription{Medications: []PrescriptionItem{}, Orders: []string{}}
	if err := api.Only(body, "type", "diagnosisDescription", "generalInstructions", "prescriptions", "diagnosticOrders"); err != nil {
		return p, err
	}
	kind, err := api.Text(body, "type", 20)
	if err != nil {
		return p, err
	}
	if kind != "PRESCRIPTION" && kind != "STUDY_ORDERS" {
		return p, api.Invalid("El tipo de documento no es válido.")
	}
	p.Type = kind
	medications, err := prescriptionList(body, "prescriptions")
	if err != nil {
		return p, err
	}
	for _, raw := range medications {
		item, ok := raw.(map[string]any)
		if !ok {
			return p, api.Invalid("Cada medicamento debe ser un objeto JSON.")
		}
		if err := api.Only(item, "medicationName", "dosage", "administrationRoute", "frequency", "durationDays", "instructions"); err != nil {
			return p, err
		}
		medication := PrescriptionItem{}
		if item["durationDays"] != nil {
			days, err := api.Integer(item["durationDays"], "durationDays", 1)
			if err != nil {
				return p, err
			}
			if days > math.MaxInt32 {
				return p, api.Invalid("La duración debe ser un entero positivo válido.")
			}
			medication.Days = &days
		}
		for _, field := range []struct {
			key   string
			limit int
			dest  *string
		}{{"medicationName", 200, &medication.Name}, {"dosage", 500, &medication.Dosage}, {"administrationRoute", 200, &medication.Route}, {"frequency", 200, &medication.Frequency}} {
			value, err := api.Text(item, field.key, field.limit)
			if err != nil {
				return p, err
			}
			*field.dest = api.JavaTrim(value)
		}
		medication.Instructions, err = prescriptionText(item, "instructions", 4000)
		if err != nil {
			return p, err
		}
		p.Medications = append(p.Medications, medication)
	}
	orders, err := prescriptionList(body, "diagnosticOrders")
	if err != nil {
		return p, err
	}
	for _, raw := range orders {
		value, ok := raw.(string)
		if !ok || api.JavaBlank(value) || api.JavaLength(value) > 2000 {
			return p, api.Invalid("Cada estudio debe ser texto no vacío de hasta 2000 caracteres.")
		}
		p.Orders = append(p.Orders, api.JavaTrim(value))
	}
	if kind == "PRESCRIPTION" && len(p.Medications) == 0 || kind == "STUDY_ORDERS" && len(p.Orders) == 0 {
		return p, api.Invalid("El documento debe contener al menos un medicamento o estudio del tipo seleccionado.")
	}
	p.Diagnosis, err = prescriptionText(body, "diagnosisDescription", 4000)
	if err != nil {
		return p, err
	}
	p.Instructions, err = prescriptionText(body, "generalInstructions", 4000)
	return p, err
}

func prescriptionChildren(ctx context.Context, tx pgx.Tx, p *Prescription) error {
	p.Medications = []PrescriptionItem{}
	p.Orders = []string{}
	rows, err := tx.Query(ctx, `SELECT medication_name,dosage,administration_route,frequency,duration_days,instructions FROM prescription_medications WHERE record_id=$1 ORDER BY item_position`, p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m PrescriptionItem
		if err := rows.Scan(&m.Name, &m.Dosage, &m.Route, &m.Frequency, &m.Days, &m.Instructions); err != nil {
			rows.Close()
			return err
		}
		p.Medications = append(p.Medications, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	orders, err := tx.Query(ctx, `SELECT study_description FROM prescription_diagnostic_orders WHERE record_id=$1 ORDER BY item_position`, p.ID)
	if err != nil {
		return err
	}
	defer orders.Close()
	for orders.Next() {
		var item string
		if err := orders.Scan(&item); err != nil {
			return err
		}
		p.Orders = append(p.Orders, item)
	}
	return orders.Err()
}
func ownedPrescription(ctx context.Context, tx pgx.Tx, user string, patientID, id int64) (Prescription, error) {
	var p Prescription
	var date time.Time
	err := tx.QueryRow(ctx, `SELECT id,patient_id,author_user_id,created_at,type,diagnosis_description,general_instructions FROM patient_prescription_records WHERE id=$1 AND patient_id=$2 AND author_user_id=$3`, id, patientID, user).Scan(&p.ID, &p.PatientID, &p.Author, &date, &p.Type, &p.Diagnosis, &p.Instructions)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, api.Failure(404, "PRESCRIPTION_NOT_FOUND", "No se encontró la receta u orden.")
	}
	if err != nil {
		return p, err
	}
	p.Created = api.FormatInstant(date)
	err = prescriptionChildren(ctx, tx, &p)
	return p, err
}
func (s *Service) prescriptionAccess(ctx context.Context, tx pgx.Tx, user string, patientID int64) error {
	if _, err := ownedPatient(ctx, tx, user, patientID); err != nil {
		return err
	}
	return s.workspaces.Feature(ctx, tx, user, "PRESCRIPTIONS")
}
func (s *Service) CreatePrescription(ctx context.Context, user string, patientID int64, body map[string]any) (Prescription, error) {
	var result Prescription
	err := s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := s.prescriptionAccess(ctx, tx, user, patientID); err != nil {
			return err
		}
		p, err := parsePrescription(body)
		if err != nil {
			return err
		}
		date := time.Now().UTC()
		p.PatientID = patientID
		p.Author = user
		p.Created = api.FormatInstant(date)
		if err := tx.QueryRow(ctx, `INSERT INTO patient_prescription_records(patient_id,author_user_id,created_at,type,diagnosis_description,general_instructions) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, patientID, user, date, p.Type, p.Diagnosis, p.Instructions).Scan(&p.ID); err != nil {
			return err
		}
		for pos, m := range p.Medications {
			if _, err := tx.Exec(ctx, `INSERT INTO prescription_medications(record_id,item_position,medication_name,dosage,administration_route,frequency,duration_days,instructions) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.ID, pos, m.Name, m.Dosage, m.Route, m.Frequency, m.Days, m.Instructions); err != nil {
				return err
			}
		}
		for pos, order := range p.Orders {
			if _, err := tx.Exec(ctx, `INSERT INTO prescription_diagnostic_orders(record_id,item_position,study_description) VALUES($1,$2,$3)`, p.ID, pos, order); err != nil {
				return err
			}
		}
		result = p
		return nil
	})
	return result, err
}
func prescriptionRole(r *http.Request) error {
	id := api.User(r.Context())
	for _, role := range []string{"COMPANY", "SUPER_ADMIN", "AUTHENTICATED", "USER", "DOCTOR"} {
		if id.HasRole(role) {
			return nil
		}
	}
	return api.Failure(403, "FORBIDDEN", "No tenés permiso para esta operación.")
}
func (s *Service) PrescriptionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/doctorchart/patients/{patientId}/prescriptions", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "patientId")
		if err == nil {
			err = prescriptionRole(r)
		}
		if err != nil {
			api.WriteError(w, err)
			return
		}
		body, err := api.Decode(w, r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		p, err := s.CreatePrescription(r.Context(), api.User(r.Context()).UserID, id, body)
		if err == nil {
			w.Header().Set("Location", "/api/doctorchart/patients/"+strconv.FormatInt(id, 10)+"/prescriptions/"+strconv.FormatInt(p.ID, 10))
		}
		clinicalReply(w, p, err, 201)
	})
	mux.HandleFunc("GET /api/doctorchart/patients/{patientId}/prescriptions", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "patientId")
		if err == nil {
			err = prescriptionRole(r)
		}
		result := []Prescription{}
		if err == nil {
			err = s.transaction(r.Context(), true, func(tx pgx.Tx) error {
				ctx := r.Context()
				user := api.User(ctx).UserID
				if err := s.prescriptionAccess(ctx, tx, user, id); err != nil {
					return err
				}
				rows, err := tx.Query(ctx, `SELECT id,patient_id,author_user_id,created_at,type,diagnosis_description,general_instructions FROM patient_prescription_records WHERE patient_id=$1 AND author_user_id=$2 ORDER BY created_at DESC,id DESC`, id, user)
				if err != nil {
					return err
				}
				for rows.Next() {
					var p Prescription
					var date time.Time
					if err := rows.Scan(&p.ID, &p.PatientID, &p.Author, &date, &p.Type, &p.Diagnosis, &p.Instructions); err != nil {
						rows.Close()
						return err
					}
					p.Created = api.FormatInstant(date)
					result = append(result, p)
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					return err
				}
				for i := range result {
					if err := prescriptionChildren(ctx, tx, &result[i]); err != nil {
						return err
					}
				}
				return nil
			})
		}
		clinicalReply(w, result, err, 200)
	})
	readOrDelete := func(w http.ResponseWriter, r *http.Request) {
		patientID, err := pathID(r, "patientId")
		var id int64
		if err == nil {
			id, err = pathID(r, "recordId")
		}
		if err == nil {
			err = prescriptionRole(r)
		}
		var p Prescription
		if err == nil {
			err = s.transaction(r.Context(), r.Method == "GET", func(tx pgx.Tx) error {
				ctx := r.Context()
				user := api.User(ctx).UserID
				if err := s.prescriptionAccess(ctx, tx, user, patientID); err != nil {
					return err
				}
				var err error
				p, err = ownedPrescription(ctx, tx, user, patientID, id)
				if err != nil {
					return err
				}
				if r.Method == "DELETE" {
					for _, table := range []string{"prescription_medications", "prescription_diagnostic_orders", "patient_prescription_records"} {
						column := "record_id"
						if strings.HasPrefix(table, "patient_") {
							column = "id"
						}
						if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE "+column+"=$1", id); err != nil {
							return err
						}
					}
				}
				return nil
			})
		}
		status := 200
		if r.Method == "DELETE" {
			status = 204
		}
		clinicalReply(w, p, err, status)
	}
	mux.HandleFunc("GET /api/doctorchart/patients/{patientId}/prescriptions/{recordId}", readOrDelete)
	mux.HandleFunc("DELETE /api/doctorchart/patients/{patientId}/prescriptions/{recordId}", readOrDelete)
}
