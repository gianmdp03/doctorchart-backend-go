package db

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestMultipleContextUpgradeFromFourteenRollbackRetryPreservation(t *testing.T) {
	for _, mode := range []string{"success", "SQL failure", "catalog drift", "late seed failure"} {
		t.Run(mode, func(t *testing.T) {
			pool := schemaThirteenFixture(t)
			instance := mustPool(t.Context(), pool)
			defer instance.Close()
			pins := fixtureCatalogPins(t, pool)
			tx, err := instance.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if err = migrateBookingContexts(t.Context(), tx, pins); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(t.Context(), "UPDATE app_schema_state SET baseline_version=14,catalog_sha256=$1", pins.bookingContexts); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec("UPDATE doctor_workspaces SET quick_notes='Preserve notes',patient_columns_config='{\"email\":false}',version=8 WHERE id=1"); err != nil {
				t.Fatal(err)
			}
			before := readString(t, pool, "SELECT jsonb_agg(to_jsonb(w) ORDER BY id)::text FROM doctor_workspaces w")
			bookings := readString(t, pool, "SELECT jsonb_agg(to_jsonb(b) ORDER BY id)::text FROM bookings b")
			history := migrationHistory(t, pool)
			state := readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s")
			original := multipleContextMigration
			t.Cleanup(func() { multipleContextMigration = original })
			var seed func(context.Context, pgx.Tx) error
			switch mode {
			case "SQL failure":
				multipleContextMigration += "; SELECT 1/0;"
			case "catalog drift":
				multipleContextMigration += "; CREATE TABLE fixture_m5_drift(id int);"
			case "late seed failure":
				seed = func(context.Context, pgx.Tx) error { return errors.New("synthetic seed failure") }
			}
			err = Ensure(t.Context(), instance, seed)
			multipleContextMigration = original
			if mode != "success" {
				if err == nil || state != readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s") || history != migrationHistory(t, pool) || readString(t, pool, "SELECT (to_regclass('public.specialty_context_preferences') IS NULL)::text") != "true" {
					t.Fatal("rollback failed", err)
				}
				if err = Ensure(t.Context(), instance, nil); err != nil {
					t.Fatal("retry", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if before != readString(t, pool, "SELECT jsonb_agg(to_jsonb(w) ORDER BY id)::text FROM doctor_workspaces w") || bookings != readString(t, pool, "SELECT jsonb_agg(to_jsonb(b) ORDER BY id)::text FROM bookings b") {
				t.Fatal("historical data changed")
			}
			if readString(t, pool, "SELECT quick_notes||':'||p.version::text||':'||(patient_columns_config->>'email') FROM specialty_context_preferences p JOIN doctor_specialty_contexts c ON c.id=p.specialty_context_id WHERE c.workspace_id=1") != "Preserve notes:8:false" {
				t.Fatal("preferences not preserved")
			}
			if err = Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal("restart", err)
			}
			if _, err = pool.Exec("UPDATE app_schema_migrations SET sql_sha256=repeat('0',64) WHERE version=15"); err != nil {
				t.Fatal(err)
			}
			if err = Ensure(t.Context(), instance, nil); err == nil {
				t.Fatal("tampered M5 checksum admitted")
			}
		})
	}
}
