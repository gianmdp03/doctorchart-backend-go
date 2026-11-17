package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

var navigation = []string{"DASHBOARD", "PATIENTS", "CALCULATORS", "VACCINATION", "WIZARD"}
var tabs = []string{"SUMMARY", "ENCOUNTERS", "VACCINATION", "GROWTH", "PRESCRIPTIONS", "STUDIES"}
var actions = []string{"EDIT_PATIENT", "NEW_ENCOUNTER", "VIEW_GROWTH", "PRINT_DOCUMENT"}

func experienceList(value any, key string) ([]string, error) {
	catalog := navigation
	required := []string{"DASHBOARD", "PATIENTS", "WIZARD"}
	message := "La navegación debe contener Inicio, Pacientes y Personalizar Fichas, sin opciones desconocidas o repetidas."
	if key == "patientRecordTabs" {
		catalog = tabs
		required = []string{"SUMMARY", "ENCOUNTERS"}
		message = "Las pestañas deben contener Datos del paciente y Consultas, sin módulos desconocidos o repetidos."
	}
	if key == "patientRecordActions" {
		catalog = actions
		required = nil
		message = "Las acciones deben ser una lista de opciones conocidas, sin repeticiones."
	}
	invalid := api.Failure(400, "INVALID_EXPERIENCE", message)
	entries, ok := value.([]any)
	if !ok || len(entries) > len(catalog) {
		return nil, invalid
	}
	result := []string{}
	for _, entry := range entries {
		v, ok := entry.(string)
		if !ok || !contains(catalog, v) || contains(result, v) {
			return nil, invalid
		}
		result = append(result, v)
	}
	for _, v := range required {
		if !contains(result, v) {
			return nil, invalid
		}
	}
	return result, nil
}
func (s *Store) Experience(ctx context.Context, id int64) (Experience, error) {
	var result Experience
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		specialty, err := specialty(ctx, tx, id)
		if err != nil {
			if failure, ok := err.(*api.Error); ok && failure.Status == 404 {
				return api.Failure(404, "SPECIALTY_NOT_FOUND", "La especialidad no existe.")
			}
			return err
		}
		result, err = draftExperience(ctx, tx, specialty)
		return err
	})
	return result, err
}

func draftExperience(ctx context.Context, tx pgx.Tx, specialty Specialty) (Experience, error) {
	result := specialty.experience()
	var raw []byte
	err := tx.QueryRow(ctx, "SELECT draft_experience FROM wizard_defaults WHERE specialty_id=$1", specialty.ID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err == nil && len(raw) > 0 {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}
func (s *Store) UpdateExperience(ctx context.Context, id int64, key string, request map[string]any) (Experience, error) {
	var result Experience
	if err := api.Only(request, "expectedVersion", key); err != nil {
		return result, err
	}
	expected, err := api.Version(request, false)
	if err != nil {
		return result, err
	}
	value, err := experienceList(request[key], key)
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := catalogLock(ctx, tx); err != nil {
			return err
		}
		specialty, err := specialty(ctx, tx, id)
		if err != nil {
			if failure, ok := err.(*api.Error); ok && failure.Status == 404 {
				return api.Failure(404, "SPECIALTY_NOT_FOUND", "La especialidad no existe.")
			}
			return err
		}
		if err := version(specialty.Version, expected, "La especialidad cambió. Volvé a cargarla antes de guardar."); err != nil {
			return err
		}
		draft, err := draftExperience(ctx, tx, specialty)
		if err != nil {
			return err
		}
		switch key {
		case "navigation":
			draft.Navigation = value
		case "patientRecordTabs":
			draft.Tabs = value
		case "patientRecordActions":
			draft.Actions = value
		}
		tag, err := tx.Exec(ctx, `UPDATE wizard_defaults SET draft_experience=public.template_experience($2,$3,$4),
			version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE specialty_id=$1`, id, draft.Navigation, draft.Tabs, draft.Actions)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return api.Failure(409, "SPECIALTY_NOT_AVAILABLE", "La especialidad no tiene un borrador.")
		}
		err = tx.QueryRow(ctx, `UPDATE specialties SET version=version+1,
			updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 RETURNING version`, id).Scan(&draft.Version)
		result = draft
		return err
	})
	return result, err
}
