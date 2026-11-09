package db

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestAttendeeMigrationFromM4AndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "late failure"}[fail], func(t *testing.T) {
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
			if _, err = tx.Exec(t.Context(), "UPDATE app_schema_state SET baseline_version=6,catalog_sha256=$1", pins.bookings); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec("INSERT INTO bookings(workspace_id,clinic_id,patient_id,start_at,end_at,contact_name) SELECT 1,id,1,'2030-01-01 09:00Z','2030-01-01 09:30Z','Original M4' FROM clinics LIMIT 1; INSERT INTO booking_events(booking_id,version,kind,actor_id,after_document) SELECT id,1,'CREATED','m1-owner',jsonb_build_object('contactName',contact_name) FROM bookings; INSERT INTO booking_idempotency(workspace_id,key,request_sha256,booking_id,response_status,response_document) SELECT workspace_id,'m4-existing-retry-key',repeat('a',64),id,201,jsonb_build_object('contactName',contact_name) FROM bookings"); err != nil {
				t.Fatal(err)
			}
			snapshot := func() string {
				return readString(t, pool, "SELECT json_build_object('bookings',(SELECT jsonb_agg(to_jsonb(b)-'attendee_name') FROM bookings b),'events',(SELECT jsonb_agg(to_jsonb(e)) FROM booking_events e),'receipts',(SELECT jsonb_agg(to_jsonb(i)) FROM booking_idempotency i),'patients',(SELECT jsonb_agg(to_jsonb(p)) FROM patients p))::text")
			}
			before := snapshot()
			if fail {
				err = Ensure(t.Context(), instance, func(context.Context, pgx.Tx) error { return errors.New("synthetic late seed") })
				if err == nil {
					t.Fatal("failure committed")
				}
				if readString(t, pool, "SELECT baseline_version::text FROM app_schema_state") != "6" || readString(t, pool, "SELECT count(*)::text FROM information_schema.columns WHERE table_name='bookings' AND column_name='attendee_name'") != "0" || snapshot() != before {
					t.Fatal("M4 rollback lost data")
				}
			}
			if err = Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal(err)
			}
			if snapshot() != before || readString(t, pool, "SELECT COALESCE(attendee_name,contact_name) FROM bookings") != "Original M4" {
				t.Fatal("upgrade changed historical data")
			}
			if err = Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal("restart", err)
			}
			fresh := fixture(t)
			if err = ensureFixture(t.Context(), fresh, nil); err != nil {
				t.Fatal(err)
			}
			if readString(t, pool, catalogSQL, []string{}) != readString(t, fresh, catalogSQL, []string{}) {
				t.Fatal("fresh/upgrade catalog mismatch")
			}
		})
	}
}
