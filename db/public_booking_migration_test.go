package db

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestPublicBookingMigrationFromM5PreservesClinicalAndReservations(t *testing.T) {
	for _, mode := range []string{"success", "SQL failure", "catalog drift", "late seed failure"} {
		t.Run(mode, func(t *testing.T) {
			pool := bookingUpgradeFixture(t)
			instance := mustPool(t.Context(), pool)
			defer instance.Close()
			tx, err := instance.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			pins := fixtureCatalogPins(t, pool)
			if err = migrateBookings(t.Context(), tx, pins); err != nil {
				t.Fatal(err)
			}
			if err = migrateAttendee(t.Context(), tx, pins); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(t.Context(), "UPDATE app_schema_state SET baseline_version=7,catalog_sha256=$1", pins.attendee); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec("INSERT INTO bookings(workspace_id,clinic_id,patient_id,start_at,end_at,contact_name,attendee_name) SELECT 1,id,1,'2030-01-01 09:00Z','2030-01-01 09:30Z','Requester preserved','Attendee preserved' FROM clinics LIMIT 1"); err != nil {
				t.Fatal(err)
			}
			before := readString(t, pool, "SELECT json_build_object('bookings',(SELECT jsonb_agg(to_jsonb(b)) FROM bookings b),'patients',(SELECT jsonb_agg(to_jsonb(p)) FROM patients p),'agendas',(SELECT jsonb_agg(to_jsonb(c)) FROM clinic_schedules c))::text")
			original := publicBookingMigration
			t.Cleanup(func() { publicBookingMigration = original })
			var seed func(context.Context, pgx.Tx) error
			switch mode {
			case "SQL failure":
				publicBookingMigration += "\nSELECT 1/0;"
			case "catalog drift":
				publicBookingMigration += "\nCREATE TABLE fixture_public_drift(id int);"
			case "late seed failure":
				seed = func(context.Context, pgx.Tx) error { return errors.New("synthetic late seed") }
			}
			err = Ensure(t.Context(), instance, seed)
			publicBookingMigration = original
			if mode != "success" {
				if err == nil || readString(t, pool, "SELECT baseline_version::text FROM app_schema_state") != "7" || readString(t, pool, "SELECT (to_regclass('public.booking_publications') IS NULL)::text") != "true" {
					t.Fatal("public upgrade did not roll back", err)
				}
				if err = Ensure(t.Context(), instance, nil); err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			after := readString(t, pool, "SELECT json_build_object('bookings',(SELECT jsonb_agg(to_jsonb(b)) FROM bookings b),'patients',(SELECT jsonb_agg(to_jsonb(p)) FROM patients p),'agendas',(SELECT jsonb_agg(to_jsonb(c)) FROM clinic_schedules c))::text")
			if before != after {
				t.Fatal("M5/clinical data changed")
			}
			fresh := fixture(t)
			if err = ensureFixture(t.Context(), fresh, nil); err != nil {
				t.Fatal(err)
			}
			if readString(t, pool, catalogSQL, []string{}) != readString(t, fresh, catalogSQL, []string{}) {
				t.Fatal("public fresh/upgrade differ")
			}
			if err = Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal("restart", err)
			}
		})
	}
}
