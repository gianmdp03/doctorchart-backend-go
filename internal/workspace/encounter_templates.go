package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

type EncounterTemplate struct {
	ID                int64           `json:"id"`
	WorkspaceID       int64           `json:"workspaceId"`
	ContextID         int64           `json:"specialtyContextId"`
	Name              string          `json:"name"`
	FormDefinition    json.RawMessage `json:"formDefinition"`
	SourceDefaultCode *string         `json:"sourceDefaultCode"`
	DisplayOrder      int             `json:"displayOrder"`
	Version           int64           `json:"version"`
	ArchivedAt        *string         `json:"archivedAt"`
	CreatedAt         string          `json:"createdAt"`
	UpdatedAt         string          `json:"updatedAt"`
}

type DefaultEncounterTemplateInfo struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
}

type EncounterTemplatesResponse struct {
	SpecialtyContextID int64                          `json:"specialtyContextId"`
	Specialty          Summary                        `json:"specialty"`
	Templates          []EncounterTemplate            `json:"templates"`
	ArchivedTemplates  []EncounterTemplate            `json:"archivedTemplates"`
	SystemDefaults     []DefaultEncounterTemplateInfo `json:"systemDefaults"`
}

type ResolvedEncounterTemplate struct {
	ID             int64
	Name           string
	FormDefinition json.RawMessage
	Version        int64
}

const encounterTemplateColumns = `id,workspace_id,specialty_context_id,name,form_definition,source_default_code,display_order,version,archived_at,created_at,updated_at`

func scanEncounterTemplate(row pgx.Row) (EncounterTemplate, error) {
	var t EncounterTemplate
	var archivedAt *time.Time
	var createdAt, updatedAt time.Time
	err := row.Scan(
		&t.ID, &t.WorkspaceID, &t.ContextID, &t.Name, &t.FormDefinition,
		&t.SourceDefaultCode, &t.DisplayOrder, &t.Version, &archivedAt, &createdAt, &updatedAt,
	)
	if err != nil {
		return t, err
	}
	if archivedAt != nil {
		formatted := api.FormatInstant(*archivedAt)
		t.ArchivedAt = &formatted
	}
	t.CreatedAt = api.FormatInstant(createdAt)
	t.UpdatedAt = api.FormatInstant(updatedAt)
	return t, nil
}

func normalizeEncounterFormRaw(raw json.RawMessage) (json.RawMessage, error) {
	decoded, err := wizard.Decode(raw)
	if err != nil {
		return nil, err
	}
	if decoded == nil {
		return nil, api.Invalid("La definición de historia clínica no es válida.")
	}
	normalized := wizard.EnsureEncounterDiseaseField(wizard.WithClinicalHistory(decoded))
	return json.Marshal(normalized)
}

func normalizeEncounterFormMap(form map[string]any) (map[string]any, json.RawMessage, error) {
	raw, err := json.Marshal(form)
	if err != nil {
		return nil, nil, err
	}
	normalizedRaw, err := normalizeEncounterFormRaw(raw)
	if err != nil {
		return nil, nil, err
	}
	normalizedMap, err := wizard.Decode(normalizedRaw)
	if err != nil {
		return nil, nil, err
	}
	return normalizedMap, normalizedRaw, nil
}

func validateTemplateName(request map[string]any) (string, error) {
	name, err := api.Text(request, "name", 120)
	if err != nil {
		return "", api.Invalid("Ingresá un nombre de plantilla de entre 1 y 120 caracteres.")
	}
	name = api.JavaTrim(name)
	if name == "" || api.JavaLength(name) > 120 {
		return "", api.Invalid("Ingresá un nombre de plantilla de entre 1 y 120 caracteres.")
	}
	return name, nil
}

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func seedContextEncounterTemplatesTx(ctx context.Context, tx pgx.Tx, workspaceID, contextID, specialtyID int64, publishedDef json.RawMessage, replaceIfUncompleted bool) error {
	if replaceIfUncompleted {
		if _, err := tx.Exec(ctx, `DELETE FROM doctor_encounter_templates WHERE specialty_context_id=$1 AND workspace_id=$2`, contextID, workspaceID); err != nil {
			return err
		}
	}
	var existing int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM doctor_encounter_templates WHERE specialty_context_id=$1`, contextID).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	rows, err := tx.Query(ctx, `SELECT code,name,form_definition,display_order FROM specialty_encounter_template_defaults WHERE specialty_id=$1 AND active=true ORDER BY display_order,id`, specialtyID)
	if err != nil {
		return err
	}
	type defaultRow struct {
		code  string
		name  string
		form  json.RawMessage
		order int
	}
	defaults := []defaultRow{}
	for rows.Next() {
		var d defaultRow
		if err := rows.Scan(&d.code, &d.name, &d.form, &d.order); err != nil {
			rows.Close()
			return err
		}
		defaults = append(defaults, d)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(defaults) > 0 {
		for _, d := range defaults {
			normalized, err := normalizeEncounterFormRaw(d.form)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO doctor_encounter_templates(workspace_id,specialty_context_id,name,form_definition,source_default_code,display_order,version,created_at,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,0,clock_timestamp(),clock_timestamp())`,
				workspaceID, contextID, d.name, normalized, d.code, d.order); err != nil {
				return err
			}
		}
		return nil
	}

	defMap, err := wizard.Decode(publishedDef)
	if err != nil {
		return err
	}
	encForm, ok := defMap["encounterForm"].(map[string]any)
	if !ok {
		return nil
	}
	_, normalized, err := normalizeEncounterFormMap(encForm)
	if err != nil {
		return err
	}
	defaultCode := "default"
	_, err = tx.Exec(ctx, `INSERT INTO doctor_encounter_templates(workspace_id,specialty_context_id,name,form_definition,source_default_code,display_order,version,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,0,0,clock_timestamp(),clock_timestamp())`,
		workspaceID, contextID, "Historia clínica general", normalized, defaultCode)
	return err
}

func ensureExistingContextTemplatesTx(ctx context.Context, tx pgx.Tx, w *workspace) error {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM doctor_encounter_templates WHERE specialty_context_id=$1`, w.ContextID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM doctor_workspaces WHERE id=$1 FOR UPDATE`, w.ID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM doctor_encounter_templates WHERE specialty_context_id=$1`, w.ContextID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	sourceDef := w.Definition
	if len(sourceDef) == 0 {
		sourceDef = w.Draft
	}
	if len(sourceDef) == 0 {
		return nil
	}
	defMap, err := wizard.Decode(sourceDef)
	if err != nil {
		return err
	}
	encForm, ok := defMap["encounterForm"].(map[string]any)
	if !ok {
		return nil
	}
	_, normalized, err := normalizeEncounterFormMap(encForm)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO doctor_encounter_templates(workspace_id,specialty_context_id,name,form_definition,source_default_code,display_order,version,created_at,updated_at)
		VALUES($1,$2,$3,$4,NULL,0,0,clock_timestamp(),clock_timestamp())`,
		w.ID, w.ContextID, "Historia clínica general", normalized)
	return err
}

func readEncounterTemplatesTx(ctx context.Context, tx pgx.Tx, w *workspace) (EncounterTemplatesResponse, error) {
	result := EncounterTemplatesResponse{
		SpecialtyContextID: w.ContextID,
		Specialty:          w.Specialty,
		Templates:          []EncounterTemplate{},
		ArchivedTemplates:  []EncounterTemplate{},
		SystemDefaults:     []DefaultEncounterTemplateInfo{},
	}
	rows, err := tx.Query(ctx, `SELECT `+encounterTemplateColumns+` FROM doctor_encounter_templates WHERE specialty_context_id=$1 AND workspace_id=$2 ORDER BY display_order,id`, w.ContextID, w.ID)
	if err != nil {
		return result, err
	}
	installedCodes := map[string]bool{}
	for rows.Next() {
		t, err := scanEncounterTemplate(rows)
		if err != nil {
			rows.Close()
			return result, err
		}
		if t.SourceDefaultCode != nil && *t.SourceDefaultCode != "" {
			installedCodes[*t.SourceDefaultCode] = true
		}
		if t.ArchivedAt != nil {
			result.ArchivedTemplates = append(result.ArchivedTemplates, t)
		} else {
			result.Templates = append(result.Templates, t)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()

	defRows, err := tx.Query(ctx, `SELECT code,name,description FROM specialty_encounter_template_defaults WHERE specialty_id=$1 AND active=true ORDER BY display_order,id`, w.Specialty.ID)
	if err != nil {
		return result, err
	}
	defer defRows.Close()
	for defRows.Next() {
		var info DefaultEncounterTemplateInfo
		if err := defRows.Scan(&info.Code, &info.Name, &info.Description); err != nil {
			return result, err
		}
		info.Installed = installedCodes[info.Code]
		result.SystemDefaults = append(result.SystemDefaults, info)
	}
	return result, defRows.Err()
}

func (s *Store) ListEncounterTemplates(ctx context.Context, userID string) (EncounterTemplatesResponse, error) {
	var result EncounterTemplatesResponse
	err := s.transaction(ctx, false, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := requireWorkspace(w); err != nil {
			return err
		}
		if err := ensureExistingContextTemplatesTx(ctx, tx, w); err != nil {
			return err
		}
		result, err = readEncounterTemplatesTx(ctx, tx, w)
		return err
	})
	return result, err
}

func (s *Store) CreateEncounterTemplate(ctx context.Context, userID string, request map[string]any) (EncounterTemplate, error) {
	var result EncounterTemplate
	var scopeErr error
	ctx, scopeErr = contextRequest(ctx, request)
	if scopeErr != nil {
		return result, scopeErr
	}
	if err := api.Only(request, "specialtyContextId", "name", "baseTemplateId", "sourceTemplateId", "formDefinition"); err != nil {
		return result, err
	}
	name, err := validateTemplateName(request)
	if err != nil {
		return result, err
	}
	var baseTemplateID *int64
	for _, key := range []string{"baseTemplateId", "sourceTemplateId"} {
		if raw, present := request[key]; present && raw != nil {
			id, err := api.Integer(raw, key, 1)
			if err != nil {
				return result, err
			}
			baseTemplateID = &id
			break
		}
	}
	var normalizedRaw json.RawMessage
	if rawForm, hasForm := request["formDefinition"]; hasForm && rawForm != nil {
		formDef, err := api.Object(request, "formDefinition")
		if err != nil {
			return result, err
		}
		var normalizedMap map[string]any
		normalizedMap, normalizedRaw, err = normalizeEncounterFormMap(formDef)
		if err != nil {
			return result, err
		}
		if err := s.validator.ValidateEncounterForm(normalizedMap); err != nil {
			return result, err
		}
	} else if baseTemplateID == nil {
		return result, api.Invalid("Indicá una plantilla base o una definición de historia clínica.")
	}

	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := requireWorkspace(w); err != nil {
			return err
		}
		if err := authorizeContext(w, request); err != nil {
			return err
		}
		if err := ensureExistingContextTemplatesTx(ctx, tx, w); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT id FROM doctor_workspaces WHERE id=$1 AND user_id=$2 FOR UPDATE`, w.ID, userID); err != nil {
			return err
		}
		if baseTemplateID != nil {
			var archived *time.Time
			var baseFormRaw json.RawMessage
			err := tx.QueryRow(ctx, `SELECT archived_at,form_definition FROM doctor_encounter_templates WHERE id=$1 AND specialty_context_id=$2 AND workspace_id=$3`, *baseTemplateID, w.ContextID, w.ID).Scan(&archived, &baseFormRaw)
			if errors.Is(err, pgx.ErrNoRows) {
				return api.Failure(404, "ENCOUNTER_TEMPLATE_NOT_FOUND", "La plantilla base no está disponible.")
			}
			if err != nil {
				return err
			}
			if archived != nil {
				return api.Failure(409, "ENCOUNTER_TEMPLATE_ARCHIVED", "La plantilla base está archivada y no se puede utilizar.")
			}
			if len(normalizedRaw) == 0 {
				var err error
				normalizedRaw, err = normalizeEncounterFormRaw(baseFormRaw)
				if err != nil {
					return err
				}
			}
		}
		var nextOrder int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(display_order), -1) + 1 FROM doctor_encounter_templates WHERE specialty_context_id=$1`, w.ContextID).Scan(&nextOrder); err != nil {
			return err
		}
		result, err = scanEncounterTemplate(tx.QueryRow(ctx, `INSERT INTO doctor_encounter_templates(workspace_id,specialty_context_id,name,form_definition,source_default_code,display_order,version,created_at,updated_at)
			VALUES($1,$2,$3,$4,NULL,$5,0,clock_timestamp(),clock_timestamp()) RETURNING `+encounterTemplateColumns,
			w.ID, w.ContextID, name, normalizedRaw, nextOrder))
		if isUniqueViolation(err) {
			return api.Failure(409, "TEMPLATE_NAME_EXISTS", "Ya existe una plantilla activa con ese nombre en esta especialidad.")
		}
		return err
	})
	return result, err
}

func (s *Store) UpdateEncounterTemplate(ctx context.Context, userID string, id int64, request map[string]any) (EncounterTemplate, error) {
	var result EncounterTemplate
	var scopeErr error
	ctx, scopeErr = contextRequest(ctx, request)
	if scopeErr != nil {
		return result, scopeErr
	}
	if err := api.Only(request, "specialtyContextId", "expectedVersion", "name", "formDefinition"); err != nil {
		return result, err
	}
	expected, err := api.Version(request, false)
	if err != nil {
		return result, err
	}
	name, err := validateTemplateName(request)
	if err != nil {
		return result, err
	}
	formDef, err := api.Object(request, "formDefinition")
	if err != nil {
		return result, err
	}
	normalizedMap, normalizedRaw, err := normalizeEncounterFormMap(formDef)
	if err != nil {
		return result, err
	}

	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := requireWorkspace(w); err != nil {
			return err
		}
		if err := authorizeContext(w, request); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT id FROM doctor_workspaces WHERE id=$1 AND user_id=$2 FOR UPDATE`, w.ID, userID); err != nil {
			return err
		}
		current, err := scanEncounterTemplate(tx.QueryRow(ctx, `SELECT `+encounterTemplateColumns+` FROM doctor_encounter_templates WHERE id=$1 AND specialty_context_id=$2 AND workspace_id=$3 FOR UPDATE`, id, w.ContextID, w.ID))
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(404, "ENCOUNTER_TEMPLATE_NOT_FOUND", "No se encontró la plantilla de historia clínica.")
		}
		if err != nil {
			return err
		}
		if current.ArchivedAt != nil {
			return api.Failure(409, "ENCOUNTER_TEMPLATE_ARCHIVED", "La plantilla está archivada. Restaurala antes de editarla.")
		}
		if err := version(current.Version, expected, workspaceConflict); err != nil {
			return err
		}
		previousMap, err := wizard.Decode(current.FormDefinition)
		if err != nil {
			return err
		}
		if err := s.validator.ValidateEncounterFormTransition(previousMap, normalizedMap); err != nil {
			return err
		}
		result, err = scanEncounterTemplate(tx.QueryRow(ctx, `UPDATE doctor_encounter_templates
			SET name=$3,form_definition=$4,version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond')
			WHERE id=$1 AND specialty_context_id=$2 AND version=$5
			RETURNING `+encounterTemplateColumns,
			id, w.ContextID, name, normalizedRaw, *expected))
		if isUniqueViolation(err) {
			return api.Failure(409, "TEMPLATE_NAME_EXISTS", "Ya existe una plantilla activa con ese nombre en esta especialidad.")
		}
		return err
	})
	return result, err
}

func (s *Store) ArchiveEncounterTemplate(ctx context.Context, userID string, id int64, request map[string]any) (EncounterTemplate, error) {
	var result EncounterTemplate
	var scopeErr error
	ctx, scopeErr = contextRequest(ctx, request)
	if scopeErr != nil {
		return result, scopeErr
	}
	if err := api.Only(request, "specialtyContextId", "expectedVersion"); err != nil {
		return result, err
	}
	expected, err := api.Version(request, false)
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := requireWorkspace(w); err != nil {
			return err
		}
		if err := authorizeContext(w, request); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT id FROM doctor_workspaces WHERE id=$1 AND user_id=$2 FOR UPDATE`, w.ID, userID); err != nil {
			return err
		}
		current, err := scanEncounterTemplate(tx.QueryRow(ctx, `SELECT `+encounterTemplateColumns+` FROM doctor_encounter_templates WHERE id=$1 AND specialty_context_id=$2 AND workspace_id=$3 FOR UPDATE`, id, w.ContextID, w.ID))
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(404, "ENCOUNTER_TEMPLATE_NOT_FOUND", "No se encontró la plantilla de historia clínica.")
		}
		if err != nil {
			return err
		}
		if current.ArchivedAt != nil {
			return api.Failure(409, "ENCOUNTER_TEMPLATE_ARCHIVED", "La plantilla ya está archivada.")
		}
		if err := version(current.Version, expected, workspaceConflict); err != nil {
			return err
		}
		var activeCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM doctor_encounter_templates WHERE specialty_context_id=$1 AND archived_at IS NULL`, w.ContextID).Scan(&activeCount); err != nil {
			return err
		}
		if activeCount <= 1 {
			return api.Failure(409, "LAST_ACTIVE_TEMPLATE", "Debe quedar al menos una plantilla activa para crear historias clínicas.")
		}
		result, err = scanEncounterTemplate(tx.QueryRow(ctx, `UPDATE doctor_encounter_templates
			SET archived_at=clock_timestamp(),version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond')
			WHERE id=$1 AND specialty_context_id=$2 AND version=$3
			RETURNING `+encounterTemplateColumns, id, w.ContextID, *expected))
		return err
	})
	return result, err
}

func (s *Store) RestoreEncounterTemplate(ctx context.Context, userID string, id int64, request map[string]any) (EncounterTemplate, error) {
	var result EncounterTemplate
	var scopeErr error
	ctx, scopeErr = contextRequest(ctx, request)
	if scopeErr != nil {
		return result, scopeErr
	}
	if err := api.Only(request, "specialtyContextId", "expectedVersion"); err != nil {
		return result, err
	}
	expected, err := api.Version(request, false)
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := requireWorkspace(w); err != nil {
			return err
		}
		if err := authorizeContext(w, request); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT id FROM doctor_workspaces WHERE id=$1 AND user_id=$2 FOR UPDATE`, w.ID, userID); err != nil {
			return err
		}
		current, err := scanEncounterTemplate(tx.QueryRow(ctx, `SELECT `+encounterTemplateColumns+` FROM doctor_encounter_templates WHERE id=$1 AND specialty_context_id=$2 AND workspace_id=$3 FOR UPDATE`, id, w.ContextID, w.ID))
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(404, "ENCOUNTER_TEMPLATE_NOT_FOUND", "No se encontró la plantilla de historia clínica.")
		}
		if err != nil {
			return err
		}
		if current.ArchivedAt == nil {
			return api.Failure(409, "ENCOUNTER_TEMPLATE_ACTIVE", "La plantilla ya está activa.")
		}
		if err := version(current.Version, expected, workspaceConflict); err != nil {
			return err
		}
		result, err = scanEncounterTemplate(tx.QueryRow(ctx, `UPDATE doctor_encounter_templates
			SET archived_at=NULL,version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond')
			WHERE id=$1 AND specialty_context_id=$2 AND version=$3
			RETURNING `+encounterTemplateColumns, id, w.ContextID, *expected))
		if isUniqueViolation(err) {
			return api.Failure(409, "TEMPLATE_NAME_EXISTS", "Ya existe una plantilla activa con ese nombre en esta especialidad.")
		}
		return err
	})
	return result, err
}

func (s *Store) ImportDefaultEncounterTemplates(ctx context.Context, userID string, request map[string]any) (EncounterTemplatesResponse, error) {
	var result EncounterTemplatesResponse
	var scopeErr error
	ctx, scopeErr = contextRequest(ctx, request)
	if scopeErr != nil {
		return result, scopeErr
	}
	if err := api.Only(request, "specialtyContextId"); err != nil {
		return result, err
	}
	err := s.transaction(ctx, false, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := requireWorkspace(w); err != nil {
			return err
		}
		if err := authorizeContext(w, request); err != nil {
			return err
		}
		if err := ensureExistingContextTemplatesTx(ctx, tx, w); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT id FROM doctor_workspaces WHERE id=$1 AND user_id=$2 FOR UPDATE`, w.ID, userID); err != nil {
			return err
		}
		existingCodes := map[string]bool{}
		activeNames := map[string]bool{}
		maxOrder := -1
		rows, err := tx.Query(ctx, `SELECT name,source_default_code,display_order,archived_at FROM doctor_encounter_templates WHERE specialty_context_id=$1`, w.ContextID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var tName string
			var code *string
			var ord int
			var archived *time.Time
			if err := rows.Scan(&tName, &code, &ord, &archived); err != nil {
				rows.Close()
				return err
			}
			if code != nil && *code != "" {
				existingCodes[*code] = true
			}
			if archived == nil {
				activeNames[strings.ToLower(strings.TrimSpace(tName))] = true
			}
			if ord > maxOrder {
				maxOrder = ord
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		defRows, err := tx.Query(ctx, `SELECT code,name,form_definition FROM specialty_encounter_template_defaults WHERE specialty_id=$1 AND active=true ORDER BY display_order,id`, w.Specialty.ID)
		if err != nil {
			return err
		}
		type pendingDefault struct {
			code, name string
			form       json.RawMessage
		}
		toInsert := []pendingDefault{}
		for defRows.Next() {
			var item pendingDefault
			if err := defRows.Scan(&item.code, &item.name, &item.form); err != nil {
				defRows.Close()
				return err
			}
			if !existingCodes[item.code] {
				toInsert = append(toInsert, item)
			}
		}
		if err := defRows.Err(); err != nil {
			defRows.Close()
			return err
		}
		defRows.Close()

		for _, item := range toInsert {
			normalized, err := normalizeEncounterFormRaw(item.form)
			if err != nil {
				return err
			}
			candidate := item.name
			if activeNames[strings.ToLower(strings.TrimSpace(candidate))] {
				candidate = item.name + " (predeterminada)"
				suffix := 2
				for activeNames[strings.ToLower(strings.TrimSpace(candidate))] {
					candidate = fmt.Sprintf("%s (predeterminada %d)", item.name, suffix)
					suffix++
				}
			}
			activeNames[strings.ToLower(strings.TrimSpace(candidate))] = true
			maxOrder++
			if _, err := tx.Exec(ctx, `INSERT INTO doctor_encounter_templates(workspace_id,specialty_context_id,name,form_definition,source_default_code,display_order,version,created_at,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,0,clock_timestamp(),clock_timestamp())`,
				w.ID, w.ContextID, candidate, normalized, item.code, maxOrder); err != nil {
				return err
			}
		}
		result, err = readEncounterTemplatesTx(ctx, tx, w)
		return err
	})
	return result, err
}

// ResolveEncounterTemplate selects and locks the encounter template inside the
// clinical transaction, enforcing ownership, context isolation, and manual choice.
func (s *Store) ResolveEncounterTemplate(ctx context.Context, tx pgx.Tx, userID string, contextID int64, templateID *int64, templateVersion *int64) (*ResolvedEncounterTemplate, error) {
	if templateID != nil {
		var t ResolvedEncounterTemplate
		var archivedAt *time.Time
		err := tx.QueryRow(ctx, `SELECT t.id,t.name,t.form_definition,t.version,t.archived_at
			FROM doctor_encounter_templates t
			JOIN doctor_workspaces w ON w.id=t.workspace_id
			WHERE t.id=$1 AND t.specialty_context_id=$2 AND w.user_id=$3 FOR SHARE`,
			*templateID, contextID, userID).Scan(&t.ID, &t.Name, &t.FormDefinition, &t.Version, &archivedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, api.Failure(404, "ENCOUNTER_TEMPLATE_NOT_FOUND", "La plantilla de historia clínica seleccionada no pertenece a esta especialidad.")
		}
		if err != nil {
			return nil, err
		}
		if archivedAt != nil {
			return nil, api.Failure(409, "ENCOUNTER_TEMPLATE_ARCHIVED", "La plantilla seleccionada fue archivada. Elegí otra plantilla activa.")
		}
		if templateVersion != nil && t.Version != *templateVersion {
			return nil, api.Failure(409, "TEMPLATE_VERSION_CONFLICT", "La plantilla seleccionada cambió. Volvé a cargarla antes de guardar.")
		}
		return &t, nil
	}

	rows, err := tx.Query(ctx, `SELECT t.id,t.name,t.form_definition,t.version
		FROM doctor_encounter_templates t
		JOIN doctor_workspaces w ON w.id=t.workspace_id
		WHERE t.specialty_context_id=$1 AND w.user_id=$2 AND t.archived_at IS NULL
		ORDER BY t.display_order,t.id LIMIT 2`, contextID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	active := []ResolvedEncounterTemplate{}
	for rows.Next() {
		var t ResolvedEncounterTemplate
		if err := rows.Scan(&t.ID, &t.Name, &t.FormDefinition, &t.Version); err != nil {
			return nil, err
		}
		active = append(active, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(active) == 0 {
		return nil, nil
	}
	if len(active) > 1 {
		return nil, api.Failure(400, "ENCOUNTER_TEMPLATE_REQUIRED", "Seleccioná manualmente la plantilla de historia clínica a utilizar.")
	}
	if templateVersion != nil && active[0].Version != *templateVersion {
		return nil, api.Failure(409, "TEMPLATE_VERSION_CONFLICT", "La plantilla seleccionada cambió. Volvé a cargarla antes de guardar.")
	}
	return &active[0], nil
}
