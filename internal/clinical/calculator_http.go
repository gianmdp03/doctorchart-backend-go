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
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

type calculationLog struct {
	ID             int64           `json:"id"`
	PatientID      int64           `json:"patientId"`
	PatientName    string          `json:"patientFullName"`
	UserID         string          `json:"userId"`
	EncounterID    *int64          `json:"encounterId"`
	Code           string          `json:"calculationCode"`
	Name           string          `json:"calculationName"`
	Inputs         json.RawMessage `json:"inputSnapshot"`
	Value          *float64        `json:"resultValue"`
	Formatted      string          `json:"formattedResult"`
	Unit           *string         `json:"resultUnit"`
	Interpretation *string         `json:"resultInterpretation"`
	Severity       *string         `json:"severity"`
	Evolution      *string         `json:"plainTextEvolution"`
	At             string          `json:"createdAt"`
}

const calculationLogColumns = "l.id,l.patient_id,p.full_name,l.user_id,l.encounter_id,l.calculation_code,l.calculation_name,l.input_snapshot,l.result_value,l.formatted_result,l.result_unit,l.result_interpretation,l.severity,l.plain_text_evolution,l.created_at"

func scanCalculationLog(row pgx.Row) (calculationLog, error) {
	var log calculationLog
	var at time.Time
	err := row.Scan(&log.ID, &log.PatientID, &log.PatientName, &log.UserID, &log.EncounterID, &log.Code, &log.Name, &log.Inputs, &log.Value, &log.Formatted, &log.Unit, &log.Interpretation, &log.Severity, &log.Evolution, &at)
	log.At = api.FormatInstant(at)
	return log, err
}
func calculatorRequest(w http.ResponseWriter, r *http.Request, execute bool) (map[string]any, error) {
	request, err := api.Decode(w, r)
	if err != nil {
		return nil, err
	}
	fields := []string{"calculationCode", "inputSnapshot"}
	if execute {
		fields = append(fields, "patientId", "encounterId")
	}
	code, ok := request["calculationCode"].(string)
	_, inputsOK := request["inputSnapshot"].(map[string]any)
	if api.Only(request, fields...) != nil || !ok || api.JavaBlank(code) || !inputsOK {
		return nil, api.Invalid("La solicitud contiene datos inválidos.")
	}
	if execute {
		for _, key := range []string{"patientId", "encounterId"} {
			if key == "encounterId" && request[key] == nil {
				continue
			}
			value, err := api.Integer(request[key], key, 1)
			if err != nil {
				return nil, api.Invalid("La solicitud contiene datos inválidos.")
			}
			request[key] = value
		}
	}
	return request, nil
}
func (s *Service) calculatorPreferences(ctx context.Context, tx pgx.Tx, user string, codes []string, replace bool) (map[string]any, error) {
	if err := s.workspaces.Feature(ctx, tx, user, "CALCULATORS"); err != nil {
		return nil, err
	}
	var workspaceID, specialtyID int64
	err := tx.QueryRow(ctx, "SELECT id,specialty_id FROM doctor_workspaces WHERE user_id=$1 FOR UPDATE", user).Scan(&workspaceID, &specialtyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, api.Failure(409, "WORKSPACE_REQUIRED", "Configurá tu especialidad antes de elegir calculadoras.")
	}
	if err != nil {
		return nil, err
	}
	if codes != nil {
		seen := map[string]bool{}
		for _, code := range codes {
			if _, ok := s.calculators[code]; !ok || seen[code] {
				return nil, api.Failure(400, "INVALID_CALCULATOR_SELECTION", "La selección de calculadoras no es válida.")
			}
			seen[code] = true
		}
	}
	var id int64
	var updated time.Time
	err = tx.QueryRow(ctx, "SELECT id,updated_at FROM doctor_calculator_preferences WHERE workspace_id=$1 AND specialty_id=$2", workspaceID, specialtyID).Scan(&id, &updated)
	fresh := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !fresh {
		return nil, err
	}
	if (replace || fresh) && codes == nil {
		codes = []string{}
		rows, err := tx.Query(ctx, "SELECT c.code FROM specialty_calculator_defaults d JOIN clinical_calculation_definitions c ON c.id=d.calculator_id WHERE d.specialty_id=$1 ORDER BY d.position", specialtyID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var code string
			if err := rows.Scan(&code); err != nil {
				rows.Close()
				return nil, err
			}
			if _, ok := s.calculators[code]; ok {
				codes = append(codes, code)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if replace || fresh {
		selected := []int64{}
		for _, code := range codes {
			var calculationID int64
			err := tx.QueryRow(ctx, "SELECT id FROM clinical_calculation_definitions WHERE code=$1", code).Scan(&calculationID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, api.Failure(400, "INVALID_CALCULATOR_SELECTION", "La selección de calculadoras no es válida.")
			}
			if err != nil {
				return nil, err
			}
			selected = append(selected, calculationID)
		}
		updated = time.Now().UTC()
		if fresh {
			err = tx.QueryRow(ctx, "INSERT INTO doctor_calculator_preferences(workspace_id,specialty_id,created_at,updated_at) VALUES($1,$2,$3,$4) RETURNING id", workspaceID, specialtyID, time.Now().UTC(), updated).Scan(&id)
		} else {
			_, err = tx.Exec(ctx, "UPDATE doctor_calculator_preferences SET updated_at=$2 WHERE id=$1", id, updated)
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM doctor_calculator_selections WHERE preference_id=$1", id); err != nil {
			return nil, err
		}
		for position, calculationID := range selected {
			if _, err := tx.Exec(ctx, "INSERT INTO doctor_calculator_selections(preference_id,calculator_id,position) VALUES($1,$2,$3)", id, calculationID, position); err != nil {
				return nil, err
			}
		}
	} else {
		codes = []string{}
		rows, err := tx.Query(ctx, "SELECT c.code FROM doctor_calculator_selections d JOIN clinical_calculation_definitions c ON c.id=d.calculator_id WHERE d.preference_id=$1 ORDER BY d.position", id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var code string
			if err := rows.Scan(&code); err != nil {
				rows.Close()
				return nil, err
			}
			codes = append(codes, code)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return map[string]any{"userId": user, "favoriteCalculationCodes": codes, "updatedAt": api.FormatInstant(updated)}, nil
}
func (s *Service) CalculatorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/doctorchart/calculators/preview", func(w http.ResponseWriter, r *http.Request) {
		request, err := calculatorRequest(w, r, false)
		var result calculationResult
		if err == nil {
			err = s.workspaces.RequireFeature(r.Context(), api.User(r.Context()).UserID, "CALCULATORS")
		}
		if err == nil {
			result, err = s.calculate(request["calculationCode"].(string), request["inputSnapshot"].(map[string]any), time.Now().UTC())
		}
		clinicalReply(w, result, err, 200)
	})
	mux.HandleFunc("POST /api/doctorchart/calculators/execute", func(w http.ResponseWriter, r *http.Request) {
		request, err := calculatorRequest(w, r, true)
		var log calculationLog
		if err == nil {
			err = s.transaction(r.Context(), false, func(tx pgx.Tx) error {
				ctx, user := r.Context(), api.User(r.Context()).UserID
				patient, err := ownedPatient(ctx, tx, user, request["patientId"].(int64))
				if err != nil {
					return err
				}
				if err := s.workspaces.Feature(ctx, tx, user, "CALCULATORS"); err != nil {
					return err
				}
				var encounterID *int64
				if request["encounterId"] != nil {
					id := request["encounterId"].(int64)
					encounter, err := ownedEncounter(ctx, tx, user, id)
					var failure *api.Error
					if errors.As(err, &failure) && failure.Code == "ENCOUNTER_NOT_FOUND" {
						return api.Failure(404, "RESOURCE_NOT_FOUND", "No se encontró el recurso.")
					}
					if err != nil {
						return err
					}
					if encounter.PatientID != patient.ID {
						return api.Failure(400, "ENCOUNTER_PATIENT_MISMATCH", "La consulta no pertenece al paciente del cálculo.")
					}
					encounterID = &id
				}
				result, err := s.calculate(request["calculationCode"].(string), request["inputSnapshot"].(map[string]any), time.Now().UTC())
				if err != nil {
					return err
				}
				inputs, err := json.Marshal(request["inputSnapshot"])
				if err != nil {
					return err
				}
				at := time.Now().UTC()
				var id int64
				err = tx.QueryRow(ctx, "INSERT INTO patient_calculation_logs(patient_id,user_id,encounter_id,calculation_code,calculation_name,input_snapshot,result_value,formatted_result,result_unit,result_interpretation,severity,plain_text_evolution,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id", patient.ID, user, encounterID, result.Code, result.Name, inputs, result.Raw, api.JavaTrim(result.Formatted+" "+result.Unit), result.Unit, result.Interpretation.Label, result.Interpretation.Severity, result.Evolution, at).Scan(&id)
				if err != nil {
					return err
				}
				log = calculationLog{ID: id, PatientID: patient.ID, PatientName: patient.FullName, UserID: user, EncounterID: encounterID, Code: result.Code, Name: result.Name, Inputs: inputs, Value: &result.Raw, Formatted: api.JavaTrim(result.Formatted + " " + result.Unit), Unit: &result.Unit, Interpretation: &result.Interpretation.Label, Severity: &result.Interpretation.Severity, Evolution: &result.Evolution, At: api.FormatInstant(at)}
				return nil
			})
		}
		if err == nil {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			w.Header().Set("Location", scheme+"://"+r.Host+r.URL.Path+"/"+strconv.FormatInt(log.ID, 10))
		}
		clinicalReply(w, log, err, 201)
	})
	mux.HandleFunc("GET /api/doctorchart/calculators/catalog", func(w http.ResponseWriter, r *http.Request) {
		catalog := []map[string]any{}
		err := s.transaction(r.Context(), true, func(tx pgx.Tx) error {
			if err := s.workspaces.Feature(r.Context(), tx, api.User(r.Context()).UserID, "CALCULATORS"); err != nil {
				return err
			}
			rows, err := tx.Query(r.Context(), "SELECT id,code,name,short_name,category,category_label,description,evidence_reference,summary_formula,required_parameters,result_unit,is_system,display_order FROM clinical_calculation_definitions ORDER BY display_order,id")
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id int64
				var code, name, category, label string
				var short, description, evidence, formula, unit *string
				var parameters json.RawMessage
				var system bool
				var order int
				if err := rows.Scan(&id, &code, &name, &short, &category, &label, &description, &evidence, &formula, &parameters, &unit, &system, &order); err != nil {
					return err
				}
				if _, ok := s.calculators[code]; !ok {
					continue
				}
				// MapStruct leaves the source record's isSystem component false.
				catalog = append(catalog, map[string]any{"id": id, "code": code, "name": name, "shortName": short, "category": category, "categoryLabel": label, "description": description, "evidenceReference": evidence, "summaryFormula": formula, "requiredParameters": parameters, "resultUnit": unit, "isSystem": false, "displayOrder": order})
			}
			return rows.Err()
		})
		clinicalReply(w, catalog, err, 200)
	})
	preferences := func(w http.ResponseWriter, r *http.Request) {
		var codes []string
		var result map[string]any
		var err error
		if r.Method == "PUT" {
			request, decodeErr := api.Decode(w, r)
			err = decodeErr
			if err == nil {
				values, ok := request["favoriteCalculationCodes"].([]any)
				if api.Only(request, "favoriteCalculationCodes") != nil || !ok || len(values) < 1 || len(values) > 64 {
					err = api.Invalid("La solicitud contiene datos inválidos.")
				} else {
					codes = []string{}
					for _, value := range values {
						code, ok := value.(string)
						if !ok || api.JavaBlank(code) {
							err = api.Invalid("La solicitud contiene datos inválidos.")
							break
						}
						codes = append(codes, code)
					}
				}
			}
		}
		if err == nil {
			err = s.transaction(r.Context(), false, func(tx pgx.Tx) error {
				var err error
				result, err = s.calculatorPreferences(r.Context(), tx, api.User(r.Context()).UserID, codes, r.Method != "GET")
				return err
			})
		}
		clinicalReply(w, result, err, 200)
	}
	mux.HandleFunc("GET /api/doctorchart/calculators/preferences", preferences)
	mux.HandleFunc("PUT /api/doctorchart/calculators/preferences", preferences)
	mux.HandleFunc("POST /api/doctorchart/calculators/preferences/reset", preferences)
	mux.HandleFunc("GET /api/doctorchart/calculators/patients/{patientId}/history", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "patientId")
		result := []calculationLog{}
		var from, to *time.Time
		for key, target := range map[string]**time.Time{"fromDate": &from, "toDate": &to} {
			if value := r.URL.Query().Get(key); value != "" {
				parsed, parseErr := time.Parse(time.RFC3339Nano, value)
				if parseErr != nil {
					err = api.Invalid("La solicitud contiene datos inválidos.")
				} else {
					*target = &parsed
				}
			}
		}
		code := strings.ToLower(api.JavaTrim(r.URL.Query().Get("calculationCode")))
		var filter *string
		if !api.JavaBlank(code) {
			filter = &code
		}
		if err == nil {
			err = s.transaction(r.Context(), true, func(tx pgx.Tx) error {
				user := api.User(r.Context()).UserID
				if _, err := ownedPatient(r.Context(), tx, user, id); err != nil {
					return err
				}
				if err := s.workspaces.Feature(r.Context(), tx, user, "CALCULATORS"); err != nil {
					return err
				}
				rows, err := tx.Query(r.Context(), "SELECT "+calculationLogColumns+" FROM patient_calculation_logs l JOIN patients p ON p.id=l.patient_id WHERE p.id=$1 AND p.user_id=$2 AND ($3::text IS NULL OR l.calculation_code=$3) AND ($4::timestamptz IS NULL OR l.created_at>=$4) AND ($5::timestamptz IS NULL OR l.created_at<=$5) ORDER BY l.created_at DESC", id, user, filter, from, to)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					log, err := scanCalculationLog(rows)
					if err != nil {
						return err
					}
					result = append(result, log)
				}
				return rows.Err()
			})
		}
		clinicalReply(w, result, err, 200)
	})
	mux.HandleFunc("GET /api/doctorchart/calculators/patients/{patientId}/autofill", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "patientId")
		var result map[string]any
		if err == nil {
			err = s.transaction(r.Context(), true, func(tx pgx.Tx) error {
				var err error
				result, err = s.calculatorAutofill(r.Context(), tx, api.User(r.Context()).UserID, id)
				return err
			})
		}
		clinicalReply(w, result, err, 200)
	})
}
func numericAlias(raw json.RawMessage, candidates ...string) *float64 {
	// Decode entries in PostgreSQL's JSON order, matching Hibernate's LinkedHashMap.
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	type entry struct {
		key   string
		value any
	}
	entries := []entry{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil
		}
		var value any
		if decoder.Decode(&value) != nil {
			return nil
		}
		entries = append(entries, entry{key.(string), value})
	}
	for _, candidate := range candidates {
		for _, item := range entries {
			if !strings.EqualFold(item.key, candidate) {
				continue
			}
			number, ok := calculationNumber(item.value)
			if str, isText := item.value.(string); isText {
				var err error
				number, err = strconv.ParseFloat(strings.ReplaceAll(api.JavaTrim(str), ",", "."), 64)
				ok = err == nil
			}
			if ok {
				return &number
			}
		}
	}
	return nil
}
func (s *Service) calculatorAutofill(ctx context.Context, tx pgx.Tx, user string, id int64) (map[string]any, error) {
	patient, err := ownedPatient(ctx, tx, user, id)
	if err != nil {
		return nil, err
	}
	if err := s.workspaces.Feature(ctx, tx, user, "CALCULATORS"); err != nil {
		return nil, err
	}
	if err := s.detail(ctx, tx, &patient); err != nil {
		return nil, err
	}
	birth, err := time.Parse("2006-01-02", patient.BirthDate)
	if err != nil {
		return nil, err
	}
	months := ageMonths(birth, time.Now().In(s.zone))
	var age *int
	if months != nil {
		years := *months / 12
		age = &years
	}
	result := map[string]any{"patientId": id, "fullName": patient.FullName, "birthDate": patient.BirthDate, "ageYears": age, "gender": patient.Gender, "weightKg": nil, "heightCm": nil, "systolicBloodPressure": nil, "diastolicBloodPressure": nil, "creatinineMgDl": nil, "serumCalciumMgDl": nil, "serumAlbuminGDl": nil, "encounterId": nil, "encounterDate": nil}
	if patient.Measurements != nil {
		result["weightKg"] = patient.Measurements.Weight
		result["heightCm"] = patient.Measurements.Height
	}
	aliases := map[string][]string{"creatinineMgDl": {"creatinine", "serumcreatinine", "creatinina", "creatinina_serica", "creatininaserica", "cr"}, "serumCalciumMgDl": {"calcium", "serumcalcium", "calcio", "calcio_serico", "calcioserico", "ca"}, "serumAlbuminGDl": {"albumin", "serumalbumin", "albumina", "albumina_serica", "albuminaserica", "alb"}}
	rows, err := tx.Query(ctx, "SELECT encounter_date,id,vital_signs,form_values FROM clinical_encounters WHERE patient_id=$1 AND encounter_date<=$2 AND (encounter_date AT TIME ZONE 'America/Argentina/Buenos_Aires')::date>=$3::date ORDER BY encounter_date DESC,id DESC", id, time.Now().UTC(), patient.BirthDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var at time.Time
		var encounterID int64
		var vitals, values json.RawMessage
		if err := rows.Scan(&at, &encounterID, &vitals, &values); err != nil {
			return nil, err
		}
		if result["encounterId"] == nil {
			result["encounterId"] = encounterID
			result["encounterDate"] = at.UTC().Format(time.RFC3339Nano)
		}
		vitalValues, err := wizard.Decode(vitals)
		if err != nil {
			return nil, err
		}
		for _, key := range []string{"systolicBloodPressure", "diastolicBloodPressure"} {
			if result[key] == nil {
				result[key] = vitalValues[key]
			}
		}
		for key, candidates := range aliases {
			if result[key] == nil {
				if number := numericAlias(values, candidates...); number != nil {
					result[key] = number
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for key, candidates := range aliases {
		if result[key] == nil {
			if number := numericAlias(patient.Values, candidates...); number != nil {
				result[key] = number
			}
		}
	}
	return result, nil
}
