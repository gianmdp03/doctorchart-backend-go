package clinics

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func clinicContextIDs(body map[string]any) ([]int64, error) {
	raw, present := body["specialtyContextIds"]
	if !present {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok || len(values) == 0 || len(values) > 100 {
		return nil, field("specialtyContextIds", "INVALID_SPECIALTIES")
	}
	ids := make([]int64, 0, len(values))
	seen := make(map[int64]bool, len(values))
	for _, value := range values {
		text, ok := value.(string)
		id, err := decimal(text, true)
		if !ok || err != nil || seen[id] {
			return nil, field("specialtyContextIds", "INVALID_SPECIALTIES")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

func setClinicContexts(ctx context.Context, tx pgx.Tx, workspace int64, clinic string, ids []int64) error {
	var ready int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM doctor_specialty_contexts
		WHERE workspace_id=$1 AND onboarding_completed AND definition IS NOT NULL AND id=ANY($2)`, workspace, ids).Scan(&ready); err != nil {
		return err
	}
	if ready != len(ids) {
		return api.Invalid("Elegí especialidades configuradas de tu espacio de trabajo.")
	}
	var blocked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schedule_blocks
		WHERE clinic_id=$1 AND NOT (specialty_context_id=ANY($2)))`, clinic, ids).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return api.Failure(409, "CLINIC_SPECIALTY_HAS_HOURS", "Todavía hay horarios de una especialidad que quitaste. Ajustá la agenda de esa especialidad antes de quitarla del consultorio.")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM clinic_specialty_contexts
		WHERE clinic_id=$1 AND NOT (specialty_context_id=ANY($2))`, clinic, ids); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO clinic_specialty_contexts(clinic_id, workspace_id, specialty_context_id)
		SELECT $1, $2, id FROM unnest($3::bigint[]) AS chosen(id) ON CONFLICT DO NOTHING`, clinic, workspace, ids)
	return err
}

func validateClinicScheduleContexts(clinic Clinic, schedule ScheduleConfig) error {
	if len(clinic.SpecialtyContextIDs) == 0 {
		return nil
	}
	check := func(ranges []ScheduleRange) error {
		for _, r := range ranges {
			if !slices.Contains(clinic.SpecialtyContextIDs, r.ContextID) {
				return api.Failure(400, "CLINIC_SPECIALTY_NOT_ASSIGNED", "Esa especialidad no está asignada a este consultorio. Agregala desde Editar consultorio para cargar sus horarios.")
			}
		}
		return nil
	}
	for _, day := range schedule.Weekly {
		if err := check(day.Ranges); err != nil {
			return err
		}
	}
	for _, exception := range schedule.Exceptions {
		if err := check(exception.Ranges); err != nil {
			return err
		}
	}
	return nil
}
