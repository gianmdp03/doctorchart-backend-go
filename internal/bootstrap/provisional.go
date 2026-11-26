package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/templates"
)

type provisionalSpecialty struct {
	Status          string
	CreateSpecialty struct {
		Code, Name, Description string
		Definition              json.RawMessage
	}
	Experience struct {
		Navigation, PatientRecordTabs, PatientRecordActions []string
	}
}

func provisionalDefinition(code string, validator *wizard.Validator) (provisionalSpecialty, error) {
	var item provisionalSpecialty
	data, err := templates.Provisional.ReadFile(code + ".provisional.json")
	if err != nil {
		return item, err
	}
	if err := json.Unmarshal(data, &item); err != nil {
		return item, err
	}
	if item.Status != "PROVISIONAL_TEST_ONLY" || item.CreateSpecialty.Code != code {
		return item, fmt.Errorf("invalid provisional specialty metadata for %s", code)
	}
	definition, err := wizard.Decode(item.CreateSpecialty.Definition)
	if err != nil {
		return item, err
	}
	return item, validator.Validate(definition)
}

func seedProvisional(ctx context.Context, tx pgx.Tx, validator *wizard.Validator, refs []map[string]any) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1,$2)", db.LockNamespace, db.TemplateCatalogLockKey); err != nil {
		return err
	}
	for _, ref := range refs {
		code, ok := ref["code"].(string)
		if !ok || code == "" {
			return fmt.Errorf("invalid provisional specialty reference")
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM specialties WHERE lower(code)=lower($1))", code).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		item, err := provisionalDefinition(code, validator)
		if err != nil {
			return fmt.Errorf("validate provisional specialty %s: %w", code, err)
		}
		draft, experience := item.CreateSpecialty, item.Experience
		var id int64
		err = tx.QueryRow(ctx, `
     INSERT INTO specialties(code,display_name,description,active,version,navigation,
        patient_record_tabs,patient_record_actions,created_at,updated_at)
     VALUES($1,$2,$3,true,0,$4,$5,$6,clock_timestamp(),clock_timestamp())
     RETURNING id
   `, code, draft.Name, draft.Description, experience.Navigation, experience.PatientRecordTabs, experience.PatientRecordActions).Scan(&id)
		if err != nil {
			return fmt.Errorf("insert provisional specialty %s: %w", code, err)
		}
		if _, err := tx.Exec(ctx, `
     INSERT INTO wizard_defaults(specialty_id,draft_definition,published_definition,published_version,version,updated_at)
     VALUES($1,$2,$2,1,0,clock_timestamp())
   `, id, draft.Definition); err != nil {
			return fmt.Errorf("insert provisional wizard %s: %w", code, err)
		}
	}
	return nil
}
