package db

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// A genuine published v3 installation; never relabel a v4 catalog as v3.
func timezoneUpgradeFixture(t *testing.T) *sql.DB {
	t.Helper()
	pool := baselineFixture(t)
	clinicalFixture(t, pool)
	instance := mustPool(t.Context(), pool)
	defer instance.Close()
	tx, err := instance.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	pins := fixtureCatalogPins(t, pool)
	if err := migrateClinics(t.Context(), tx, pins); err != nil {
		t.Fatal(err)
	}
	if err := migrateEntitlements(t.Context(), tx, pins); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), "UPDATE app_schema_state SET baseline_version=3,catalog_sha256=$1", pins.entitlements); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	return pool
}

func timezoneClinic(t *testing.T, pool *sql.DB, zone, status string) {
	t.Helper()
	if _, err := pool.Exec("INSERT INTO clinics(workspace_id,name,country,province,city,address,timezone,status) VALUES(1,'Preserve clinic','Argentina','BA','City','Synthetic address',$1,$2)", zone, status); err != nil {
		t.Fatal(err)
	}
}

func migrationHistory(t *testing.T, pool *sql.DB) string {
	t.Helper()
	return readString(t, pool, "SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM app_schema_migrations m")
}

func timezoneDataSnapshot(t *testing.T, pool *sql.DB) map[string]string {
	t.Helper()
	values := dataSnapshot(t, pool)
	for _, table := range []string{"clinics", "clinic_entitlement_epoch"} {
		values[table] = readString(t, pool, "SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM "+table+" r")
	}
	values["clinic sequence"] = readString(t, pool, "SELECT last_value::text||':'||is_called::text FROM clinics_id_seq")
	return values
}

func TestTimezoneUpgradePreservesDataAndMatchesFreshInstall(t *testing.T) {
	pool := timezoneUpgradeFixture(t)
	for _, zone := range []string{"UTC", "America/Argentina/Buenos_Aires", "Europe/Madrid", "Africa/Abidjan"} {
		timezoneClinic(t, pool, zone, "ACTIVE")
	}
	before := timezoneDataSnapshot(t, pool)
	initialized := readString(t, pool, "SELECT initialized_at::text FROM app_schema_state")
	for range 2 {
		if err := ensureFixture(t.Context(), pool, nil); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, timezoneDataSnapshot(t, pool)) || readString(t, pool, "SELECT initialized_at::text FROM app_schema_state") != initialized {
			t.Fatal("upgrade/restart changed existing rows")
		}
	}
	if readString(t, pool, "SELECT baseline_version::text FROM app_schema_state") != "10" || readString(t, pool, "SELECT count(*)::text FROM app_schema_migrations") != "9" {
		t.Fatal("missing v4 history")
	}
	fresh := fixture(t)
	if err := ensureFixture(t.Context(), fresh, nil); err != nil {
		t.Fatal(err)
	}
	if readString(t, pool, catalogSQL, []string{}) != readString(t, fresh, catalogSQL, []string{}) {
		t.Fatal("upgrade/fresh catalogs differ")
	}
	if readString(t, pool, "SELECT convalidated::text FROM pg_constraint WHERE conrelid='clinics'::regclass AND conname='clinics_timezone_check'") != "true" {
		t.Fatal("CHECK was not revalidated")
	}
	for _, zone := range []string{"Etc/GMT+3", "Etc/GMT-3", "Etc/GMT0", "Etc/UTC", "Etc/GMT"} {
		_, err := pool.Exec("INSERT INTO clinics(workspace_id,name,country,province,city,address,timezone) VALUES(1,'Rejected','Argentina','BA','City','Address',$1)", zone)
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code != "23514" {
			t.Fatal("CHECK allowed excluded zone", zone, err)
		}
	}
}

func TestTimezoneUpgradeRefusesExcludedRowsWithoutChangingThem(t *testing.T) {
	for i, zone := range []string{"Etc/GMT+3", "Etc/GMT-3", "Etc/GMT0", "Etc/UTC", "Etc/GMT", "Etc/Greenwich"} {
		t.Run(zone, func(t *testing.T) {
			pool := timezoneUpgradeFixture(t)
			timezoneClinic(t, pool, "UTC", "ACTIVE")
			timezoneClinic(t, pool, zone, []string{"ACTIVE", "INACTIVE", "ARCHIVED"}[i%3])
			before, catalog := timezoneDataSnapshot(t, pool), readString(t, pool, catalogSQL, []string{})
			state, history := readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s"), migrationHistory(t, pool)
			for range 2 {
				err := ensureFixture(t.Context(), pool, nil)
				if err == nil || !strings.Contains(err.Error(), "explicit owner reconciliation required") {
					t.Fatal("existing incompatible row must block startup with an actionable error", err)
				}
				if !reflect.DeepEqual(before, timezoneDataSnapshot(t, pool)) || catalog != readString(t, pool, catalogSQL, []string{}) || history != migrationHistory(t, pool) || state != readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s") {
					t.Fatal("blocked upgrade changed data, schema or history")
				}
			}
		})
	}
}

func TestTimezoneUpgradeRollbackAndRetry(t *testing.T) {
	for _, mode := range []string{"SQL failure", "catalog drift", "late seed failure"} {
		t.Run(mode, func(t *testing.T) {
			pool := timezoneUpgradeFixture(t)
			timezoneClinic(t, pool, "UTC", "INACTIVE")
			before, catalog := timezoneDataSnapshot(t, pool), readString(t, pool, catalogSQL, []string{})
			state, history := readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s"), migrationHistory(t, pool)
			original := timezoneMigration
			t.Cleanup(func() { timezoneMigration = original })
			if mode == "SQL failure" {
				timezoneMigration += "; SELECT 1/0;"
			} else if mode == "catalog drift" {
				timezoneMigration += "; CREATE TABLE unexpected_timezone_object(id integer);"
			}
			instance := mustPool(t.Context(), pool)
			defer instance.Close()
			seed := func(ctx context.Context, tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, "UPDATE clinics SET name='must roll back'"); err != nil {
					return err
				}
				return errors.New("synthetic late seed failure")
			}
			var err error
			if mode == "late seed failure" {
				err = Ensure(t.Context(), instance, seed)
			} else {
				err = Ensure(t.Context(), instance, nil)
			}
			if err == nil {
				t.Fatal("expected failed upgrade")
			}
			if !reflect.DeepEqual(before, timezoneDataSnapshot(t, pool)) || catalog != readString(t, pool, catalogSQL, []string{}) || history != migrationHistory(t, pool) || state != readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s") {
				t.Fatal("rollback did not restore v3 data/function/constraint/history")
			}
			timezoneMigration = original
			if err := Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal("retry failed", err)
			}
			if !reflect.DeepEqual(before, timezoneDataSnapshot(t, pool)) {
				t.Fatal("retry changed preserved data")
			}
		})
	}
}
