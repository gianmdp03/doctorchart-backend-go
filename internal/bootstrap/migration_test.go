package bootstrap

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

func TestBaselineUpgradeWithRealSeedsPreservesAdminConfiguration(t *testing.T) {
	pool := schemaFixture(t)
	ctx := t.Context()
	validator, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	// Build a genuine pre-M1 installation with real seed data, not schema 2
	// relabeled as version 1. These pins describe the published baseline only.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, db.Schema); err != nil {
		t.Fatal(err)
	}
	if err := seed(ctx, tx, validator); err != nil {
		t.Fatal(err)
	}
	var major int
	if err := tx.QueryRow(ctx, "SELECT current_setting('server_version_num')::integer / 10000").Scan(&major); err != nil {
		t.Fatal(err)
	}
	baselinePins := map[int]string{
		17: "6f58df736f5a4398ee20dc0e5abad8c8913eb7bea7ecc060bb8e1971d14843cc",
		18: "e992a8133a1af9f510914e5958165a32e7f1cfbddf5b920ec4ae15fdab4f3d2f",
	}
	if baselinePins[major] == "" {
		t.Fatalf("baseline fixture supports only PostgreSQL 17/18, got %d", major)
	}
	sqlHash := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ReplaceAll(db.Schema, "\r\n", "\n"))))
	if _, err := tx.Exec(ctx, "INSERT INTO app_schema_state(id,backend,baseline_version,sql_sha256,catalog_sha256) VALUES(1,$1,1,$2,$3)", db.Backend, sqlHash, baselinePins[major]); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var baselineVersion, tables int
	if err := pool.QueryRow(ctx, "SELECT baseline_version,(SELECT count(*) FROM pg_tables WHERE schemaname='public') FROM app_schema_state").Scan(&baselineVersion, &tables); err != nil || baselineVersion != 1 || tables != 23 {
		t.Fatalf("expected seeded baseline 1 with 23 tables, got %d/%d: %v", baselineVersion, tables, err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE chart_definitions SET title='Admin chart title' WHERE id=(SELECT min(id) FROM chart_definitions);
UPDATE specialties SET display_name='Admin specialty' WHERE id=(SELECT min(id) FROM specialties);
UPDATE clinical_calculation_definitions SET name='Admin calculator' WHERE id=(SELECT min(id) FROM clinical_calculation_definitions);
UPDATE wizard_defaults SET version=version+1 WHERE id=(SELECT min(id) FROM wizard_defaults);
INSERT INTO doctor_workspaces(created_at,current_step_key,definition,onboarding_completed,updated_at,user_id,version,specialty_id,quick_notes)
 SELECT now(),'COMPLETE',published_definition,true,now(),'m1-bootstrap-owner',7,specialty_id,'Preserve doctor notes' FROM wizard_defaults ORDER BY id LIMIT 1;
`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() map[string]string {
		t.Helper()
		values := make(map[string]string)
		for _, table := range []string{"specialties", "wizard_defaults", "clinical_calculation_definitions", "chart_definitions", "specialty_calculator_defaults", "vaccination_calendars", "vaccination_doses", "doctor_workspaces"} {
			var rows string
			if err := pool.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id),'[]'::jsonb)::text FROM "+pgx.Identifier{"public", table}.Sanitize()+" r").Scan(&rows); err != nil {
				t.Fatal(err)
			}
			values[table] = rows
		}
		return values
	}
	before := snapshot()
	var initialized time.Time
	if err := pool.QueryRow(ctx, "SELECT initialized_at FROM app_schema_state").Scan(&initialized); err != nil {
		t.Fatal(err)
	}
	// The first Run must upgrade 1 -> 2; the second must preserve the result.
	for range 2 {
		if err := Run(ctx, pool, validator); err != nil {
			t.Fatal(err)
		}
		assertFixtureReady(t, pool)
		if !reflect.DeepEqual(before, snapshot()) {
			t.Fatal("upgrade/restart replaced administered configuration or real seed data")
		}
		var version, migrations, clinics int
		var timestamp time.Time
		var checksum string
		if err := pool.QueryRow(ctx, "SELECT baseline_version,initialized_at,sql_sha256,(SELECT count(*) FROM app_schema_migrations WHERE version=2),(SELECT count(*) FROM clinics) FROM app_schema_state").Scan(&version, &timestamp, &checksum, &migrations, &clinics); err != nil {
			t.Fatal(err)
		}
		if version != 2 || migrations != 1 || clinics != 0 || !initialized.Equal(timestamp) || checksum != sqlHash {
			t.Fatal("upgrade/restart changed baseline identity, duplicated migration or invented places")
		}
	}
}
