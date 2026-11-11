package db

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func schemaTwelveFixture(t *testing.T) *sql.DB {
	t.Helper()
	pool := schemaElevenFixture(t)
	instance := mustPool(t.Context(), pool)
	defer instance.Close()
	tx, err := instance.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	pins := fixtureCatalogPins(t, pool)
	if err = migratePatientContexts(t.Context(), tx, pins); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), "UPDATE app_schema_state SET baseline_version=12,catalog_sha256=$1", pins.patientContexts); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(`INSERT INTO clinics(id,workspace_id,name,country,province,city,address,timezone,status) VALUES(1,1,'Centro','Argentina','Buenos Aires','City','Synthetic','UTC','ACTIVE'),(2,2,'Other','Argentina','Buenos Aires','City','Synthetic','UTC','ACTIVE');
 INSERT INTO clinic_schedules(clinic_id,slot_minutes,min_notice_minutes,max_advance_days,weekly,exceptions,version) VALUES(1,20,60,45,
 '[{"weekday":1,"ranges":[{"start":"08:00","end":"10:00"},{"start":"10:00","end":"12:00"}]},{"weekday":3,"ranges":[{"start":"14:00","end":"15:15"}]}]',
 '[{"from":"2027-01-01","through":"2027-01-03","label":"Cierre","ranges":[]},{"from":"2027-01-05","through":"2027-01-05","label":"Especial","ranges":[{"start":"09:00","end":"11:00"}]}]',7);
 INSERT INTO bookings(workspace_id,clinic_id,start_at,end_at,contact_name) VALUES(1,1,'2027-01-04 08:00Z','2027-01-04 08:20Z','Synthetic')`); err != nil {
		t.Fatal(err)
	}
	// The existing HTTP contract counts trimmed runes, not raw label length.
	if _, err := pool.Exec(`UPDATE clinic_schedules SET exceptions=jsonb_set(exceptions,'{1,label}',to_jsonb('  '||repeat('E',160)||'  '))`); err != nil {
		t.Fatal(err)
	}
	return pool
}
func TestScheduleBlocksUpgradePreservesAndRollsBack(t *testing.T) {
	for _, mode := range []string{"success", "missing context", "invalid legacy", "SQL failure", "catalog drift", "late seed failure"} {
		t.Run(mode, func(t *testing.T) {
			pool := schemaTwelveFixture(t)
			switch mode {
			case "missing context":
				if _, err := pool.Exec("DELETE FROM doctor_specialty_contexts WHERE workspace_id=1"); err == nil {
					t.Fatal("patient context should be referenced")
				}
				if _, err := pool.Exec("UPDATE clinic_schedules SET clinic_id=2"); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec("DELETE FROM doctor_specialty_contexts WHERE workspace_id=2"); err == nil {
					t.Fatal("patient context should be referenced")
				}
				// A genuinely context-less workspace with an agenda, not an inferred owner.
				if _, err := pool.Exec("INSERT INTO doctor_workspaces(id,user_id,version,created_at,updated_at) VALUES(3,'orphan',0,now(),now()); UPDATE clinics SET workspace_id=3 WHERE id=2"); err != nil {
					t.Fatal(err)
				}
			case "invalid legacy":
				if _, err := pool.Exec(`UPDATE clinic_schedules SET weekly='[{"weekday":1,"ranges":[{"start":"09:00","end":"12:00"},{"start":"11:00","end":"13:00"}]}]'`); err != nil {
					t.Fatal(err)
				}
			}
			before := dataSnapshot(t, pool)
			state := readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s")
			bookings := readString(t, pool, "SELECT jsonb_agg((to_jsonb(b)-'specialty_context_id'-'specialty_snapshot'-'clinic_snapshot'-'provenance'))::text FROM bookings b")
			root := readString(t, pool, "SELECT (to_jsonb(s)-'slot_minutes'-'weekly'-'exceptions')::text FROM clinic_schedules s")
			original := scheduleBlockMigration
			t.Cleanup(func() { scheduleBlockMigration = original })
			var seed func(context.Context, pgx.Tx) error
			switch mode {
			case "SQL failure":
				scheduleBlockMigration += "\nSELECT 1/0;"
			case "catalog drift":
				scheduleBlockMigration += "\nCREATE TABLE fixture_blocks_drift(id int);"
			case "late seed failure":
				seed = func(context.Context, pgx.Tx) error { return errors.New("late seed") }
			}
			instance := mustPool(t.Context(), pool)
			defer instance.Close()
			err := Ensure(t.Context(), instance, seed)
			scheduleBlockMigration = original
			if mode != "success" {
				if err == nil || readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s") != state || readString(t, pool, "SELECT (to_regclass('schedule_blocks') IS NULL)::text") != "true" {
					t.Fatal("failed migration changed catalog/state", err)
				}
				if mode == "missing context" || mode == "invalid legacy" {
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
					t.Fatal("historical data changed", table)
				}
			}
			if root != readString(t, pool, "SELECT (to_jsonb(s)-'workspace_id')::text FROM clinic_schedules s") || bookings != readString(t, pool, "SELECT jsonb_agg((to_jsonb(b)-'specialty_context_id'-'specialty_snapshot'-'clinic_snapshot'-'provenance'))::text FROM bookings b") {
				t.Fatal("root/version/booking changed")
			}
			if readString(t, pool, "SELECT count(*)::text FROM schedule_blocks WHERE slot_minutes=20 AND workspace_id=1 AND specialty_context_id=(SELECT id FROM doctor_specialty_contexts WHERE workspace_id=1)") != "4" {
				t.Fatal("blocks not transferred")
			}
			if readString(t, pool, `SELECT count(*)::text FROM schedule_blocks b LEFT JOIN schedule_exceptions e ON e.id=b.exception_id
 WHERE (weekday=1 AND start_minute=480 AND end_minute=600)
 OR (weekday=1 AND start_minute=600 AND end_minute=720)
 OR (weekday=3 AND start_minute=840 AND end_minute=915)
 OR (weekday IS NULL AND start_minute=540 AND end_minute=660 AND e.label='  '||repeat('E',160)||'  ' AND e.from_date='2027-01-05' AND e.through_date='2027-01-05')`) != "4" {
				t.Fatal("legacy days/times/exception changed")
			}
			if readString(t, pool, "SELECT count(*)::text FROM schedule_exceptions e WHERE label='Cierre' AND from_date='2027-01-01' AND through_date='2027-01-03' AND NOT EXISTS(SELECT 1 FROM schedule_blocks b WHERE b.exception_id=e.id)") != "1" {
				t.Fatal("closure lost")
			}
			snapshot := readString(t, pool, "SELECT jsonb_build_array((SELECT jsonb_agg((to_jsonb(b)-'specialty_context_id'-'specialty_snapshot'-'clinic_snapshot'-'provenance') ORDER BY id) FROM schedule_blocks b),(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM schedule_exceptions e))::text")
			if err = Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal(err)
			}
			if snapshot != readString(t, pool, "SELECT jsonb_build_array((SELECT jsonb_agg((to_jsonb(b)-'specialty_context_id'-'specialty_snapshot'-'clinic_snapshot'-'provenance') ORDER BY id) FROM schedule_blocks b),(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM schedule_exceptions e))::text") {
				t.Fatal("restart changed block identities")
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
func TestScheduleBlocksRelationalIntegrity(t *testing.T) {
	pool := schemaTwelveFixture(t)
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal(err)
	}
	reject := func(query string) {
		t.Helper()
		_, err := pool.Exec(query)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code[:2] != "23" {
			t.Fatal("invalid block admitted", err)
		}
	}
	base := "INSERT INTO schedule_blocks(clinic_id,workspace_id,specialty_context_id,weekday,start_minute,end_minute,slot_minutes) SELECT 1,1,id,2,540,600,"
	reject(base + "0 FROM doctor_specialty_contexts WHERE workspace_id=1")
	reject(base + "20 FROM doctor_specialty_contexts WHERE workspace_id=2")
	reject("UPDATE schedule_blocks SET start_minute=end_minute WHERE id=(SELECT min(id) FROM schedule_blocks)")
	reject("UPDATE schedule_blocks SET weekday=NULL WHERE exception_id IS NULL")
	reject("UPDATE schedule_blocks SET exception_id=(SELECT id FROM schedule_exceptions LIMIT 1) WHERE weekday IS NOT NULL")
	reject("UPDATE schedule_blocks SET clinic_id=2 WHERE id=(SELECT min(id) FROM schedule_blocks)")
	reject("UPDATE schedule_exceptions SET through_date=from_date-1")
	reject("UPDATE schedule_blocks SET start_minute=480,end_minute=720 WHERE weekday=1")
	reject("UPDATE schedule_exceptions SET from_date='2027-01-01',through_date='2027-01-05'")
	reject("DELETE FROM doctor_specialty_contexts WHERE workspace_id=1")
}
