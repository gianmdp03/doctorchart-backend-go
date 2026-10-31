package db

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func scheduleUpgradeFixture(t *testing.T) *pgx.Conn {
	t.Helper()
	pool := timezoneUpgradeFixture(t)
	timezoneClinic(t, pool, "UTC", "ACTIVE")
	instance := mustPool(t.Context(), pool)
	tx, err := instance.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	pins := fixtureCatalogPins(t, pool)
	if err = migrateTimezones(t.Context(), tx, pins); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), "UPDATE app_schema_state SET baseline_version=4,catalog_sha256=$1 WHERE id=1", pins.timezones); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg := instance.Config().ConnConfig.Copy()
	instance.Close()
	conn, err := pgx.ConnectConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}
func TestScheduleUpgradeFromM2RollbackPreservationAndCatalog(t *testing.T) {
	for _, mode := range []string{"success", "SQL failure", "catalog drift", "late seed failure"} {
		t.Run(mode, func(t *testing.T) {
			conn := scheduleUpgradeFixture(t)
			raw := conn.Config().ConnString()
			instance, err := pgxpool.New(t.Context(), raw)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			snapshot := func() string {
				var result string
				err := conn.QueryRow(t.Context(), "SELECT json_build_object('patients',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM patients p),'encounters',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM clinical_encounters e),'clinics',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM clinics c),'history',(SELECT jsonb_agg(to_jsonb(m) ORDER BY version) FROM app_schema_migrations m))::text").Scan(&result)
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			before := snapshot()
			original := scheduleMigration
			t.Cleanup(func() { scheduleMigration = original })
			var seed func(context.Context, pgx.Tx) error
			switch mode {
			case "SQL failure":
				scheduleMigration += "\nSELECT 1/0;"
			case "catalog drift":
				scheduleMigration += "\nCREATE TABLE m3_unexpected(id integer);"
			case "late seed failure":
				seed = func(context.Context, pgx.Tx) error { return errors.New("synthetic seed failure") }
			}
			err = Ensure(t.Context(), instance, seed)
			scheduleMigration = original
			if mode != "success" {
				if err == nil {
					t.Fatal("failed migration committed")
				}
				var version int
				var agendaTable *string
				if e := conn.QueryRow(t.Context(), "SELECT baseline_version,to_regclass('public.clinic_schedules')::text FROM app_schema_state").Scan(&version, &agendaTable); e != nil || version != 4 || agendaTable != nil {
					t.Fatal("rollback changed schema", version, agendaTable, e)
				}
				if !reflect.DeepEqual(before, snapshot()) {
					t.Fatal("rollback changed M2 data/history")
				}
				if err = Ensure(t.Context(), instance, nil); err != nil {
					t.Fatal("retry", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			// Compare clinical rows directly; only the history is allowed to gain version 5.
			var patientName, clinicZone string
			if err = conn.QueryRow(t.Context(), "SELECT full_name,(SELECT timezone FROM clinics LIMIT 1) FROM patients WHERE id=1").Scan(&patientName, &clinicZone); err != nil || patientName != "Preserve patient" || clinicZone != "UTC" {
				t.Fatal(patientName, clinicZone, err)
			}
			if _, err = conn.Exec(t.Context(), "INSERT INTO clinic_schedules(clinic_id,slot_minutes,min_notice_minutes,max_advance_days,weekly,exceptions,version) SELECT id,30,0,30,'[]','[]',7 FROM clinics LIMIT 1"); err != nil {
				t.Fatal(err)
			}
			if err = Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal("restart", err)
			}
			var agendaVersion int
			if err = conn.QueryRow(t.Context(), "SELECT version FROM clinic_schedules LIMIT 1").Scan(&agendaVersion); err != nil || agendaVersion != 7 {
				t.Fatal("restart altered stored agenda", agendaVersion, err)
			}
			var version, count int
			if err = conn.QueryRow(t.Context(), "SELECT baseline_version,(SELECT count(*) FROM app_schema_migrations WHERE version=5) FROM app_schema_state").Scan(&version, &count); err != nil || version != 7 || count != 1 {
				t.Fatal(version, count, err)
			}
			fresh := fixture(t)
			if err = ensureFixture(t.Context(), fresh, nil); err != nil {
				t.Fatal(err)
			}
			var actual string
			if err = conn.QueryRow(t.Context(), catalogSQL, []string{}).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			if actual != readString(t, fresh, catalogSQL, []string{}) {
				t.Fatal("fresh and M2 upgrade catalogs diverge")
			}
		})
	}
}
