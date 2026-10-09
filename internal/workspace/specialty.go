package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,79}$`)

func duplicate() error {
	return api.Failure(409, "SPECIALTY_CODE_EXISTS", "Ya existe una especialidad con ese código.")
}

func (s *Store) CreateSpecialty(ctx context.Context, request map[string]any) (Specialty, error) {
	var result Specialty
	if err := api.Only(request, "code", "name", "description", "definition"); err != nil {
		return result, err
	}
	code, err := api.Text(request, "code", 80)
	if err != nil {
		return result, err
	}
	if !codePattern.MatchString(code) {
		return result, api.Invalid("El código de especialidad debe estar en minúsculas y usar caracteres válidos.")
	}
	name, err := api.Text(request, "name", 120)
	if err != nil {
		return result, err
	}
	description, err := api.OptionalText(request, "description")
	if err != nil {
		return result, err
	}
	definition, err := api.Object(request, "definition")
	if err != nil {
		return result, err
	}
	if err := s.validator.Validate(definition); err != nil {
		return result, err
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := catalogLock(ctx, tx); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM specialties WHERE code=$1)", code).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return duplicate()
		}
		result, err = scanSpecialty(tx.QueryRow(ctx, `INSERT INTO specialties(code,display_name,description,active,version,navigation,patient_record_tabs,patient_record_actions,created_at,updated_at) VALUES($1,$2,$3,false,0,$4,$5,$6,clock_timestamp(),clock_timestamp()) RETURNING `+specialtyColumns, code, name, description, []string{"DASHBOARD", "PATIENTS", "WIZARD"}, []string{"SUMMARY", "ENCOUNTERS"}, []string{"EDIT_PATIENT", "NEW_ENCOUNTER"}))
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return duplicate()
			}
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO wizard_defaults(specialty_id,draft_definition,published_version,version,updated_at) VALUES($1,$2,0,0,clock_timestamp())`, result.ID, encoded)
		return err
	})
	return result, err
}
func (s *Store) UpdateSpecialty(ctx context.Context, id int64, request map[string]any) (Specialty, error) {
	var result Specialty
	if err := api.Only(request, "expectedVersion", "name", "description", "active"); err != nil {
		return result, err
	}
	expected, err := api.Version(request, false)
	if err != nil {
		return result, err
	}
	_, hasName := request["name"]
	_, hasDescription := request["description"]
	_, hasActive := request["active"]
	if !hasName && !hasDescription && !hasActive {
		return result, api.Invalid("La modificación debe incluir nombre, descripción o disponibilidad.")
	}
	var name string
	if hasName {
		name, err = api.Text(request, "name", 120)
		if err != nil {
			return result, err
		}
	}
	description, err := api.OptionalText(request, "description")
	if err != nil {
		return result, err
	}
	var active *bool
	if request["active"] != nil {
		value, ok := request["active"].(bool)
		if !ok {
			return result, api.Invalid("El campo active debe ser booleano.")
		}
		active = &value
	}
	if hasActive && active == nil {
		return result, api.Invalid("La disponibilidad no admite null.")
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := catalogLock(ctx, tx); err != nil {
			return err
		}
		current, err := specialty(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := version(current.Version, expected, "La configuración cambió. Recargá antes de guardar."); err != nil {
			return err
		}
		if active != nil {
			var published bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM wizard_defaults WHERE specialty_id=$1 AND published_definition IS NOT NULL)", id).Scan(&published); err != nil {
				return err
			}
			if *active && !published {
				return api.Failure(409, "SPECIALTY_NOT_AVAILABLE", "Publicá el wizard antes de activar la especialidad.")
			}
			if !*active && current.Active && published {
				var count int64
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM specialties s JOIN wizard_defaults w ON w.specialty_id=s.id WHERE s.active AND w.published_definition IS NOT NULL").Scan(&count); err != nil {
					return err
				}
				if count <= 1 {
					return api.Failure(409, "LAST_ACTIVE_SPECIALTY", "Debe quedar al menos una especialidad publicada y activa.")
				}
			}
			current.Active = *active
		}
		if hasName {
			current.Name = name
		}
		if hasDescription {
			current.Description = description
		}
		result, err = scanSpecialty(tx.QueryRow(ctx, "UPDATE specialties SET display_name=$2,description=$3,active=$4,version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND version=$5 RETURNING "+specialtyColumns, id, current.Name, current.Description, current.Active, current.Version))
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(409, "VERSION_CONFLICT", "La configuración cambió. Actualizá los datos antes de guardar.")
		}
		return err
	})
	return result, err
}
func (s *Store) SaveWizard(ctx context.Context, id int64, request map[string]any, publish bool) (Default, error) {
	var result Default
	fields := []string{"expectedVersion"}
	if !publish {
		fields = append(fields, "definition")
	}
	if err := api.Only(request, fields...); err != nil {
		return result, err
	}
	expected, err := api.Version(request, false)
	if err != nil {
		return result, err
	}
	var next map[string]any
	if !publish {
		next, err = api.Object(request, "definition")
		if err != nil {
			return result, err
		}
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if publish {
			if err := catalogLock(ctx, tx); err != nil {
				return err
			}
		}
		current, err := defaultWizard(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := version(current.Version, expected, "La configuración cambió. Recargá antes de guardar."); err != nil {
			return err
		}
		if publish {
			draft, err := wizard.Decode(current.Draft)
			if err != nil {
				return err
			}
			if err := s.validator.Validate(draft); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, "UPDATE specialties SET active=true,version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1", id)
			if err != nil {
				return err
			}
			current.Published = current.Draft
			current.PublishedVersion++
		} else {
			previous, err := wizard.Decode(current.Draft)
			if err != nil {
				return err
			}
			if err := s.validator.Transition(previous, next); err != nil {
				return err
			}
			current.Draft, err = json.Marshal(next)
			if err != nil {
				return err
			}
		}
		err = tx.QueryRow(ctx, `UPDATE wizard_defaults SET draft_definition=$2,published_definition=$3,published_version=$4,version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE specialty_id=$1 AND version=$5 RETURNING specialty_id,version,published_version,draft_definition,published_definition`, id, current.Draft, current.Published, current.PublishedVersion, current.Version).Scan(&result.SpecialtyID, &result.Version, &result.PublishedVersion, &result.Draft, &result.Published)
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(409, "VERSION_CONFLICT", "La configuración cambió. Actualizá los datos antes de guardar.")
		}
		return err
	})
	return result, err
}
