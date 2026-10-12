package bootstrap

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	if err := EnsureSchema(context.Background(), pool); err != nil {
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
	if tables != 20 || constraints != 19 || specialties == 0 || calendars != 1 {
		t.Fatalf("startup produced %d tables, %d foreign keys, %d specialties and %d calendars", tables, constraints, specialties, calendars)
	}
}

func TestEmptySchemaAndRestartPreserveData(t *testing.T) {
	pool := schemaFixture(t)
	initializeFixture(t, pool)
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
	initializeFixture(t, pool)
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
	failures := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			ctx := context.Background()
			validator, err := wizard.NewValidator()
			if err == nil {
				err = EnsureSchema(ctx, pool)
			}
			if err == nil {
				err = Run(ctx, pool, validator)
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
	if err == nil || !strings.Contains(err.Error(), "incompleto") {
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
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(17209,41000)"); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if err := EnsureSchema(bounded, pool); err == nil {
		t.Fatal("locked initialization did not time out")
	}
}
