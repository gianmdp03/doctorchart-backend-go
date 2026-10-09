package workspace

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

func patientKeys(definition map[string]any, enabled bool) []string {
	keys := []string{}
	if definition == nil {
		return keys
	}
	form, _ := definition["patientForm"].(map[string]any)
	categories, _ := form["categories"].([]any)
	for _, category := range categories {
		data, _ := category.(map[string]any)
		if !enabled || data["enabled"] == true {
			key, _ := data["key"].(string)
			keys = append(keys, key)
		}
	}
	return keys
}
func firstStep(definition map[string]any) string {
	keys := patientKeys(definition, true)
	if len(keys) > 0 {
		return "patient:" + keys[0]
	}
	return "encounter"
}

var stepPattern = regexp.MustCompile(`^(specialty|encounter|patient:[A-Za-z][A-Za-z0-9_-]{0,79})$`)

func normalizeStep(step string, previous, next map[string]any) (string, error) {
	if !stepPattern.MatchString(step) {
		return "", api.Failure(400, "INVALID_STEP", "El paso del wizard no es válido.")
	}
	if !strings.HasPrefix(step, "patient:") {
		return step, nil
	}
	key := strings.TrimPrefix(step, "patient:")
	newKeys := patientKeys(next, true)
	if contains(newKeys, key) {
		return step, nil
	}
	oldKeys := patientKeys(previous, false)
	index := -1
	for i, k := range oldKeys {
		if k == key {
			index = i
			break
		}
	}
	if index < 0 && !contains(patientKeys(next, false), key) {
		return "", api.Failure(400, "INVALID_STEP", "La categoría indicada no pertenece al wizard.")
	}
	if index < 0 {
		return "encounter", nil
	}
	for _, k := range oldKeys[index+1:] {
		if contains(newKeys, k) {
			return "patient:" + k, nil
		}
	}
	return "encounter", nil
}

func (s *Store) Select(ctx context.Context, userID string, request map[string]any) (*Setup, error) {
	if err := api.Only(request, "specialtyId", "expectedVersion"); err != nil {
		return nil, err
	}
	id, err := api.Integer(request["specialtyId"], "specialtyId", 1)
	if err != nil {
		return nil, err
	}
	expected, err := api.Version(request, true)
	if err != nil {
		return nil, err
	}
	var result *Setup
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := catalogLock(ctx, tx); err != nil {
			return err
		}
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if w == nil {
			if expected != nil {
				return api.Failure(409, "VERSION_CONFLICT", "La configuración todavía no existe.")
			}
		} else {
			if err := version(w.Setup.Version, expected, workspaceConflict); err != nil {
				return err
			}
			if w.Completed {
				return api.Failure(409, "SPECIALTY_LOCKED", "La especialidad no se cambia después de finalizar la configuración.")
			}
		}
		d, err := defaultWizard(ctx, tx, id)
		if err != nil {
			if failure, ok := err.(*api.Error); ok && failure.Status == 404 {
				return api.Failure(409, "SPECIALTY_NOT_AVAILABLE", "La especialidad no está disponible.")
			}
			return err
		}
		specialty, err := specialty(ctx, tx, id)
		if err != nil {
			return err
		}
		if !specialty.Active || len(d.Published) == 0 {
			return api.Failure(409, "SPECIALTY_NOT_AVAILABLE", "La especialidad no está disponible.")
		}
		definition, err := wizard.Decode(d.Published)
		if err != nil {
			return err
		}
		if err := s.validator.Validate(definition); err != nil {
			return err
		}
		if w == nil {
			w = &workspace{UserID: userID}
		}
		w.Specialty = specialty.summary()
		w.Experience = specialty.experience()
		w.Draft = d.Published
		w.DraftSourceVersion = &d.PublishedVersion
		w.Step = firstStep(definition)
		if err := saveWorkspace(ctx, tx, w); err != nil {
			return err
		}
		result = &w.Setup
		return nil
	})
	return result, err
}

func (s *Store) Edit(ctx context.Context, userID, operation string, request map[string]any) (*Setup, error) {
	allowed := []string{"expectedVersion"}
	if operation == "draft" {
		allowed = append(allowed, "definition", "currentStepKey")
	}
	if err := api.Only(request, allowed...); err != nil {
		return nil, err
	}
	expected, err := api.Version(request, false)
	if err != nil {
		return nil, err
	}
	var definition map[string]any
	var step string
	if operation == "draft" {
		definition, err = api.Object(request, "definition")
		if err != nil {
			return nil, err
		}
		step, err = api.Text(request, "currentStepKey", 96)
		if err != nil {
			return nil, err
		}
	}
	var result *Setup
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := requireWorkspace(w); err != nil {
			return err
		}
		if err := version(w.Setup.Version, expected, workspaceConflict); err != nil {
			return err
		}
		switch operation {
		case "draft":
			previousRaw := w.Draft
			if len(previousRaw) == 0 {
				previousRaw = w.Definition
				w.DraftSourceVersion = w.SourceVersion
			}
			previous, err := wizard.Decode(previousRaw)
			if err != nil {
				return err
			}
			if err := s.validator.Transition(previous, definition); err != nil {
				return err
			}
			step, err = normalizeStep(step, previous, definition)
			if err != nil {
				return err
			}
			w.Step = step
			w.Draft, err = json.Marshal(definition)
			if err != nil {
				return err
			}
		case "finish":
			if len(w.Draft) == 0 {
				return api.Failure(409, "NO_DRAFT_TO_APPLY", "No hay un borrador para aplicar.")
			}
			draft, err := wizard.Decode(w.Draft)
			if err != nil {
				return err
			}
			if err := s.validator.Validate(draft); err != nil {
				return err
			}
			w.Definition = w.Draft
			w.SourceVersion = w.DraftSourceVersion
			w.Draft = nil
			w.DraftSourceVersion = nil
			w.Completed = true
			w.Step = "completed"
		case "discard":
			if !w.Completed {
				return api.Failure(409, "ONBOARDING_NOT_COMPLETED", "El alta inicial debe completarse antes de descartar una edición.")
			}
			w.Draft = nil
			w.DraftSourceVersion = nil
			w.Step = "completed"
		case "reset":
			d, err := defaultWizard(ctx, tx, w.Specialty.ID)
			if err != nil || len(d.Published) == 0 {
				return api.Failure(409, "SPECIALTY_NOT_AVAILABLE", "La especialidad no tiene una configuración publicada.")
			}
			definition, err := wizard.Decode(d.Published)
			if err != nil {
				return err
			}
			if err := s.validator.Validate(definition); err != nil {
				return err
			}
			w.Draft = d.Published
			w.DraftSourceVersion = &d.PublishedVersion
			w.Step = firstStep(definition)
		}
		if err := saveWorkspace(ctx, tx, w); err != nil {
			return err
		}
		result = &w.Setup
		return nil
	})
	return result, err
}
func (s *Store) UpdatePreferences(ctx context.Context, userID string, request map[string]any, notes bool) (Preferences, error) {
	var result Preferences
	var expected *int64
	var columns map[string]any
	var content *string
	var err error
	if notes {
		if err := api.Only(request, "content"); err != nil {
			return result, err
		}
		content, err = api.OptionalText(request, "content")
		if err != nil {
			return result, err
		}
		if content != nil && api.JavaLength(*content) > 10000 {
			return result, api.Invalid("Las notas rápidas no pueden superar los 10000 caracteres.")
		}
	} else {
		if err := api.Only(request, "expectedVersion", "patientColumnsConfig"); err != nil {
			return result, err
		}
		expected, err = api.Version(request, false)
		if err != nil {
			return result, err
		}
		columns, err = api.Object(request, "patientColumnsConfig")
		if err != nil {
			return result, err
		}
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := requireWorkspace(w); err != nil {
			return err
		}
		if notes {
			value := ""
			if content != nil {
				value = api.JavaTrim(*content)
			}
			w.Preferences.Notes = &value
		} else {
			if err := version(w.Setup.Version, expected, workspaceConflict); err != nil {
				return err
			}
			w.Preferences.Columns, err = json.Marshal(columns)
			if err != nil {
				return err
			}
		}
		if err := saveWorkspace(ctx, tx, w); err != nil {
			return err
		}
		result = w.Preferences
		return nil
	})
	return result, err
}
