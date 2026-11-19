package clinics

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type teleconsultationSetting struct {
	ContextID string `json:"specialtyContextId"`
	Specialty string `json:"specialty"`
	ClinicID  string `json:"clinicId,omitempty"`
	Enabled   bool   `json:"enabled"`
}

const teleconsultationSettings = `
 SELECT c.id::text, s.display_name, COALESCE(v.id::text, ''),
        c.teleconsultation_enabled AND COALESCE(v.status='ACTIVE', false)
 FROM doctor_specialty_contexts c
 JOIN specialties s ON s.id=c.specialty_id
 LEFT JOIN clinics v ON v.workspace_id=c.workspace_id AND v.modality='TELECONSULTATION'
 WHERE c.workspace_id=$1 AND c.onboarding_completed AND c.definition IS NOT NULL`

func (s *Service) listTeleconsultations(w http.ResponseWriter, r *http.Request, user string) error {
	ctx := r.Context()
	workspace, err := s.workspace(ctx, user)
	if err != nil {
		return err
	}
	rows, err := s.pool.Query(ctx, teleconsultationSettings+" ORDER BY s.display_name,c.id", workspace)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []teleconsultationSetting{}
	for rows.Next() {
		var setting teleconsultationSetting
		if err := rows.Scan(&setting.ContextID, &setting.Specialty, &setting.ClinicID, &setting.Enabled); err != nil {
			return err
		}
		items = append(items, setting)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	api.WriteJSON(w, http.StatusOK, items)
	return nil
}

func (s *Service) setTeleconsultation(w http.ResponseWriter, r *http.Request, user string) error {
	contextID, err := decimal(r.PathValue("context"), true)
	if err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	body, err := api.Decode(w, r)
	if err != nil {
		return err
	}
	if err := api.Only(body, "enabled"); err != nil {
		return err
	}
	enabled, ok := body["enabled"].(bool)
	if !ok {
		return field("enabled", "INVALID")
	}
	ctx := r.Context()
	snapshot, err := s.rights.Resolve(ctx, user)
	if err != nil {
		return unavailable()
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(clean)
	}()
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='10s'"); err != nil {
		return err
	}
	if err = s.rights.Fence(ctx, tx, snapshot); err != nil {
		return unavailable()
	}
	var workspace int64
	err = tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR UPDATE", user).Scan(&workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.Failure(409, "ONBOARDING_REQUIRED", "Completá la configuración del médico.")
	}
	if err != nil {
		return err
	}
	if err = s.rights.Fence(ctx, tx, snapshot); err != nil {
		return unavailable()
	}
	if !snapshot.ClinicManagement {
		return api.Failure(403, "CLINIC_MANAGEMENT_DISABLED", "Tu cobertura no habilita la gestión de teleconsultas.")
	}
	updated, err := tx.Exec(ctx, `
	 UPDATE doctor_specialty_contexts SET teleconsultation_enabled=$3
	 WHERE id=$1 AND workspace_id=$2 AND onboarding_completed AND definition IS NOT NULL`, contextID, workspace, enabled)
	if err != nil {
		return err
	}
	if updated.RowsAffected() == 0 {
		return api.Failure(404, "SPECIALTY_CONTEXT_NOT_FOUND", "La especialidad no está disponible para atención.")
	}
	if enabled {
		_, err = tx.Exec(ctx, `
		 INSERT INTO clinics(workspace_id,name,country,province,city,address,timezone,modality)
		 VALUES ($1,'Teleconsulta','','','','',
		   COALESCE((SELECT timezone FROM clinics WHERE workspace_id=$1 AND modality='IN_PERSON' AND status<>'ARCHIVED' ORDER BY id LIMIT 1),
		     'America/Argentina/Buenos_Aires'),'TELECONSULTATION')
		 ON CONFLICT (workspace_id) WHERE modality='TELECONSULTATION'
		 DO UPDATE SET status='ACTIVE', version=clinics.version+1, updated_at=clock_timestamp()
		 WHERE clinics.status<>'ACTIVE'`, workspace)
		if err != nil {
			return err
		}
	}
	var result teleconsultationSetting
	err = tx.QueryRow(ctx, teleconsultationSettings+" AND c.id=$2", workspace, contextID).
		Scan(&result.ContextID, &result.Specialty, &result.ClinicID, &result.Enabled)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	api.WriteJSON(w, http.StatusOK, result)
	return nil
}
