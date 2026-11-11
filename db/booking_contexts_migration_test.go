package db

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func schemaThirteenFixture(t *testing.T) *sql.DB {
	t.Helper()
	pool := schemaTwelveFixture(t)
	instance := mustPool(t.Context(), pool)
	defer instance.Close()
	tx, err := instance.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	pins := fixtureCatalogPins(t, pool)
	if err = migrateScheduleBlocks(t.Context(), tx, pins); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), "UPDATE app_schema_state SET baseline_version=13,catalog_sha256=$1", pins.scheduleBlocks); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(`INSERT INTO bookings(workspace_id,clinic_id,start_at,end_at,contact_name,status,version) VALUES
 (1,1,'2027-01-04 08:20Z','2027-01-04 08:40Z','En atención','CHECKED_IN',4),
 (1,1,'2025-01-04 08:00Z','2025-01-04 08:20Z','Completada sin bloque actual','COMPLETED',8),
 (1,1,'2025-01-04 09:00Z','2025-01-04 09:20Z','Ausencia histórica','NO_SHOW',2),
 (1,1,'2027-01-04 09:00Z','2027-01-04 09:20Z','Cancelada','CANCELLED',3),
 (1,1,'2027-01-04 13:05Z','2027-01-04 13:25Z','Confirmada fuera de la agenda actual','CONFIRMED',6);
 INSERT INTO booking_events(booking_id,version,kind,actor_id,after_document) SELECT id,version,'CREATED','synthetic',jsonb_build_object('legacy',true,'status',status) FROM bookings;
 INSERT INTO booking_idempotency(workspace_id,key,request_sha256,booking_id,response_status,response_document) SELECT workspace_id,'legacy-receipt-number-'||id,repeat('a',64),id,201,jsonb_build_object('legacy',true,'id',id::text) FROM bookings;
 INSERT INTO notification_outbox(dedup_key,kind,booking_id,booking_version,payload,due_at) SELECT 'legacy-notice-'||id,'BOOKING_CONFIRMED',id,version,decode('abcd','hex'),now()+interval '1 day' FROM bookings WHERE status='CONFIRMED'`)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestBookingContextsSQLIntegrity(t *testing.T) {
	pool := schemaThirteenFixture(t)
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal(err)
	}
	reject := func(query string) {
		t.Helper()
		_, err := pool.Exec(query)
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code[:2] != "23" {
			t.Fatal("invalid booking mutation accepted", err)
		}
	}
	reject("UPDATE bookings SET specialty_context_id=(SELECT id FROM doctor_specialty_contexts WHERE workspace_id=2),version=version+1 WHERE id=(SELECT min(id) FROM bookings)")
	reject("UPDATE bookings SET patient_id=2,version=version+1 WHERE id=(SELECT min(id) FROM bookings)")
	reject(`UPDATE bookings SET specialty_snapshot=jsonb_set(specialty_snapshot,'{name}','"Reclassified"'),version=version+1 WHERE id=(SELECT min(id) FROM bookings)`)
	reject(`UPDATE bookings SET provenance='{"kind":"SCHEDULE_BLOCK","blockId":"999999"}',version=version+1 WHERE id=(SELECT min(id) FROM bookings)`)
	reject(`INSERT INTO bookings(workspace_id,clinic_id,start_at,end_at,status,contact_name,specialty_context_id,specialty_snapshot,clinic_snapshot,provenance)
 SELECT workspace_id,clinic_id,start_at,end_at,'CANCELLED','Legacy cannot be newly inserted',specialty_context_id,specialty_snapshot,clinic_snapshot,provenance FROM bookings LIMIT 1`)
	if _, err := pool.Exec("UPDATE bookings SET patient_id=1,version=version+1 WHERE id=(SELECT min(id) FROM bookings)"); err != nil {
		t.Fatal(err)
	}
	reject("UPDATE bookings SET patient_id=NULL,version=version+1 WHERE id=(SELECT min(id) FROM bookings)")
}
func legacyBookingSnapshot(t *testing.T, pool *sql.DB) string {
	return readString(t, pool, `SELECT jsonb_build_object(
 'bookings',(SELECT jsonb_agg(to_jsonb(b)-'specialty_context_id'-'specialty_snapshot'-'clinic_snapshot'-'provenance' ORDER BY id) FROM bookings b),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY booking_id,version) FROM booking_events e),
 'receipts',(SELECT jsonb_agg(to_jsonb(i) ORDER BY booking_id) FROM booking_idempotency i),
 'outbox',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM notification_outbox o))::text`)
}
func TestBookingContextsUpgradePreservesUnknownHistory(t *testing.T) {
	for _, mode := range []string{"success", "SQL failure", "catalog drift", "late seed failure", "missing context"} {
		t.Run(mode, func(t *testing.T) {
			pool := schemaThirteenFixture(t)
			before := legacyBookingSnapshot(t, pool)
			state := readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s")
			if mode == "missing context" {
				if _, err := pool.Exec(`INSERT INTO doctor_workspaces(id,user_id,version,created_at,updated_at) VALUES(3,'orphan',0,now(),now());
   INSERT INTO clinics(id,workspace_id,name,country,province,city,address,timezone,status) VALUES(3,3,'Orphan','Argentina','BA','City','Synthetic','UTC','ACTIVE');
   INSERT INTO bookings(workspace_id,clinic_id,start_at,end_at,contact_name) VALUES(3,3,'2027-02-01 09:00Z','2027-02-01 09:30Z','No context')`); err != nil {
					t.Fatal(err)
				}
				before = legacyBookingSnapshot(t, pool)
			}
			original := bookingContextMigration
			t.Cleanup(func() { bookingContextMigration = original })
			var seed func(context.Context, pgx.Tx) error
			switch mode {
			case "SQL failure":
				bookingContextMigration += "\nSELECT 1/0;"
			case "catalog drift":
				bookingContextMigration += "\nCREATE TABLE fixture_booking_context_drift(id int);"
			case "late seed failure":
				seed = func(context.Context, pgx.Tx) error { return errors.New("late fixture seed") }
			}
			instance := mustPool(t.Context(), pool)
			defer instance.Close()
			err := Ensure(t.Context(), instance, seed)
			bookingContextMigration = original
			if mode != "success" {
				if err == nil || state != readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s") || before != legacyBookingSnapshot(t, pool) || readString(t, pool, "SELECT count(*)::text FROM information_schema.columns WHERE table_name='bookings' AND column_name='specialty_context_id'") != "0" {
					t.Fatal("failed migration changed data/schema/state", err)
				}
				if mode == "missing context" {
					return
				}
				if err = Ensure(t.Context(), instance, nil); err != nil {
					t.Fatal("retry", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if before != legacyBookingSnapshot(t, pool) {
				t.Fatal("migration changed bookings/events/receipts/notices")
			}
			if readString(t, pool, "SELECT count(*)::text FROM bookings WHERE specialty_context_id=(SELECT id FROM doctor_specialty_contexts WHERE workspace_id=1) AND provenance=jsonb_build_object('kind','LEGACY_CONTEXT_ONLY') AND specialty_snapshot->>'name'='Fixture'") != "6" {
				t.Fatal("legacy classification missing or invented a block")
			}
			frozen := readString(t, pool, "SELECT jsonb_agg(to_jsonb(b) ORDER BY id)::text FROM bookings b")
			if err = Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal(err)
			}
			if frozen != readString(t, pool, "SELECT jsonb_agg(to_jsonb(b) ORDER BY id)::text FROM bookings b") {
				t.Fatal("restart changed provenance")
			}
			fresh := fixture(t)
			if err = ensureFixture(t.Context(), fresh, nil); err != nil {
				t.Fatal(err)
			}
			if readString(t, pool, catalogSQL, []string{}) != readString(t, fresh, catalogSQL, []string{}) {
				t.Fatal("fresh/upgrade catalog mismatch")
			}
			if _, err = pool.Exec("UPDATE app_schema_migrations SET sql_sha256=repeat('0',64) WHERE version=14"); err != nil {
				t.Fatal(err)
			}
			if err = Ensure(t.Context(), instance, nil); err == nil {
				t.Fatal("tampered migration checksum accepted")
			}
		})
	}
}
