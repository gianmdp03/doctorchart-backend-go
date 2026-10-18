package clinical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

// EncounterSummary is the stored clinical form shared by list and detail responses.
type EncounterSummary struct {
	ID               int64             `json:"id"`
	Version          int64             `json:"version"`
	PatientID        int64             `json:"patientId"`
	PatientName      string            `json:"patientFullName"`
	Date             string            `json:"encounterDate"`
	SpecialtyID      int64             `json:"specialtyId"`
	Specialty        workspace.Summary `json:"specialty"`
	Type             string            `json:"consultationType"`
	Complaint        *string           `json:"chiefComplaint"`
	DiagnosisCode    *string           `json:"primaryDiagnosisCode"`
	Diagnosis        *string           `json:"primaryDiagnosisDescription"`
	VitalSigns       json.RawMessage   `json:"vitalSigns"`
	Prescriptions    json.RawMessage   `json:"prescriptions"`
	History          *string           `json:"historyOfPresentIllness"`
	Treatment        *string           `json:"treatmentPlan"`
	FollowUp         *string           `json:"followUpInstructions"`
	FollowUpDate     *string           `json:"followUpDate"`
	Measurements     Measurements      `json:"measurements"`
	Values           json.RawMessage   `json:"formValues"`
	Results          json.RawMessage   `json:"formulaResults"`
	Snapshot         json.RawMessage   `json:"formDefinitionSnapshot"`
	Context          json.RawMessage   `json:"formulaContext"`
	WorkspaceVersion int64             `json:"workspaceVersion"`
}
type Encounter struct {
	EncounterSummary
	Physical   *string         `json:"physicalExamination"`
	Secondary  json.RawMessage `json:"secondaryDiagnoses"`
	Impression *string         `json:"clinicalImpression"`
	Orders     json.RawMessage `json:"diagnosticOrders"`
	owner      string
	instant    time.Time
}

const encounterColumns = `e.id,e.version,e.patient_id,p.full_name,e.encounter_date,e.specialty_id,s.id,s.code,s.display_name,s.description,e.consultation_type,e.chief_complaint,e.primary_diagnosis_code,e.primary_diagnosis_description,e.vital_signs,e.prescriptions,e.history_of_present_illness,e.treatment_plan,e.follow_up_instructions,e.follow_up_date::text,e.weight_kg,e.height_cm,e.head_circumference_cm,e.form_values,e.formula_results,e.form_definition_snapshot,e.formula_context,e.workspace_version,e.physical_examination,e.secondary_diagnoses,e.clinical_impression,e.diagnostic_orders,p.user_id`
const encounterFrom = ` FROM clinical_encounters e JOIN patients p ON p.id=e.patient_id JOIN specialties s ON s.id=e.specialty_id `

func scanEncounter(row pgx.Row, extra ...any) (Encounter, error) {
	var e Encounter
	dest := []any{&e.ID, &e.Version, &e.PatientID, &e.PatientName, &e.instant, &e.SpecialtyID, &e.Specialty.ID, &e.Specialty.Code, &e.Specialty.Name, &e.Specialty.Description, &e.Type, &e.Complaint, &e.DiagnosisCode, &e.Diagnosis, &e.VitalSigns, &e.Prescriptions, &e.History, &e.Treatment, &e.FollowUp, &e.FollowUpDate, &e.Measurements.Weight, &e.Measurements.Height, &e.Measurements.Head, &e.Values, &e.Results, &e.Snapshot, &e.Context, &e.WorkspaceVersion, &e.Physical, &e.Secondary, &e.Impression, &e.Orders, &e.owner}
	err := row.Scan(append(dest, extra...)...)
	e.Date = e.instant.UTC().Format(time.RFC3339Nano)
	return e, err
}
func ownedEncounter(ctx context.Context, tx pgx.Tx, userID string, id int64) (Encounter, error) {
	e, err := scanEncounter(tx.QueryRow(ctx, "SELECT "+encounterColumns+encounterFrom+"WHERE e.id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return e, api.Failure(404, "ENCOUNTER_NOT_FOUND", "No se encontró la consulta.")
	}
	if err != nil {
		return e, err
	}
	if e.owner != userID {
		return e, api.Failure(403, "FORBIDDEN", "No tenés permiso para acceder a este paciente.")
	}
	return e, nil
}
func consultation(request map[string]any) (string, error) {
	value, err := api.Text(request, "consultationType", 32)
	if err != nil {
		return "", err
	}
	switch value {
	case "FIRST_VISIT", "FOLLOW_UP", "ROUTINE_CHECKUP", "EMERGENCY", "INTERCONSULTATION":
		return value, nil
	}
	return "", api.Invalid("La solicitud contiene datos inválidos.")
}
func encounterDate(request map[string]any) (time.Time, error) {
	value, err := api.Text(request, "encounterDate", 64)
	if err != nil {
		return time.Time{}, err
	}
	date, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || date.After(time.Now()) {
		return time.Time{}, api.Invalid("La solicitud contiene datos inválidos.")
	}
	return date, nil
}
func (s *Service) encounterContext(ctx context.Context, tx pgx.Tx, p Patient, date time.Time, excluded *int64) (map[string]any, error) {
	result, err := patientContext(date.In(s.viewZone(ctx)).Format("2006-01-02"), map[string]any{"patient.birthDate": p.BirthDate, "patient.gender": p.Gender})
	if err != nil {
		return nil, err
	}
	result["zone"] = s.viewZone(ctx).String()
	if p.Gender == nil {
		result["patient.gender"] = nil
	} else {
		result["patient.gender"] = *p.Gender
	}
	rows, err := tx.Query(ctx, `SELECT weight_kg,height_cm FROM clinical_encounters WHERE patient_id=$1 AND encounter_date<$2 AND ($3::bigint IS NULL OR id<>$3) AND (encounter_date AT TIME ZONE $5)::date >= $4::date AND (weight_kg IS NOT NULL OR height_cm IS NOT NULL OR head_circumference_cm IS NOT NULL) ORDER BY encounter_date DESC,id DESC`, p.ID, date, excluded, p.BirthDate, s.viewZone(ctx).String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var weight, height *float64
	for rows.Next() {
		var w, h *float64
		if err := rows.Scan(&w, &h); err != nil {
			return nil, err
		}
		if weight == nil {
			weight = w
		}
		if height == nil {
			height = h
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if weight != nil {
		result["patient.latestWeightKg"] = *weight
	}
	if height != nil {
		result["patient.latestHeightCm"] = *height
	}
	return result, nil
}
func applyEncounter(e *Encounter, evaluation wizard.Evaluation) error {
	p := evaluation.Projections
	for key, dest := range map[string]**string{"chiefComplaint": &e.Complaint, "historyOfPresentIllness": &e.History, "physicalExamination": &e.Physical, "primaryDiagnosisDescription": &e.Diagnosis, "treatmentPlan": &e.Treatment, "followUpInstructions": &e.FollowUp, "followUpDate": &e.FollowUpDate} {
		text, err := nullableText(p["encounter."+key])
		if err != nil {
			return err
		}
		*dest = text
	}
	for key, dest := range map[string]**float64{"weightKg": &e.Measurements.Weight, "heightCm": &e.Measurements.Height, "headCircumferenceCm": &e.Measurements.Head} {
		if p["encounter."+key] == nil {
			*dest = nil
			continue
		}
		raw, err := json.Marshal(p["encounter."+key])
		if err != nil {
			return err
		}
		var value float64
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		*dest = &value
	}
	var err error
	e.Values, err = json.Marshal(evaluation.Values)
	if err != nil {
		return err
	}
	e.Results, err = json.Marshal(evaluation.Results)
	return err
}
func (s *Service) CreateEncounter(ctx context.Context, identity api.Identity, request map[string]any) (Encounter, error) {
	var result Encounter
	if err := api.Only(request, "patientId", "workspaceVersion", "consultationType", "values"); err != nil {
		return result, err
	}
	patientID, err := api.Integer(request["patientId"], "patientId", 1)
	if err != nil {
		return result, err
	}
	expected, err := api.Version(map[string]any{"expectedVersion": request["workspaceVersion"]}, false)
	if err != nil {
		return result, err
	}
	kind, err := consultation(request)
	if err != nil {
		return result, err
	}
	values, err := api.Object(request, "values")
	if err != nil {
		return result, err
	}
	if err := s.refreshQuotaRules(ctx, identity); err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := lockPatient(ctx, tx, identity.UserID, patientID); err != nil {
			return err
		}
		p, err := ownedPatient(ctx, tx, identity.UserID, patientID)
		if err != nil {
			return err
		}
		setup, err := s.workspaces.Effective(ctx, tx, identity.UserID, *expected)
		if err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, identity.UserID, "NEW_ENCOUNTER"); err != nil {
			return err
		}
		// PostgreSQL/pgx persist microseconds. Return the exact persisted instant.
		date := time.Now().UTC().Truncate(time.Microsecond)
		var count int64
		var first bool
		if err := tx.QueryRow(ctx, `SELECT count(*),coalesce(bool_or(consultation_type='FIRST_VISIT'),false) FROM clinical_encounters WHERE patient_id=$1`, patientID).Scan(&count, &first); err != nil {
			return err
		}
		if kind == "FIRST_VISIT" && first {
			return api.Failure(400, "FIRST_VISIT_ALREADY_EXISTS", "El paciente ya cuenta con una primera consulta registrada.")
		}
		if kind != "FIRST_VISIT" && count == 0 {
			return api.Failure(400, "FIRST_VISIT_REQUIRED", "La primera atención del paciente debe registrarse como Primera Consulta.")
		}
		admin := false
		for _, role := range identity.Roles {
			if strings.EqualFold(role, "ROLE_SUPER_ADMIN") {
				admin = true
			}
		}
		if !admin {
			plan := billing.Plan(identity.Plan)
			if plan != "NONE" && plan != "FREE_TRIAL" {
				if !s.rules.Available() {
					return api.Failure(503, "BILLING_UNAVAILABLE", "Los límites no están disponibles. Reintentá más tarde.")
				}
			}
			max := s.rules.Int(plan, "max_records_per_patient", 100)
			if plan == "NONE" || max != -1 && count >= int64(max) {
				return api.Failure(403, "FORBIDDEN", "No se pudo completar la solicitud.")
			}
		}
		definition, err := wizard.Decode(setup.Definition)
		if err != nil {
			return err
		}
		form, ok := definition["encounterForm"].(map[string]any)
		if !ok {
			return api.Failure(409, "INVALID_WIZARD_DEFINITION", "La configuración no es válida.")
		}
		context, err := s.encounterContext(ctx, tx, p, date, nil)
		if err != nil {
			return err
		}
		evaluation, err := s.forms.Evaluate(form, values, context)
		if err != nil {
			return err
		}
		e := Encounter{EncounterSummary: EncounterSummary{PatientID: p.ID, PatientName: p.FullName, Date: date.Format(time.RFC3339Nano), SpecialtyID: setup.Specialty.ID, Specialty: setup.Specialty, Type: kind, Prescriptions: json.RawMessage("[]"), WorkspaceVersion: setup.Version}, Secondary: json.RawMessage("[]"), Orders: json.RawMessage("[]"), instant: date, owner: identity.UserID}
		e.Snapshot, err = json.Marshal(form)
		if err != nil {
			return err
		}
		e.Context, err = json.Marshal(context)
		if err != nil {
			return err
		}
		if err := applyEncounter(&e, evaluation); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO clinical_encounters(patient_id,specialty_id,encounter_date,consultation_type,chief_complaint,history_of_present_illness,physical_examination,primary_diagnosis_description,treatment_plan,follow_up_instructions,follow_up_date,weight_kg,height_cm,head_circumference_cm,form_values,formula_results,form_definition_snapshot,formula_context,workspace_version,secondary_diagnoses,prescriptions,diagnostic_orders,version,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::date,$12,$13,$14,$15,$16,$17,$18,$19,'[]','[]','[]',0,clock_timestamp()) RETURNING id`, e.PatientID, e.SpecialtyID, date, e.Type, e.Complaint, e.History, e.Physical, e.Diagnosis, e.Treatment, e.FollowUp, e.FollowUpDate, e.Measurements.Weight, e.Measurements.Height, e.Measurements.Head, e.Values, e.Results, e.Snapshot, e.Context, e.WorkspaceVersion).Scan(&e.ID)
		result = e
		return err
	})
	return result, err
}
func (s *Service) UpdateEncounter(ctx context.Context, userID string, id int64, request map[string]any) (Encounter, error) {
	var result Encounter
	if err := api.Only(request, "expectedVersion", "encounterDate", "consultationType", "values"); err != nil {
		return result, err
	}
	expected, err := api.Version(request, false)
	if err != nil {
		return result, err
	}
	var requestedDate *time.Time
	if _, present := request["encounterDate"]; present {
		date, err := encounterDate(request)
		if err != nil {
			return result, err
		}
		requestedDate = &date
	}
	kind, err := consultation(request)
	if err != nil {
		return result, err
	}
	values, err := api.Object(request, "values")
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		e, err := ownedEncounter(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if err := lockPatient(ctx, tx, userID, e.PatientID); err != nil {
			return err
		}
		// Re-read after waiting: another writer may have changed the encounter
		// version or removed it while this transaction acquired the patient lock.
		e, err = ownedEncounter(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		date := e.instant
		if requestedDate != nil && !requestedDate.UTC().Truncate(time.Microsecond).Equal(date) {
			return api.Failure(400, "IMMUTABLE_ENCOUNTER_DATE", "La fecha de creación es asignada por el servidor y no se puede modificar.")
		}
		if *expected != e.Version {
			return api.Failure(409, "VERSION_CONFLICT", "El registro cambió. Actualizá los datos antes de guardar.")
		}
		if kind == "FIRST_VISIT" {
			var duplicate bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM clinical_encounters WHERE patient_id=$1 AND consultation_type='FIRST_VISIT' AND id<>$2)`, e.PatientID, id).Scan(&duplicate); err != nil {
				return err
			}
			if duplicate {
				return api.Failure(400, "FIRST_VISIT_ALREADY_EXISTS", "El paciente ya cuenta con una primera consulta registrada.")
			}
		}
		context, err := wizard.Decode(e.Context)
		if err != nil {
			return err
		}
		form, err := wizard.Decode(e.Snapshot)
		if err != nil {
			return err
		}
		evaluation, err := s.forms.Evaluate(form, values, context)
		if err != nil {
			return err
		}
		if err := applyEncounter(&e, evaluation); err != nil {
			return err
		}
		e.Type = kind
		e.Date = date.UTC().Format(time.RFC3339Nano)
		e.Context, err = json.Marshal(context)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `UPDATE clinical_encounters SET encounter_date=$2,consultation_type=$3,chief_complaint=$4,history_of_present_illness=$5,physical_examination=$6,primary_diagnosis_description=$7,treatment_plan=$8,follow_up_instructions=$9,follow_up_date=$10::date,weight_kg=$11,height_cm=$12,head_circumference_cm=$13,form_values=$14,formula_results=$15,formula_context=$16,version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND version=$17 RETURNING version`, id, date, kind, e.Complaint, e.History, e.Physical, e.Diagnosis, e.Treatment, e.FollowUp, e.FollowUpDate, e.Measurements.Weight, e.Measurements.Height, e.Measurements.Head, e.Values, e.Results, e.Context, *expected).Scan(&e.Version)
		result = e
		return err
	})
	return result, err
}
func (s *Service) encounterList(ctx context.Context, tx pgx.Tx, where string, args []any, order string) ([]EncounterSummary, error) {
	rows, err := tx.Query(ctx, "SELECT "+encounterColumns+encounterFrom+"WHERE "+where+" ORDER BY "+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []EncounterSummary{}
	for rows.Next() {
		e, err := scanEncounter(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e.EncounterSummary)
	}
	return result, rows.Err()
}
func pathID(r *http.Request, key string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil {
		return 0, api.Invalid("La solicitud contiene datos inválidos.")
	}
	return id, nil
}
func clinicalReply(w http.ResponseWriter, value any, err error, status int) {
	if err != nil {
		api.WriteError(w, err)
		return
	}
	if status == 204 {
		w.WriteHeader(status)
		return
	}
	api.WriteJSON(w, status, value)
}
func (s *Service) EncounterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/doctorchart/encounters", func(w http.ResponseWriter, r *http.Request) {
		body, err := api.Decode(w, r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		e, err := s.CreateEncounter(r.Context(), api.User(r.Context()), body)
		if err == nil {
			w.Header().Set("Location", "/api/doctorchart/encounters/"+strconv.FormatInt(e.ID, 10))
		}
		clinicalReply(w, e, err, 201)
	})
	mux.HandleFunc("PUT /api/doctorchart/encounters/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err != nil {
			api.WriteError(w, err)
			return
		}
		body, err := api.Decode(w, r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		e, err := s.UpdateEncounter(r.Context(), api.User(r.Context()).UserID, id, body)
		clinicalReply(w, e, err, 200)
	})
	mux.HandleFunc("GET /api/doctorchart/encounters/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		var e Encounter
		if err == nil {
			err = s.transaction(r.Context(), true, func(tx pgx.Tx) error {
				var err error
				e, err = ownedEncounter(r.Context(), tx, api.User(r.Context()).UserID, id)
				return err
			})
		}
		clinicalReply(w, e, err, 200)
	})
	mux.HandleFunc("DELETE /api/doctorchart/encounters/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err == nil {
			err = s.transaction(r.Context(), false, func(tx pgx.Tx) error {
				e, err := ownedEncounter(r.Context(), tx, api.User(r.Context()).UserID, id)
				if err != nil {
					return err
				}
				if err := lockPatient(r.Context(), tx, api.User(r.Context()).UserID, e.PatientID); err != nil {
					return err
				}
				if _, err := ownedEncounter(r.Context(), tx, api.User(r.Context()).UserID, id); err != nil {
					return err
				}
				_, err = tx.Exec(r.Context(), "DELETE FROM clinical_encounters WHERE id=$1", id)
				return err
			})
		}
		clinicalReply(w, nil, err, 204)
	})
	byPatient := func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "patientId")
		result := []EncounterSummary{}
		where := "e.patient_id=$1"
		args := []any{id}
		if err == nil && r.PathValue("specialtyId") != "" {
			specialty, parseErr := pathID(r, "specialtyId")
			err = parseErr
			where += " AND e.specialty_id=$2"
			args = append(args, specialty)
		}
		if err == nil {
			err = s.transaction(r.Context(), true, func(tx pgx.Tx) error {
				if _, err := ownedPatient(r.Context(), tx, api.User(r.Context()).UserID, id); err != nil {
					return err
				}
				var err error
				result, err = s.encounterList(r.Context(), tx, where, args, "e.encounter_date DESC,e.id DESC")
				return err
			})
		}
		clinicalReply(w, result, err, 200)
	}
	mux.HandleFunc("GET /api/doctorchart/encounters/patient/{patientId}", byPatient)
	mux.HandleFunc("GET /api/doctorchart/encounters/patient/{patientId}/specialty/{specialtyId}", byPatient)
	s.encounterQueryRoutes(mux)
}
