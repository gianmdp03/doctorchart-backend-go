package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/testfixture"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

func TestDefaultSpecialtyDefinitions(t *testing.T) {
	var seeds map[string][]map[string]any
	if err := json.Unmarshal(seedJSON, &seeds); err != nil {
		t.Fatal(err)
	}
	validator, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds["specialties"]) != 3 || len(seeds["wizard_defaults"]) != 3 {
		t.Fatal("expected three complete defaults")
	}
	provisional := seeds["provisional_specialties"]
	if len(provisional) != 16 {
		t.Fatalf("expected sixteen provisional specialties, got %d", len(provisional))
	}
	seen := make(map[string]bool, len(seeds["specialties"])+len(provisional))
	for _, row := range seeds["specialties"] {
		seen[row["code"].(string)] = true
	}
	for _, row := range provisional {
		code, ok := row["code"].(string)
		if !ok || seen[code] {
			t.Fatalf("invalid or duplicate specialty reference: %v", row)
		}
		seen[code] = true
		if _, err := provisionalDefinition(code, validator); err != nil {
			t.Fatalf("invalid provisional specialty %s: %v", code, err)
		}
	}
	for _, row := range seeds["wizard_defaults"] {
		for _, key := range []string{"draft_definition", "published_definition"} {
			raw, err := json.Marshal(row[key])
			if err != nil {
				t.Fatal(err)
			}
			definition, err := wizard.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := validator.Validate(definition); err != nil {
				t.Fatalf("wizard %v %s: %v", row["specialty_id"], key, err)
			}
			if row["specialty_id"] == float64(1) {
				continue
			}
			if definition["modulesConfig"].(map[string]any)["GROWTH_CHARTS"] != false {
				t.Fatal("non-pediatric growth enabled")
			}
			for _, section := range []string{"patientForm", "encounterForm"} {
				for _, category := range definition[section].(map[string]any)["categories"].([]any) {
					for _, field := range category.(map[string]any)["fields"].([]any) {
						f := field.(map[string]any)
						if f["system"] != true && f["required"] != false {
							t.Fatalf("optional clinical field required: %v", f["key"])
						}
					}
				}
			}
		}
		if !reflect.DeepEqual(row["draft_definition"], row["published_definition"]) {
			t.Fatal("initial publication differs from draft")
		}
	}
	defaults := seeds["specialty_encounter_template_defaults"]
	if len(defaults) != 3 {
		t.Fatalf("expected 3 pediatric encounter template defaults, got %d", len(defaults))
	}
	for _, row := range defaults {
		if row["specialty_code"] != "pediatrics" {
			t.Fatalf("unexpected specialty_code %v", row["specialty_code"])
		}
		raw, err := json.Marshal(row["form_definition"])
		if err != nil {
			t.Fatal(err)
		}
		form, err := wizard.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := validator.ValidateEncounterForm(form); err != nil {
			t.Fatalf("invalid pediatric default template %v: %v", row["code"], err)
		}
	}
}

func TestMissingSpecialtiesAddedWithoutReplacingExistingConfiguration(t *testing.T) {
	pool := schemaFixture(t)
	initializeFixture(t, pool)
	ctx := t.Context()
	// Reproduce a previous installation containing only administered Pediatrics.
	if _, err := pool.Exec(ctx, `DELETE FROM wizard_defaults WHERE specialty_id IN (SELECT id FROM specialties WHERE code<>'pediatrics'); DELETE FROM specialties WHERE code<>'pediatrics';
UPDATE specialties SET code='PEDIATRICS',display_name='Nombre administrado' WHERE lower(code)='pediatrics';
UPDATE wizard_defaults SET version=9,published_version=4,draft_definition=jsonb_set(draft_definition,'{encounterForm,categories,0,title}','"Borrador personalizado"')`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('specialty',to_jsonb(s),'wizard',to_jsonb(w))::text FROM specialties s JOIN wizard_defaults w ON w.specialty_id=s.id WHERE lower(s.code)='pediatrics'`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	validator, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	// Addition rolls back with the surrounding startup transaction.
	failure := fmt.Errorf("abort test startup")
	if err := db.Ensure(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := seed(ctx, tx, validator); err != nil {
			return err
		}
		return failure
	}); !errors.Is(err, failure) {
		t.Fatalf("rollback hook: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM specialties").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed startup leaked specialties: %d %v", count, err)
	}
	for range 3 {
		if err := Run(ctx, pool, validator); err != nil {
			t.Fatal(err)
		}
	}
	assertFixtureReady(t, pool)
	var published int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM specialties s JOIN wizard_defaults w ON w.specialty_id=s.id
		WHERE s.active AND w.published_version=1 AND w.published_definition IS NOT NULL
		  AND w.published_approval_id IS NULL
	`).Scan(&published); err != nil || published != 19 {
		t.Fatalf("expected nineteen active starter specialties, got %d: %v", published, err)
	}
	if snapshot() != before {
		t.Fatal("Pediatrics configuration was replaced")
	}
	// Existing defaults remain owned by the administrator, including inactive ones.
	if _, err := pool.Exec(ctx, `UPDATE specialties SET active=false,display_name='Personalizada' WHERE code='dermatology'; UPDATE wizard_defaults SET published_version=8,version=12 WHERE specialty_id=(SELECT id FROM specialties WHERE code='dermatology')`); err != nil {
		t.Fatal(err)
	}
	if err := Run(ctx, pool, validator); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err := pool.QueryRow(ctx, `SELECT NOT s.active AND s.display_name='Personalizada' AND w.published_version=8 AND w.version=12 FROM specialties s JOIN wizard_defaults w ON w.specialty_id=s.id WHERE code='dermatology'`).Scan(&preserved); err != nil || !preserved {
		t.Fatal("admin defaults changed", err)
	}
}

func TestFreshStarterCatalogRollsBackIfSpecialtiesWouldBeHidden(t *testing.T) {
	pool := schemaFixture(t)
	initializeFixture(t, pool)
	var approved string
	if err := pool.QueryRow(t.Context(), `SELECT jsonb_object_agg(s.code,
		encode(sha256(convert_to(public.template_review_content(w.published_definition,
			public.template_experience(s.navigation,s.patient_record_tabs,s.patient_record_actions))::text,'UTF8')),'hex'))
		FROM specialties s JOIN wizard_defaults w ON w.specialty_id=s.id`).Scan(&approved); err != nil {
		t.Fatal(err)
	}
	var stale map[string]string
	if err := json.Unmarshal([]byte(approved), &stale); err != nil {
		t.Fatal(err)
	}
	stale["pediatrics"] = strings.Repeat("0", 64)
	delete(stale, "dermatology")
	stale["removed_specialty"] = strings.Repeat("1", 64)
	invalid, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	replacement := schemaFixture(t)
	err = RunWithStarterCatalog(t.Context(), replacement, validator, string(invalid))
	if err == nil || !strings.Contains(err.Error(), "dermatology, pediatrics, removed_specialty") {
		t.Fatalf("expected actionable starter catalog failure, got %v", err)
	}
	var empty bool
	if err := replacement.QueryRow(t.Context(), "SELECT to_regclass('public.app_schema_state') IS NULL AND to_regclass('public.specialties') IS NULL").Scan(&empty); err != nil || !empty {
		t.Fatalf("failed initialization did not roll back: %v", err)
	}
	if err := RunWithStarterCatalog(t.Context(), replacement, validator, approved); err != nil {
		t.Fatal(err)
	}
	if _, err := replacement.Exec(t.Context(), "UPDATE specialties SET active=false WHERE code='dermatology'"); err != nil {
		t.Fatal(err)
	}
	if err := RunWithStarterCatalog(t.Context(), replacement, validator, approved); err != nil {
		t.Fatal(err)
	}
	var active bool
	if err := replacement.QueryRow(t.Context(), "SELECT active FROM specialties WHERE code='dermatology'").Scan(&active); err != nil || active {
		t.Fatalf("existing administrator choice changed: %v", err)
	}
}

func TestNewSpecialtyWizardPersonalizationLifecycle(t *testing.T) {
	pool := schemaFixture(t)
	initializeFixture(t, pool)
	ctx := t.Context()
	if err := testfixture.ApprovePublishedTemplates(ctx, pool); err != nil {
		t.Fatal(err)
	}
	validator, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	store := workspace.New(pool, validator)
	for _, code := range []string{"general_medicine", "dermatology"} {
		t.Run(code, func(t *testing.T) {
			var id int64
			if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code=$1", code).Scan(&id); err != nil {
				t.Fatal(err)
			}
			setup, err := store.Select(ctx, code, map[string]any{"specialtyId": json.Number(fmt.Sprint(id)), "expectedVersion": nil})
			if err != nil {
				t.Fatal(err)
			}
			definition, err := wizard.Decode(setup.Draft)
			if err != nil {
				t.Fatal(err)
			}
			categories := definition["encounterForm"].(map[string]any)["categories"].([]any)
			categories[0].(map[string]any)["enabled"] = false
			// Enable an additional category where available.
			patient := definition["patientForm"].(map[string]any)["categories"].([]any)
			patient[len(patient)-1].(map[string]any)["enabled"] = true
			// Removing specialty-owned history must survive reads, finish and restart.
			filtered := []any{}
			for _, raw := range patient {
				if raw.(map[string]any)["key"] != "sys_clinical_history" {
					filtered = append(filtered, raw)
				}
			}
			definition["patientForm"].(map[string]any)["categories"] = filtered
			setup, err = store.Edit(ctx, code, "draft", map[string]any{"expectedVersion": json.Number(fmt.Sprint(setup.Version)), "definition": definition, "currentStepKey": "encounter"})
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := workspace.New(pool, validator).Setup(ctx, code)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(setup.Draft, reopened.Draft) || reopened.Step != "encounter" {
				t.Fatal("draft not restored")
			}
			finished, err := store.Edit(ctx, code, "finish", map[string]any{"expectedVersion": json.Number(fmt.Sprint(reopened.Version))})
			if err != nil {
				t.Fatal(err)
			}
			if !finished.Completed || finished.Step != "completed" || !reflect.DeepEqual(finished.Definition, reopened.Draft) {
				t.Fatal("personalized definition not applied")
			}
			if err := Run(ctx, pool, validator); err != nil {
				t.Fatal(err)
			}
			reopened, err = store.Setup(ctx, code)
			if err != nil {
				t.Fatal(err)
			}
			if !reopened.Completed || !reflect.DeepEqual(finished.Definition, reopened.Definition) {
				t.Fatal("restart changed personal settings")
			}
		})
	}
}
