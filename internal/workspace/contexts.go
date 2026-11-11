package workspace

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"net/http"
	"strconv"
)

type ContextSummary struct {
	ID          int64   `json:"id"`
	WorkspaceID int64   `json:"workspaceId"`
	Version     int64   `json:"version"`
	Specialty   Summary `json:"specialty"`
	Completed   bool    `json:"onboardingCompleted"`
	Ready       bool    `json:"ready"`
	HasDraft    bool    `json:"hasDraft"`
}

func (s *Store) Contexts(ctx context.Context, user string) ([]ContextSummary, error) {
	result := []ContextSummary{}
	rows, err := s.pool.Query(ctx, `SELECT c.id,c.workspace_id,c.version,s.id,s.code,s.display_name,s.description,c.onboarding_completed,c.onboarding_completed AND c.definition IS NOT NULL,c.draft_definition IS NOT NULL FROM doctor_specialty_contexts c JOIN doctor_workspaces w ON w.id=c.workspace_id JOIN specialties s ON s.id=c.specialty_id WHERE w.user_id=$1 ORDER BY c.created_at,c.id`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c ContextSummary
		if err = rows.Scan(&c.ID, &c.WorkspaceID, &c.Version, &c.Specialty.ID, &c.Specialty.Code, &c.Specialty.Name, &c.Specialty.Description, &c.Completed, &c.Ready, &c.HasDraft); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func contextRequest(ctx context.Context, request map[string]any) (context.Context, error) {
	if raw, present := request["specialtyContextId"]; present {
		id, err := api.Integer(raw, "specialtyContextId", 1)
		if err != nil {
			return ctx, err
		}
		if prior := api.SpecialtyContextID(ctx); prior != 0 && prior != id {
			return ctx, api.Invalid("La especialidad de la ruta no coincide con la solicitud.")
		}
		return api.WithSpecialtyContext(ctx, id), nil
	}
	return ctx, nil
}
func bindContextQuery(r *http.Request) error {
	values, present := r.URL.Query()["specialtyContextId"]
	if !present {
		return nil
	}
	if len(values) != 1 {
		return api.Invalid("Indicá una única especialidad.")
	}
	id, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || id <= 0 {
		return api.Invalid("La especialidad no es válida.")
	}
	*r = *r.WithContext(api.WithSpecialtyContext(r.Context(), id))
	return nil
}
func (s *Store) AddContext(ctx context.Context, user string, request map[string]any) (*Setup, error) {
	if err := api.Only(request, "specialtyId"); err != nil {
		return nil, err
	}
	id, err := api.Integer(request["specialtyId"], "specialtyId", 1)
	if err != nil {
		return nil, err
	}
	var result *Setup
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := catalogLock(ctx, tx); err != nil {
			return err
		}
		var workspaceID int64
		if err := tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR UPDATE", user).Scan(&workspaceID); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM doctor_specialty_contexts WHERE workspace_id=$1 AND specialty_id=$2)", workspaceID, id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return api.Failure(409, "SPECIALTY_ALREADY_ADDED", "Ya agregaste esta especialidad.")
		}
		spec, err := specialty(ctx, tx, id)
		if err != nil {
			return err
		}
		d, err := defaultWizard(ctx, tx, id)
		if err != nil {
			return err
		}
		if !spec.Active || !supportedSpecialty(spec.Code) || len(d.Published) == 0 {
			return api.Failure(409, "SPECIALTY_NOT_AVAILABLE", "La especialidad no está publicada para su selección.")
		}
		definition, err := wizard.Decode(d.Published)
		if err != nil {
			return err
		}
		if err = s.validator.Validate(definition); err != nil {
			return err
		}
		w := &workspace{UserID: user, Setup: Setup{ID: workspaceID, Specialty: spec.summary(), Experience: spec.experience(), Draft: json.RawMessage(d.Published), DraftSourceVersion: &d.PublishedVersion, Step: firstStep(definition)}}
		if err = saveWorkspace(ctx, tx, w); err != nil {
			return err
		}
		result = &w.Setup
		return nil
	})
	return result, err
}
