package clinical

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
	"github.com/syntah/doctorchart-backend-go/internal/growth"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

type Service struct {
	pool        *pgxpool.Pool
	workspaces  *workspace.Store
	forms       *wizard.Validator
	rules       *billing.Rules
	zone        *time.Location
	growth      *growth.References
	calculators map[string]calculatorMetadata
	files       *storage.Store
}

func New(pool *pgxpool.Pool, workspaces *workspace.Store, forms *wizard.Validator, rules *billing.Rules, files *storage.Store) (*Service, error) {
	zone, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		return nil, err
	}
	growth, err := growth.Load()
	if err != nil {
		return nil, err
	}
	calculators := map[string]calculatorMetadata{}
	if err := json.Unmarshal(calculatorJSON, &calculators); err != nil {
		return nil, err
	}
	return &Service{pool: pool, workspaces: workspaces, forms: forms, rules: rules, zone: zone, growth: growth, calculators: calculators, files: files}, nil
}

type Measurements struct {
	Weight *float64 `json:"weightKg"`
	Height *float64 `json:"heightCm"`
	Head   *float64 `json:"headCircumferenceCm"`
}
type Patient struct {
	ID               int64           `json:"id"`
	Version          int64           `json:"version"`
	FullName         string          `json:"fullName"`
	BirthDate        string          `json:"birthDate"`
	NationalID       *string         `json:"nationalId"`
	Gender           *string         `json:"gender"`
	BloodType        *string         `json:"bloodType"`
	Phone            *string         `json:"phone"`
	Email            *string         `json:"email"`
	Address          *string         `json:"address"`
	Emergency        json.RawMessage `json:"emergencyContact"`
	Insurance        json.RawMessage `json:"insuranceInfo"`
	Allergies        json.RawMessage `json:"allergies"`
	Chronic          json.RawMessage `json:"chronicConditions"`
	Guardian         json.RawMessage `json:"guardianInfo"`
	Perinatal        json.RawMessage `json:"perinatalHistory"`
	Personal         json.RawMessage `json:"personalHistory"`
	Family           json.RawMessage `json:"familyHistory"`
	Immunization     json.RawMessage `json:"immunizationAndNutrition"`
	Measurements     *Measurements   `json:"latestMeasurements"`
	Values           json.RawMessage `json:"formValues"`
	Results          json.RawMessage `json:"formulaResults"`
	Snapshot         json.RawMessage `json:"formDefinitionSnapshot"`
	Context          json.RawMessage `json:"formulaContext"`
	WorkspaceVersion int64           `json:"workspaceVersion"`
	owner            string
}

const patientColumns = "id,version,full_name,birth_date::text,national_id,gender,blood_type,phone,email,address,emergency_contact,insurance_info,allergies,chronic_conditions,guardian_info,perinatal_history,personal_history,family_history,immunization_and_nutrition,form_values,formula_results,form_definition_snapshot,formula_context,workspace_version,user_id"

func scanPatient(row pgx.Row) (Patient, error) {
	var p Patient
	err := row.Scan(&p.ID, &p.Version, &p.FullName, &p.BirthDate, &p.NationalID, &p.Gender, &p.BloodType, &p.Phone, &p.Email, &p.Address, &p.Emergency, &p.Insurance, &p.Allergies, &p.Chronic, &p.Guardian, &p.Perinatal, &p.Personal, &p.Family, &p.Immunization, &p.Values, &p.Results, &p.Snapshot, &p.Context, &p.WorkspaceVersion, &p.owner)
	if err == nil {
		err = normalizePatientHistory(&p)
	}
	return p, err
}

// Hibernate materializes JSON record components, including omitted nullable fields.
// Match that read representation and its subsequent persistence on entity updates.
func normalizePatientHistory(p *Patient) error {
	for _, field := range []struct {
		raw  *json.RawMessage
		keys []string
	}{
		{&p.Emergency, []string{"fullName", "phone", "relationship", "nationalId", "address"}},
		{&p.Insurance, []string{"providerName", "planName", "affiliateNumber"}},
		{&p.Guardian, []string{"fatherFullName", "motherFullName", "siblingsCount", "firstGuardianFullName", "secondGuardianFullName", "firstGuardianPhone", "secondGuardianPhone", "firstGuardianNationalId", "secondGuardianNationalId", "address", "primaryGuardian", "primaryContactPhone", "hasSiblings", "siblingOrder", "livingArrangement"}},
		{&p.Perinatal, []string{"gestationCount", "prenatalVisitsSummary", "pregnancyComplications"}},
		{&p.Personal, []string{"chronicOrPreviousDiseases", "hospitalizationsOrSurgeries", "allergies", "habitualMedication", "birthMeasurements"}},
		{&p.Family, []string{"familyChronicDiseases", "familyAllergies", "geneticDiseases", "paternalDiseases", "maternalDiseases", "siblingDiseases"}},
		{&p.Immunization, []string{"vaccinationHistory", "feedingDiet"}},
	} {
		if len(*field.raw) == 0 || string(*field.raw) == "null" {
			continue
		}
		old, err := wizard.Decode(*field.raw)
		if err != nil {
			return err
		}
		value := map[string]any{}
		for _, key := range field.keys {
			value[key] = old[key]
		}
		if field.raw == &p.Personal && value["birthMeasurements"] != nil {
			birth, ok := value["birthMeasurements"].(map[string]any)
			if !ok {
				return fmt.Errorf("invalid birth measurements record")
			}
			value["birthMeasurements"] = map[string]any{"weightKg": birth["weightKg"], "heightCm": birth["heightCm"], "headCircumferenceCm": birth["headCircumferenceCm"]}
		}
		*field.raw, err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	return nil
}
func ownedPatient(ctx context.Context, tx pgx.Tx, userID string, id int64) (Patient, error) {
	p, err := scanPatient(tx.QueryRow(ctx, "SELECT "+patientColumns+" FROM patients WHERE id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, api.Failure(404, "PATIENT_NOT_FOUND", "No se encontró el paciente.")
	}
	if err != nil {
		return p, err
	}
	if p.owner != userID {
		return p, api.Failure(403, "FORBIDDEN", "No tenés permiso para acceder a este paciente.")
	}
	return p, nil
}
func (s *Service) transaction(ctx context.Context, readOnly bool, fn func(pgx.Tx) error) error {
	options := pgx.TxOptions{}
	if readOnly {
		options.AccessMode = pgx.ReadOnly
	}
	tx, err := s.pool.BeginTx(ctx, options)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Warn("clinical rollback failed")
		}
	}()
	if err := fn(tx); err != nil {
		return clinicalDBError(err)
	}
	return clinicalDBError(tx.Commit(ctx))
}
func clinicalDBError(err error) error {
	var pg *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || errors.As(err, &pg) && (strings.HasPrefix(pg.Code, "23") || pg.Code == "22001") {
		return api.Failure(409, "VERSION_CONFLICT", "La configuración cambió. Actualizá los datos antes de guardar.")
	}
	return err
}
func nullableText(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("clinical projection is not text")
	}
	return &text, nil
}

func applyPatient(p *Patient, evaluation wizard.Evaluation) error {
	projections := evaluation.Projections
	fullName, err := nullableText(projections["patient.fullName"])
	if err != nil {
		return err
	}
	if fullName == nil {
		return fmt.Errorf("patient full name projection is missing")
	}
	p.FullName = *fullName
	born, err := nullableText(projections["patient.birthDate"])
	if err != nil {
		return err
	}
	if born == nil {
		return fmt.Errorf("patient birth date projection is missing")
	}
	p.BirthDate = *born
	for key, destination := range map[string]**string{"nationalId": &p.NationalID, "gender": &p.Gender, "bloodType": &p.BloodType, "phone": &p.Phone, "email": &p.Email, "address": &p.Address} {
		value, err := nullableText(projections["patient."+key])
		if err != nil {
			return err
		}
		*destination = value
	}
	guardian := map[string]any{}
	for _, key := range []string{"fatherFullName", "motherFullName", "siblingsCount", "firstGuardianFullName", "secondGuardianFullName", "firstGuardianPhone", "secondGuardianPhone", "firstGuardianNationalId", "secondGuardianNationalId", "address", "primaryGuardian", "primaryContactPhone", "hasSiblings", "siblingOrder", "livingArrangement"} {
		guardian[key] = nil
	}
	for _, key := range []string{"fatherFullName", "motherFullName", "siblingsCount", "primaryGuardian", "primaryContactPhone", "hasSiblings", "siblingOrder", "livingArrangement"} {
		guardian[key] = projections["patient.guardianInfo."+key]
	}
	family := map[string]any{}
	if len(p.Family) > 0 {
		previous, err := wizard.Decode(p.Family)
		if err != nil {
			return err
		}
		for _, key := range []string{"familyChronicDiseases", "familyAllergies", "geneticDiseases"} {
			family[key] = previous[key]
		}
	} else {
		for _, key := range []string{"familyChronicDiseases", "familyAllergies", "geneticDiseases"} {
			family[key] = nil
		}
	}
	for _, key := range []string{"paternalDiseases", "maternalDiseases", "siblingDiseases"} {
		family[key] = projections["patient.familyHistory."+key]
	}
	provider, plan, affiliate := projections["patient.insurance"], projections["patient.insurance.plan"], projections["patient.insurance.affiliate"]
	if provider != nil || plan != nil || affiliate != nil {
		p.Insurance, err = json.Marshal(map[string]any{"providerName": provider, "planName": plan, "affiliateNumber": affiliate})
		if err != nil {
			return err
		}
	}
	p.Guardian, err = json.Marshal(guardian)
	if err != nil {
		return err
	}
	p.Family, err = json.Marshal(family)
	if err != nil {
		return err
	}
	p.Values, err = json.Marshal(evaluation.Values)
	if err != nil {
		return err
	}
	p.Results, err = json.Marshal(evaluation.Results)
	return err
}
func patientContext(reference string, projections map[string]any) (map[string]any, error) {
	var age any
	if born, ok := projections["patient.birthDate"].(string); ok {
		birth, err := time.Parse("2006-01-02", born)
		if err != nil {
			return nil, err
		}
		today, err := time.Parse("2006-01-02", reference)
		if err != nil {
			return nil, err
		}
		years := today.Year() - birth.Year()
		if today.Month() < birth.Month() || today.Month() == birth.Month() && today.Day() < birth.Day() {
			years--
		}
		age = years
	}
	return map[string]any{"referenceDate": reference, "zone": "America/Argentina/Buenos_Aires", "patient.ageYears": age, "patient.gender": projections["patient.gender"], "patient.latestWeightKg": nil, "patient.latestHeightCm": nil}, nil
}

func (s *Service) detail(ctx context.Context, tx pgx.Tx, p *Patient) error {
	rows, err := tx.Query(ctx, `SELECT weight_kg,height_cm,head_circumference_cm FROM clinical_encounters WHERE patient_id=$1 AND encounter_date<$2 AND (encounter_date AT TIME ZONE 'America/Argentina/Buenos_Aires')::date >= $3::date AND (weight_kg IS NOT NULL OR height_cm IS NOT NULL OR head_circumference_cm IS NOT NULL) ORDER BY encounter_date DESC,id DESC`, p.ID, time.Now().UTC(), p.BirthDate)
	if err != nil {
		return err
	}
	defer rows.Close()
	var measurements Measurements
	for rows.Next() {
		var row Measurements
		if err := rows.Scan(&row.Weight, &row.Height, &row.Head); err != nil {
			return err
		}
		if measurements.Weight == nil {
			measurements.Weight = row.Weight
		}
		if measurements.Height == nil {
			measurements.Height = row.Height
		}
		if measurements.Head == nil {
			measurements.Head = row.Head
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if measurements.Weight != nil || measurements.Height != nil || measurements.Head != nil {
		p.Measurements = &measurements
	}
	insurance, err := wizard.Decode(p.Insurance)
	if err != nil {
		return err
	}
	provider, _ := insurance["providerName"].(string)
	if strings.TrimSpace(provider) == "" {
		values, err := wizard.Decode(p.Values)
		if err != nil {
			return err
		}
		var provider, plan, affiliate *string
		for _, key := range []string{"sys_insurance", "insurance", "obraSocial", "obra_social", "cobertura"} {
			if text, ok := values[key].(string); ok && strings.TrimSpace(text) != "" {
				value := strings.TrimSpace(text)
				provider = &value
				break
			}
		}
		if provider != nil {
			first := func(keys ...string) *string {
				for _, key := range keys {
					if text, ok := values[key].(string); ok && strings.TrimSpace(text) != "" {
						v := strings.TrimSpace(text)
						return &v
					}
				}
				return nil
			}
			plan = first("sys_insurance_plan", "insurancePlan", "planName", "plan")
			affiliate = first("sys_insurance_affiliate", "sys_affiliate", "affiliateNumber", "nroAfiliado", "numeroAfiliado", "afiliado")
			p.Insurance, err = json.Marshal(map[string]any{"providerName": provider, "planName": plan, "affiliateNumber": affiliate})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) GetPatient(ctx context.Context, userID string, id int64) (Patient, error) {
	var result Patient
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		p, err := ownedPatient(ctx, tx, userID, id)
		if err != nil {
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
func (s *Service) CreatePatient(ctx context.Context, identity api.Identity, request map[string]any) (Patient, error) {
	var result Patient
	if err := api.Only(request, "workspaceVersion", "values"); err != nil {
		return result, err
	}
	expected, err := api.Version(map[string]any{"expectedVersion": request["workspaceVersion"]}, false)
	if err != nil {
		return result, err
	}
	values, err := api.Object(request, "values")
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		setup, err := s.workspaces.Effective(ctx, tx, identity.UserID, *expected)
		if err != nil {
			return err
		}
		admin := false
		for _, role := range identity.Roles {
			if strings.EqualFold(role, "ROLE_SUPER_ADMIN") {
				admin = true
			}
		}
		if !admin {
			plan := billing.Plan(identity.Plan)
			if plan == "NONE" {
				return api.Failure(403, "FORBIDDEN", "No se pudo completar la solicitud.")
			}
			var count int64
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM patients WHERE user_id=$1", identity.UserID).Scan(&count); err != nil {
				return err
			}
			max := s.rules.Int(plan, "max_patients", 50)
			if max != -1 && count >= int64(max) {
				return api.Failure(403, "FORBIDDEN", "No se pudo completar la solicitud.")
			}
		}
		definition, err := wizard.Decode(setup.Definition)
		if err != nil {
			return err
		}
		form, ok := definition["patientForm"].(map[string]any)
		if !ok {
			return api.Failure(409, "INVALID_WIZARD_DEFINITION", "La configuración no es válida.")
		}
		reference := time.Now().In(s.zone).Format("2006-01-02")
		context, err := patientContext(reference, map[string]any{})
		if err != nil {
			return err
		}
		validated, err := s.forms.Evaluate(form, values, context)
		if err != nil {
			return err
		}
		context, err = patientContext(reference, validated.Projections)
		if err != nil {
			return err
		}
		evaluation, err := s.forms.Evaluate(form, values, context)
		if err != nil {
			return err
		}
		p := Patient{owner: identity.UserID, WorkspaceVersion: setup.Version, Allergies: json.RawMessage(`[]`), Chronic: json.RawMessage(`[]`)}
		p.Snapshot, err = json.Marshal(form)
		if err != nil {
			return err
		}
		p.Context, err = json.Marshal(context)
		if err != nil {
			return err
		}
		if err := applyPatient(&p, evaluation); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO patients(user_id,workspace_version,full_name,birth_date,national_id,gender,blood_type,phone,email,address,guardian_info,family_history,insurance_info,form_values,formula_results,form_definition_snapshot,formula_context,allergies,chronic_conditions,version,updated_at) VALUES($1,$2,$3,$4::date,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,0,clock_timestamp()) RETURNING id,version`, p.owner, p.WorkspaceVersion, p.FullName, p.BirthDate, p.NationalID, p.Gender, p.BloodType, p.Phone, p.Email, p.Address, p.Guardian, p.Family, p.Insurance, p.Values, p.Results, p.Snapshot, p.Context, p.Allergies, p.Chronic).Scan(&p.ID, &p.Version)
		if err != nil {
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
func (s *Service) UpdatePatient(ctx context.Context, userID string, id int64, request map[string]any) (Patient, error) {
	var result Patient
	if err := api.Only(request, "expectedVersion", "values"); err != nil {
		return result, err
	}
	expected, err := api.Version(request, false)
	if err != nil {
		return result, err
	}
	values, err := api.Object(request, "values")
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		p, err := ownedPatient(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, userID, "EDIT_PATIENT"); err != nil {
			return err
		}
		if p.Version != *expected {
			return api.Failure(409, "VERSION_CONFLICT", "El registro cambió. Actualizá los datos antes de guardar.")
		}
		context, err := wizard.Decode(p.Context)
		if err != nil {
			return err
		}
		reference, ok := context["referenceDate"].(string)
		if !ok {
			return fmt.Errorf("patient saved context has no reference date")
		}
		form, err := wizard.Decode(p.Snapshot)
		if err != nil {
			return err
		}
		validated, err := s.forms.Evaluate(form, values, context)
		if err != nil {
			return err
		}
		context, err = patientContext(reference, validated.Projections)
		if err != nil {
			return err
		}
		evaluation, err := s.forms.Evaluate(form, values, context)
		if err != nil {
			return err
		}
		p.Context, err = json.Marshal(context)
		if err != nil {
			return err
		}
		if err := applyPatient(&p, evaluation); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `UPDATE patients SET full_name=$2,birth_date=$3::date,national_id=$4,gender=$5,blood_type=$6,phone=$7,email=$8,address=$9,guardian_info=$10,family_history=$11,insurance_info=$12,form_values=$13,formula_results=$14,formula_context=$15,emergency_contact=$17,personal_history=$18,perinatal_history=$19,immunization_and_nutrition=$20,allergies=$21,chronic_conditions=$22,version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND version=$16 RETURNING version`, id, p.FullName, p.BirthDate, p.NationalID, p.Gender, p.BloodType, p.Phone, p.Email, p.Address, p.Guardian, p.Family, p.Insurance, p.Values, p.Results, p.Context, *expected, p.Emergency, p.Personal, p.Perinatal, p.Immunization, p.Allergies, p.Chronic).Scan(&p.Version)
		if err != nil {
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

func (s *Service) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/doctorchart/patients", func(w http.ResponseWriter, r *http.Request) {
		request, err := api.Decode(w, r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		p, err := s.CreatePatient(r.Context(), api.User(r.Context()), request)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		w.Header().Set("Location", "/api/doctorchart/patients/"+strconv.FormatInt(p.ID, 10))
		api.WriteJSON(w, 201, p)
	})
	mux.HandleFunc("GET /api/doctorchart/patients/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			api.WriteError(w, api.Invalid("La solicitud contiene datos inválidos."))
			return
		}
		p, err := s.GetPatient(r.Context(), api.User(r.Context()).UserID, id)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		api.WriteJSON(w, 200, p)
	})
	mux.HandleFunc("PUT /api/doctorchart/patients/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			api.WriteError(w, api.Invalid("La solicitud contiene datos inválidos."))
			return
		}
		request, err := api.Decode(w, r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		p, err := s.UpdatePatient(r.Context(), api.User(r.Context()).UserID, id, request)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		api.WriteJSON(w, 200, p)
	})
}
