package db

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
	"time"
)

func schemaElevenFixture(t *testing.T) *sql.DB {
	t.Helper()
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
	for _, step := range []func(context.Context, pgx.Tx, catalogPins) error{migrateClinics, migrateEntitlements, migrateTimezones, migrateSchedules, migrateBookings, migrateAttendee, migratePublicBookings, migrateSecretaries, migrateNotifications, migrateSpecialtyContexts} {
		if err = step(t.Context(), tx, pins); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(t.Context(), "UPDATE app_schema_state SET baseline_version=11,catalog_sha256=$1", pins.specialtyContexts); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestPatientInsertionFencesConcurrentSpecialtyRebinding(t *testing.T) {
	pool := schemaElevenFixture(t)
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(`INSERT INTO specialties(id,active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version) VALUES(3,true,'fixture-concurrent',now(),'Synthetic','[]','[]','[]',now(),0);
 INSERT INTO doctor_workspaces(id,user_id,version,created_at,updated_at) VALUES(3,'race-owner',0,now(),now());
 INSERT INTO doctor_specialty_contexts(workspace_id,specialty_id,version,current_step_key,onboarding_completed,created_at,updated_at) VALUES(3,1,0,'completed',true,now(),now())`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO patients(id,full_name,birth_date,user_id,version,workspace_version,updated_at,form_definition_snapshot,form_values,formula_context,formula_results,workspace_id,specialty_context_id)
 SELECT 3,'Synthetic','2000-01-01','race-owner',0,0,now(),'{}','{}','{}','{}',workspace_id,id FROM doctor_specialty_contexts WHERE workspace_id=3`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var pid int
	if err = conn.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := conn.ExecContext(ctx, "UPDATE doctor_specialty_contexts SET specialty_id=3 WHERE workspace_id=3")
		done <- err
	}()
	waiting := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err = pool.QueryRowContext(ctx, "SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("specialty update did not wait for patient insertion")
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var pg *pgconn.PgError
	if err = <-done; !errors.As(err, &pg) || pg.Code != "23514" {
		t.Fatal("concurrent rebinding changed a used context", err)
	}
}

func TestPatientContextUpgradePreservesAndRejectsInconsistentData(t *testing.T) {
	for _, mode := range []string{"success", "missing owner", "missing context", "specialty mismatch", "author mismatch", "SQL failure", "catalog drift", "late seed failure"} {
		t.Run(mode, func(t *testing.T) {
			pool := schemaElevenFixture(t)
			switch mode {
			case "missing owner":
				if _, err := pool.Exec("UPDATE patients SET user_id='orphan-owner' WHERE id=2"); err != nil {
					t.Fatal(err)
				}
			case "missing context":
				if _, err := pool.Exec("DELETE FROM doctor_specialty_contexts WHERE workspace_id=2"); err != nil {
					t.Fatal(err)
				}
			case "specialty mismatch":
				if _, err := pool.Exec("INSERT INTO specialties(id,active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version) VALUES(3,true,'fixture-other',now(),'Synthetic','[]','[]','[]',now(),0); UPDATE clinical_encounters SET specialty_id=3 WHERE id=1"); err != nil {
					t.Fatal(err)
				}
			case "author mismatch":
				if _, err := pool.Exec("UPDATE patient_prescription_records SET author_user_id='m1-other'"); err != nil {
					t.Fatal(err)
				}
			}
			before := dataSnapshot(t, pool)
			marker := readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s")
			original := patientContextMigration
			t.Cleanup(func() { patientContextMigration = original })
			var hook func(context.Context, pgx.Tx) error
			switch mode {
			case "SQL failure":
				patientContextMigration += "\nSELECT 1/0;"
			case "catalog drift":
				patientContextMigration += "\nCREATE TABLE fixture_context_drift(id int);"
			case "late seed failure":
				hook = func(context.Context, pgx.Tx) error { return errors.New("late seed") }
			}
			instance := mustPool(t.Context(), pool)
			defer instance.Close()
			err := Ensure(t.Context(), instance, hook)
			patientContextMigration = original
			if mode != "success" {
				if err == nil || readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s") != marker || readString(t, pool, "SELECT count(*)::text FROM information_schema.columns WHERE table_name='patients' AND column_name='specialty_context_id'") != "0" {
					t.Fatal("invalid migration changed state", err)
				}
				after := dataSnapshot(t, pool)
				for table, rows := range before {
					if after[table] != rows {
						t.Fatal("failed migration changed", table)
					}
				}
				if mode == "missing owner" || mode == "missing context" || mode == "specialty mismatch" || mode == "author mismatch" {
					return
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
					t.Fatal("lost historical values/references", table)
				}
			}
			if readString(t, pool, "SELECT bool_and(p.user_id=w.user_id AND p.workspace_id=c.workspace_id AND c.id=p.specialty_context_id)::text FROM patients p JOIN doctor_workspaces w ON w.id=p.workspace_id JOIN doctor_specialty_contexts c ON c.id=p.specialty_context_id") != "true" {
				t.Fatal("incorrect backfill")
			}
			state := readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s")
			if err = Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal("restart", err)
			}
			if readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s") != state {
				t.Fatal("restart rewrote state")
			}
			fresh := fixture(t)
			if err = ensureFixture(t.Context(), fresh, nil); err != nil {
				t.Fatal(err)
			}
			if readString(t, pool, catalogSQL, []string{}) != readString(t, fresh, catalogSQL, []string{}) {
				t.Fatal("fresh/upgrade differ")
			}
		})
	}
}

func TestPatientContextDatabaseOwnershipAndImmutability(t *testing.T) {
	pool := schemaElevenFixture(t)
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal(err)
	}
	reject := func(query string) {
		t.Helper()
		_, err := pool.Exec(query)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || len(pg.Code) < 2 || pg.Code[:2] != "23" {
			t.Fatal("ownership invariant was not enforced", err)
		}
	}
	base := `INSERT INTO patients(full_name,birth_date,user_id,version,workspace_version,updated_at,form_definition_snapshot,form_values,formula_context,formula_results,workspace_id,specialty_context_id) VALUES('Synthetic','2000-01-01','m1-owner',0,0,now(),'{}','{}','{}','{}',`
	reject(base + "1,NULL)")
	reject(base + "1,999999)")
	reject(base + "1,(SELECT id FROM doctor_specialty_contexts WHERE workspace_id=2))")
	reject(base + "2,(SELECT id FROM doctor_specialty_contexts WHERE workspace_id=2))")
	reject("UPDATE patients SET specialty_context_id=(SELECT id FROM doctor_specialty_contexts WHERE workspace_id=2) WHERE id=1")
	reject("UPDATE patients SET user_id='m1-other',workspace_id=2 WHERE id=1")
	for _, table := range []string{"clinical_encounters", "patient_prescription_records", "patient_medical_studies", "patient_vaccinations"} {
		reject("UPDATE " + table + " SET patient_id=2 WHERE id=1")
	}
	reject("UPDATE patient_prescription_records SET author_user_id='m1-other' WHERE id=1")
	reject("UPDATE doctor_specialty_contexts SET workspace_id=2 WHERE workspace_id=1")
	if _, err := pool.Exec("INSERT INTO specialties(id,active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version) VALUES(3,true,'fixture-other',now(),'Synthetic','[]','[]','[]',now(),0)"); err != nil {
		t.Fatal(err)
	}
	reject("UPDATE doctor_specialty_contexts SET specialty_id=3 WHERE workspace_id=1")
	reject("UPDATE clinical_encounters SET specialty_id=3 WHERE id=1")
	reject("INSERT INTO patient_calculation_logs(patient_id,user_id,encounter_id,calculation_code,calculation_name,input_snapshot,formatted_result,created_at) VALUES(2,'m1-other',1,'fixture','Synthetic','{}','Synthetic',now())")
	reject("INSERT INTO patient_calculation_logs(patient_id,user_id,calculation_code,calculation_name,input_snapshot,formatted_result,created_at) VALUES(1,'m1-other','fixture','Synthetic','{}','Synthetic',now())")
	// No new deletion cascades: existing historical children still prevent raw deletion.
	reject("DELETE FROM patients WHERE id=1")
}
