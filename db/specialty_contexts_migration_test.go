package db

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestSpecialtyContextUpgradeFromTenPreservesDataAndRollsBack(t *testing.T) {
	for _, mode := range []string{"success", "SQL failure", "catalog drift", "late seed failure"} {
		t.Run(mode, func(t *testing.T) {
			pool := baselineFixture(t)
			clinicalFixture(t, pool)
			pins := fixtureCatalogPins(t, pool)
			instance := mustPool(t.Context(), pool)
			defer instance.Close()
			tx, err := instance.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			for _, step := range []func(context.Context, pgx.Tx, catalogPins) error{migrateClinics, migrateEntitlements, migrateTimezones, migrateSchedules, migrateBookings, migrateAttendee, migratePublicBookings, migrateSecretaries, migrateNotifications} {
				if err = step(t.Context(), tx, pins); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = tx.Exec(t.Context(), "UPDATE app_schema_state SET baseline_version=10,catalog_sha256=$1", pins.notifications); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(`UPDATE doctor_workspaces SET definition='{"effective":"preserve"}',draft_definition='{"draft":"preserve"}',source_wizard_version=2,draft_source_wizard_version=3,current_step_key='encounter',onboarding_completed=false WHERE id=2;
   INSERT INTO clinics(workspace_id,name,country,province,city,address,timezone,status) VALUES(1,'Preserved clinic','Argentina','Buenos Aires','Mar del Plata','Fixture 123','UTC','ACTIVE')`); err != nil {
				t.Fatal(err)
			}
			before := dataSnapshot(t, pool)
			clinics := readString(t, pool, "SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM clinics c")
			marker := readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s")
			clinicalConfig := readString(t, pool, "SELECT jsonb_agg(jsonb_build_array(id,specialty_id,definition,draft_definition,source_wizard_version,draft_source_wizard_version,version,current_step_key,onboarding_completed,created_at,updated_at) ORDER BY id)::text FROM doctor_workspaces")
			original := specialtyContextMigration
			t.Cleanup(func() { specialtyContextMigration = original })
			var seed func(context.Context, pgx.Tx) error
			switch mode {
			case "SQL failure":
				specialtyContextMigration += "\nSELECT 1/0;"
			case "catalog drift":
				specialtyContextMigration += "\nCREATE TABLE context_drift(id int);"
			case "late seed failure":
				seed = func(context.Context, pgx.Tx) error { return errors.New("late seed failure") }
			}
			err = Ensure(t.Context(), instance, seed)
			specialtyContextMigration = original
			if mode != "success" {
				if err == nil || readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s") != marker || readString(t, pool, "SELECT (to_regclass('public.doctor_specialty_contexts') IS NULL)::text") != "true" {
					t.Fatal("failed migration did not roll back", err)
				}
				if err = Ensure(t.Context(), instance, nil); err != nil {
					t.Fatal("retry", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			after := dataSnapshot(t, pool)
			for table, rows := range before {
				if after[table] != rows {
					t.Fatal("changed existing rows:", table)
				}
			}
			if clinics != readString(t, pool, "SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM clinics c") {
				t.Fatal("clinic relationship changed")
			}
			if clinicalConfig != readString(t, pool, "SELECT jsonb_agg(jsonb_build_array(workspace_id,specialty_id,definition,draft_definition,source_wizard_version,draft_source_wizard_version,version,current_step_key,onboarding_completed,created_at,updated_at) ORDER BY workspace_id)::text FROM doctor_specialty_contexts") {
				t.Fatal("clinical configuration, versions or timestamps lost")
			}
			if readString(t, pool, "SELECT count(*)::text FROM information_schema.columns WHERE table_name='doctor_workspaces' AND column_name IN ('specialty_id','definition','draft_definition','current_step_key','onboarding_completed','source_wizard_version','draft_source_wizard_version')") != "0" {
				t.Fatal("duplicate clinical source remains")
			}
			if err = Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal("restart", err)
			}
			fresh := fixture(t)
			if err = ensureFixture(t.Context(), fresh, nil); err != nil {
				t.Fatal(err)
			}
			if readString(t, pool, catalogSQL, []string{}) != readString(t, fresh, catalogSQL, []string{}) {
				t.Fatal("fresh/upgrade diverged")
			}
		})
	}
}
