package db

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func bookingUpgradeFixture(t *testing.T) *sql.DB {
	t.Helper()
	pool := timezoneUpgradeFixture(t)
	timezoneClinic(t, pool, "UTC", "ACTIVE")
	instance := mustPool(t.Context(), pool)
	defer instance.Close()
	tx, err := instance.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	pins := fixtureCatalogPins(t, pool)
	if err = migrateTimezones(t.Context(), tx, pins); err != nil {
		t.Fatal(err)
	}
	if err = migrateSchedules(t.Context(), tx, pins); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), "INSERT INTO clinic_schedules(clinic_id,slot_minutes,min_notice_minutes,max_advance_days,weekly,exceptions,version) SELECT id,30,0,30,'[]','[]',7 FROM clinics"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), "UPDATE app_schema_state SET baseline_version=5,catalog_sha256=$1", pins.schedules); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	return pool
}
func bookingMigrationSnapshot(t *testing.T, pool *sql.DB) map[string]string {
	t.Helper()
	snapshot := timezoneDataSnapshot(t, pool)
	snapshot["agendas"] = readString(t, pool, "SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY clinic_id),'[]'::jsonb)::text FROM clinic_schedules s")
	snapshot["old history"] = readString(t, pool, "SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM app_schema_migrations m WHERE version<6")
	snapshot["original marker"] = readString(t, pool, "SELECT backend||':'||sql_sha256||':'||initialized_at::text FROM app_schema_state")
	return snapshot
}
func TestBookingMigrationFromM3PreservesDataAndRollsBack(t *testing.T) {
	for _, mode := range []string{"success", "SQL failure", "catalog drift", "late seed failure"} {
		t.Run(mode, func(t *testing.T) {
			pool := bookingUpgradeFixture(t)
			before := bookingMigrationSnapshot(t, pool)
			instance := mustPool(t.Context(), pool)
			defer instance.Close()
			original := bookingMigration
			t.Cleanup(func() { bookingMigration = original })
			var seed func(context.Context, pgx.Tx) error
			switch mode {
			case "SQL failure":
				bookingMigration += "\nSELECT 1/0;"
			case "catalog drift":
				bookingMigration += "\nCREATE TABLE fixture_booking_drift(id integer);"
			case "late seed failure":
				seed = func(context.Context, pgx.Tx) error { return errors.New("synthetic seed failure") }
			}
			err := Ensure(t.Context(), instance, seed)
			bookingMigration = original
			if mode != "success" {
				if err == nil {
					t.Fatal("faulted upgrade committed")
				}
				if readString(t, pool, "SELECT baseline_version::text FROM app_schema_state") != "5" || readString(t, pool, "SELECT (to_regclass('public.bookings') IS NULL)::text") != "true" {
					t.Fatal("rollback did not preserve M3")
				}
				if !reflect.DeepEqual(before, bookingMigrationSnapshot(t, pool)) {
					t.Fatal("failed upgrade modified clinical/agendas/history")
				}
				if err = Ensure(t.Context(), instance, nil); err != nil {
					t.Fatal("retry", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, bookingMigrationSnapshot(t, pool)) {
				t.Fatal("upgrade modified M3 data")
			}
			if readString(t, pool, "SELECT baseline_version::text FROM app_schema_state") != "8" || readString(t, pool, "SELECT count(*)::text FROM app_schema_migrations WHERE version=6") != "1" {
				t.Fatal("missing booking migration metadata")
			}
			fresh := fixture(t)
			if err = ensureFixture(t.Context(), fresh, nil); err != nil {
				t.Fatal(err)
			}
			if readString(t, pool, catalogSQL, []string{}) != readString(t, fresh, catalogSQL, []string{}) {
				t.Fatal("fresh and M3 upgrade catalogs differ")
			}
			// Retain every booking, audit event and replay receipt across bootstrap restarts.
			if _, err = pool.Exec("INSERT INTO bookings(workspace_id,clinic_id,patient_id,start_at,end_at,contact_name) SELECT 1,id,1,'2030-01-01 09:00Z','2030-01-01 09:30Z','Preserve reservation' FROM clinics LIMIT 1; INSERT INTO booking_events(booking_id,version,kind,actor_id,after_document) SELECT id,1,'CREATED','m1-owner',jsonb_build_object('id',id::text) FROM bookings; INSERT INTO booking_idempotency(workspace_id,key,request_sha256,booking_id,response_status,response_document) SELECT workspace_id,'migration-key-0001',repeat('a',64),id,201,jsonb_build_object('id',id::text) FROM bookings"); err != nil {
				t.Fatal(err)
			}
			snapshot := func() string {
				return readString(t, pool, "SELECT json_build_object('bookings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY id) FROM bookings b),'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY booking_id,version) FROM booking_events e),'idempotency',(SELECT jsonb_agg(to_jsonb(i) ORDER BY workspace_id,key) FROM booking_idempotency i))::text")
			}
			stored := snapshot()
			if err = Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal(err)
			}
			if snapshot() != stored {
				t.Fatal("restart changed reservation/audit/replay data")
			}
			if _, err = pool.Exec("UPDATE app_schema_migrations SET sql_sha256=$1 WHERE version=6", strings.Repeat("0", 64)); err != nil {
				t.Fatal(err)
			}
			if err = Ensure(t.Context(), instance, nil); err == nil {
				t.Fatal("tampered booking migration checksum accepted")
			}
		})
	}
}
