package workspace

import (
	"context"
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
		result = specialty.experience()
		return nil
	})
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
		column := "navigation"
		switch key {
		case "patientRecordTabs":
			column = "patient_record_tabs"
		case "patientRecordActions":
			column = "patient_record_actions"
		}
		row, err := scanSpecialty(tx.QueryRow(ctx, "UPDATE specialties SET "+column+"=$2,version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND version=$3 RETURNING "+specialtyColumns, id, value, *expected))
		if err != nil {
			return err
		}
		result = row.experience()
		return nil
	})
	return result, err
}
