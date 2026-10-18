package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

func schemaFixture(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("DOCTORCHART_SCHEMA_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("DOCTORCHART_SCHEMA_TEST_DATABASE_URL absent; disposable PostgreSQL required")
	}
	address, err := url.Parse(raw)
	if err != nil || (address.Scheme != "postgres" && address.Scheme != "postgresql") ||
		address.Path != "/doctorchart_schema_test" || (address.Hostname() != "localhost" &&
		(net.ParseIP(address.Hostname()) == nil || !net.ParseIP(address.Hostname()).IsLoopback())) {
		t.Fatal("schema tests require a loopback database named doctorchart_schema_test")
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid fixture configuration")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "public"
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// The URL guard above runs before any destructive fixture SQL.
	if _, err := pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	return pool
}

func initializeFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	validator, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), pool, validator); err != nil {
		t.Fatal(err)
	}
}

func assertFixtureReady(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var tables, constraints, specialties, calendars int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname='public'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_catalog.pg_constraint c JOIN pg_catalog.pg_namespace n ON n.oid=c.connamespace WHERE n.nspname='public' AND c.contype='f'`).Scan(&constraints); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM specialties").Scan(&specialties); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM vaccination_calendars").Scan(&calendars); err != nil {
		t.Fatal(err)
	}
	if tables != 23 || constraints != 20 || specialties == 0 || calendars != 1 {
		t.Fatalf("startup produced %d tables, %d foreign keys, %d specialties and %d calendars", tables, constraints, specialties, calendars)
	}
	var expected map[string][]json.RawMessage
	if err := json.Unmarshal(seedJSON, &expected); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"specialties", "wizard_defaults", "chart_definitions", "clinical_calculation_definitions", "specialty_calculator_defaults", "vaccination_calendars", "vaccination_doses"} {
		var count int
		if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+pgx.Identifier{name}.Sanitize()).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != len(expected[name]) {
			t.Fatalf("catalog %s has %d rows, expected %d", name, count, len(expected[name]))
		}
	}
}

func TestEmptySchemaAndRestartPreserveData(t *testing.T) {
	pool := schemaFixture(t)
	for range 3 {
		initializeFixture(t, pool)
	}
	assertFixtureReady(t, pool)
	ctx := context.Background()
	var patientID int64
	err := pool.QueryRow(ctx, `INSERT INTO patients(birth_date,form_definition_snapshot,form_values,formula_context,formula_results,full_name,updated_at,user_id,version,workspace_version)
		VALUES('1990-01-01','{}','{}','{}','{}','Preserved fixture patient',now(),'fixture-owner',0,0) RETURNING id`).Scan(&patientID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE chart_definitions SET title='Preserved custom title' WHERE id=(SELECT min(id) FROM chart_definitions)"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE specialties SET display_name='Admin specialty' WHERE id=(SELECT min(id) FROM specialties); UPDATE clinical_calculation_definitions SET name='Admin calculator' WHERE id=(SELECT min(id) FROM clinical_calculation_definitions)"); err != nil {
		t.Fatal(err)
	}
	catalogSnapshot := func() string {
		t.Helper()
		var result string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('specialties',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM specialties s),'wizards',(SELECT jsonb_agg(to_jsonb(w) ORDER BY id) FROM wizard_defaults w),'calculators',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM clinical_calculation_definitions c),'charts',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM chart_definitions c),'state',(SELECT to_jsonb(s) FROM app_schema_state s))::text`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := catalogSnapshot()
	for range 3 {
		initializeFixture(t, pool)
	}
	if after := catalogSnapshot(); after != before {
		t.Fatal("restart replaced administered catalogs or baseline metadata")
	}
	assertFixtureReady(t, pool)
	var name, title string
	if err := pool.QueryRow(ctx, "SELECT full_name FROM patients WHERE id=$1", patientID).Scan(&name); err != nil || name != "Preserved fixture patient" {
		t.Fatal("restart changed patient data", err)
	}
	if err := pool.QueryRow(ctx, "SELECT title FROM chart_definitions ORDER BY id LIMIT 1").Scan(&title); err != nil || title != "Preserved custom title" {
		t.Fatal("restart replaced an existing chart", err)
	}
	// A complete schema must be accepted without any DDL or data writes.
	cfg := pool.Config()
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	readOnlyPool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnlyPool.Close()
	if err := EnsureSchema(ctx, readOnlyPool); err != nil {
		t.Fatal("complete schema required writes", err)
	}
}

func TestConcurrentSchemaAndCatalogInitialization(t *testing.T) {
	pool := schemaFixture(t)
	var wg sync.WaitGroup
	failures := make(chan error, 16)
	for range 16 {
		wg.Go(func() {
			ctx := context.Background()
			cfg := pool.Config()
			cfg.MaxConns = 1
			instance, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				failures <- err
				return
			}
			defer instance.Close()
			validator, err := wizard.NewValidator()
			if err == nil {
				err = Run(ctx, instance, validator)
			}
			failures <- err
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertFixtureReady(t, pool)
}

func TestPartialSchemaIsRejectedWithoutChanges(t *testing.T) {
	pool := schemaFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "CREATE TABLE specialties(id bigint PRIMARY KEY); INSERT INTO specialties VALUES(42)"); err != nil {
		t.Fatal(err)
	}
	err := EnsureSchema(ctx, pool)
	if err == nil || !strings.Contains(err.Error(), "partial schema") {
		t.Fatal("partial schema accepted", err)
	}
	var preserved bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM specialties WHERE id=42) AND to_regclass('public.patients') IS NULL").Scan(&preserved); err != nil || !preserved {
		t.Fatal("partial schema was modified", err)
	}
}

func TestSchemaFailureRollsBackEveryCreatedTable(t *testing.T) {
	pool := schemaFixture(t)
	ctx := context.Background()
	// An unrelated object collides with an index created after all tables.
	if _, err := pool.Exec(ctx, "CREATE VIEW public.idx_patients_user_id AS SELECT 42 AS id"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSchema(ctx, pool); err == nil {
		t.Fatal("schema collision accepted")
	}
	var tables int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname='public'").Scan(&tables); err != nil || tables != 0 {
		t.Fatal("failed creation left tables behind", tables, err)
	}
	var id int
	if err := pool.QueryRow(ctx, "SELECT id FROM public.idx_patients_user_id").Scan(&id); err != nil || id != 42 {
		t.Fatal("existing object was modified", err)
	}
}

func TestMissingPublicSchemaCanBeInitialized(t *testing.T) {
	pool := schemaFixture(t)
	if _, err := pool.Exec(context.Background(), "DROP SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	initializeFixture(t, pool)
	assertFixtureReady(t, pool)
}

func TestSchemaInitializationHonorsCancellation(t *testing.T) {
	pool := schemaFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := EnsureSchema(ctx, pool); err == nil {
		t.Fatal("canceled initialization succeeded")
	}
	var tables int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM pg_tables WHERE schemaname='public'").Scan(&tables); err != nil || tables != 0 {
		t.Fatal("canceled creation changed schema", fmt.Sprint(tables), err)
	}
}

// Bound tests independently of the application's startup context.
func TestSchemaLockHasBoundedWait(t *testing.T) {
	pool := schemaFixture(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1,$2)", db.LockNamespace, db.LockKey); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if err := EnsureSchema(bounded, pool); err == nil {
		t.Fatal("locked initialization did not time out")
	}
}

func TestFirstVisitIndexDriftRejectedWithoutChanges(t *testing.T) {
	cases := []struct{ name, replacement string }{
		{"missing", ""},
		{"nonunique", "CREATE INDEX clinical_encounters_one_first_visit ON clinical_encounters(patient_id)"},
		{"wrong_predicate", "CREATE UNIQUE INDEX clinical_encounters_one_first_visit ON clinical_encounters(patient_id) WHERE consultation_type='FOLLOW_UP'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := schemaFixture(t)
			initializeFixture(t, pool)
			ctx := context.Background()
			var patientID int64
			if err := pool.QueryRow(ctx, `INSERT INTO patients(birth_date,form_definition_snapshot,form_values,formula_context,formula_results,full_name,updated_at,user_id,version,workspace_version) VALUES('1990-01-01','{}','{}','{}','{}','Preserved duplicate fixture',now(),'fixture-owner',0,0) RETURNING id`).Scan(&patientID); err != nil {
				t.Fatal(err)
			}
			insertEncounter := `INSERT INTO clinical_encounters(patient_id,specialty_id,encounter_date,consultation_type,version,workspace_version,form_values,formula_results,form_definition_snapshot,formula_context,updated_at) VALUES($1,(SELECT min(id) FROM specialties),now(),'FIRST_VISIT',0,0,'{}','{}','{}','{}',now())`
			if _, err := pool.Exec(ctx, insertEncounter, patientID); err != nil {
				t.Fatal(err)
			}
			_, err := pool.Exec(ctx, insertEncounter, patientID)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "23505" {
				t.Fatalf("baseline allowed duplicate FIRST_VISIT: %v", err)
			}
			if _, err := pool.Exec(ctx, "DROP INDEX public.clinical_encounters_one_first_visit"); err != nil {
				t.Fatal(err)
			}
			if tc.replacement != "" {
				if _, err := pool.Exec(ctx, tc.replacement); err != nil {
					t.Fatal(err)
				}
			}
			// The altered fixture permits two rows; startup must neither repair
			// its index nor remove the records or adopt a new catalog hash.
			if _, err := pool.Exec(ctx, insertEncounter, patientID); err != nil {
				t.Fatal(err)
			}
			snapshot := func() string {
				t.Helper()
				var result string
				if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('state',(SELECT to_jsonb(s) FROM app_schema_state s),'index',(SELECT pg_get_indexdef(c.oid) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname='clinical_encounters_one_first_visit'),'encounters',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM clinical_encounters e))::text`).Scan(&result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			before := snapshot()
			validator, err := wizard.NewValidator()
			if err != nil {
				t.Fatal(err)
			}
			for _, initialize := range []func() error{
				func() error { return EnsureSchema(ctx, pool) },
				func() error { return Run(ctx, pool, validator) },
			} {
				if err := initialize(); err == nil || !strings.Contains(err.Error(), "schema drift") {
					t.Fatal("incompatible FIRST_VISIT index accepted", err)
				}
				if after := snapshot(); after != before {
					t.Fatal("failed startup changed metadata, index or clinical records")
				}
			}
			var count int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM clinical_encounters WHERE patient_id=$1", patientID).Scan(&count); err != nil || count != 2 {
				t.Fatal("startup removed duplicate clinical data", count, err)
			}
		})
	}
}

func TestCatalogSeedFailureRollsBackSchemaAndData(t *testing.T) {
	pool := schemaFixture(t)
	validator, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("synthetic failure after all catalog seeds")
	err = db.Ensure(context.Background(), pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := seed(ctx, tx, validator); err != nil {
			return err
		}
		var specialties, calendars int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM specialties").Scan(&specialties); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM vaccination_calendars").Scan(&calendars); err != nil {
			return err
		}
		if specialties == 0 || calendars != 1 {
			return errors.New("catalog hook did not insert both groups")
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal("seed failure was not reported", err)
	}
	var objects int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'").Scan(&objects); err != nil || objects != 0 {
		t.Fatal("failed catalog seed left baseline objects", objects, err)
	}
	initializeFixture(t, pool)
	assertFixtureReady(t, pool)
}
