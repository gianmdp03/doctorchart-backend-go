package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) *sql.DB {
	t.Helper()
	raw := os.Getenv("BASELINE_TEST_ADMIN_DSN")
	if raw == "" {
		t.Skip("owned disposable PostgreSQL admin DSN required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Port() != "55439" && u.Port() != "55440" && u.Port() != "55442" && u.Port() != "55444" || u.Path != "/postgres" || u.User.Username() != "migration_fixture" {
		t.Fatal("owned loopback high-port admin DSN required")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("stage3_baseline_%s_%d", Backend, time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec("DROP DATABASE " + name + " WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	return pool
}
func TestBaselineEmptyRestartAndSequences(t *testing.T) {
	pool := fixture(t)
	ctx := t.Context()
	if err := ensureFixture(ctx, pool, nil); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := pool.QueryRow(catalogSQL, []string{}).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow("SELECT count(*) FROM pg_tables WHERE schemaname='public'").Scan(&count); err != nil || count != 23 {
		t.Fatal(count, err)
	}
	// Normal sequence advancement is data, never catalog drift.
	if _, err := pool.Exec("SELECT nextval(quote_ident(schemaname)||'.'||quote_ident(sequencename)) FROM pg_sequences WHERE schemaname='public'"); err != nil {
		t.Fatal(err)
	}
	var initialized time.Time
	if err := pool.QueryRow("SELECT initialized_at FROM app_schema_state WHERE id=1").Scan(&initialized); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := ensureFixture(ctx, pool, nil); err != nil {
			t.Fatal(err)
		}
	}
	var after string
	var timestamp time.Time
	pool.QueryRow(catalogSQL, []string{}).Scan(&after)
	pool.QueryRow("SELECT initialized_at FROM app_schema_state WHERE id=1").Scan(&timestamp)
	if before != after || !initialized.Equal(timestamp) {
		t.Fatal("restart changed catalog or marker")
	}
}
func TestBaselineRejectsDriftWithoutRepair(t *testing.T) {
	for _, mutation := range []string{
		"DROP TABLE app_schema_state", "ALTER TABLE app_schema_state DROP COLUMN initialized_at", "ALTER TABLE app_schema_state ALTER COLUMN backend TYPE varchar(40)", "ALTER TABLE app_schema_state ALTER COLUMN backend DROP NOT NULL", "ALTER TABLE app_schema_state ALTER COLUMN initialized_at DROP DEFAULT", "ALTER TABLE app_schema_state DROP CONSTRAINT app_schema_state_pkey", "ALTER TABLE app_schema_state ADD CONSTRAINT fixture_extra CHECK(backend<>'bad')", "CREATE INDEX fixture_extra ON app_schema_state(backend)", "CREATE TABLE fixture_unknown(id integer)", "CREATE DOMAIN fixture_domain AS integer CHECK(VALUE>0)", "CREATE SCHEMA fixture_namespace", "CREATE SCHEMA pgapp; CREATE TABLE pgapp.fixture_hidden(id integer)", "CREATE OPERATOR public.=== (LEFTARG=integer, RIGHTARG=integer, FUNCTION=pg_catalog.int4eq)", "CREATE AGGREGATE public.fixture_aggregate(integer) (SFUNC=pg_catalog.int4pl, STYPE=integer, INITCOND='0')", "CREATE RULE fixture_rule AS ON UPDATE TO app_schema_state DO ALSO NOTIFY fixture_rule",
		"UPDATE app_schema_state SET baseline_version=999", "UPDATE app_schema_state SET sql_sha256=repeat('0',64)", "UPDATE app_schema_state SET catalog_sha256=repeat('0',64)", "UPDATE app_schema_state SET backend='other'", "DELETE FROM app_schema_state",
		"ALTER TABLE patients DROP COLUMN phone", "DROP INDEX clinical_encounters_one_first_visit", "ALTER TABLE clinical_encounters DROP CONSTRAINT clinical_encounters_consultation_type_check",
	} {
		t.Run(mutation, func(t *testing.T) {
			pool := fixture(t)
			if err := ensureFixture(t.Context(), pool, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			var before string
			pool.QueryRow(catalogSQL, []string{}).Scan(&before)
			if err := ensureFixture(t.Context(), pool, nil); err == nil {
				t.Fatal("incompatible baseline accepted")
			}
			var after string
			pool.QueryRow(catalogSQL, []string{}).Scan(&after)
			if before != after {
				t.Fatal("rejected drift modified catalog")
			}
		})
	}
}
func TestBaselineRejectsUnmarkedAndForeignSchemas(t *testing.T) {
	for _, statement := range []string{Schema, "CREATE TABLE foreign_table(id integer); INSERT INTO foreign_table VALUES(42)", "CREATE DOMAIN foreign_domain AS integer", "CREATE FUNCTION foreign_function() RETURNS integer LANGUAGE SQL AS $$ SELECT 42 $$", "CREATE SCHEMA foreign_namespace", "CREATE SCHEMA pgapp; CREATE TABLE pgapp.hidden(id integer)", "CREATE OPERATOR public.=== (LEFTARG=integer, RIGHTARG=integer, FUNCTION=pg_catalog.int4eq)", `CREATE COLLATION foreign_collation FROM pg_catalog."C"`} {
		pool := fixture(t)
		if _, err := pool.Exec(statement); err != nil {
			t.Fatal(err)
		}
		var before string
		pool.QueryRow(catalogSQL, []string{}).Scan(&before)
		if err := ensureFixture(t.Context(), pool, nil); err == nil {
			t.Fatal("foreign/unmarked database adopted")
		}
		var after string
		pool.QueryRow(catalogSQL, []string{}).Scan(&after)
		if before != after {
			t.Fatal("foreign schema modified")
		}
	}
}
func TestBaselineSQLAndSeedRollback(t *testing.T) {
	pool := fixture(t)
	original := Schema
	t.Cleanup(func() { Schema = original })
	Schema = original + ";CREATE TABLE public.app_schema_state(id integer);"
	if err := ensureFixture(t.Context(), pool, nil); err == nil {
		t.Fatal("failing SQL accepted")
	}
	Schema = original
	var empty bool
	pool.QueryRow(emptySQL).Scan(&empty)
	if !empty {
		t.Fatal("failed SQL leaked objects")
	}
	if err := ensureFixture(t.Context(), pool, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE fixture_seed_side_effect(id integer)"); err != nil {
			return err
		}
		return errors.New("synthetic late seed failure")
	}); err == nil {
		t.Fatal("failed seed accepted")
	}
	pool.QueryRow(emptySQL).Scan(&empty)
	if !empty {
		t.Fatal("failed seed leaked DDL/marker")
	}
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal(err)
	}
}
func TestBaselineSixteenInstancesAndCancelledLock(t *testing.T) {
	pool := fixture(t)
	var current string
	pool.QueryRow("SELECT current_database()").Scan(&current)
	u, _ := url.Parse(os.Getenv("BASELINE_TEST_ADMIN_DSN"))
	u.Path = "/" + current
	start := make(chan struct{})
	done := make(chan error, 16)
	for range 16 {
		instance, _ := sql.Open("pgx", u.String())
		instance.SetMaxOpenConns(1)
		t.Cleanup(func() { instance.Close() })
		go func() { <-start; done <- ensureFixture(context.Background(), instance, nil) }()
	}
	close(start)
	for range 16 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	blocker, err := pool.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = blocker.Exec("SELECT pg_advisory_xact_lock($1,$2)", LockNamespace, LockKey); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	err = ensureFixture(ctx, pool, nil)
	cancel()
	if err == nil {
		t.Fatal("cancelled initialization passed lock")
	}
	blocker.Rollback()
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal("lock leaked", err)
	}
}
func TestBaselineCanonicalLineEndings(t *testing.T) {
	pool := fixture(t)
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal(err)
	}
	original := Schema
	t.Cleanup(func() { Schema = original })
	Schema = strings.ReplaceAll(original, "\n", "\r\n")
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal("equivalent line endings rejected", err)
	}
}

func ensureFixture(ctx context.Context, sqlPool *sql.DB, seed func(context.Context, *sql.Tx) error) error {
	if seed != nil {
		pool := mustPool(ctx, sqlPool)
		defer pool.Close()
		return Ensure(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "CREATE TABLE fixture_seed_side_effect(id integer)"); err != nil {
				return err
			}
			return errors.New("synthetic late seed failure")
		})
	}
	pool := mustPool(ctx, sqlPool)
	defer pool.Close()
	return Ensure(ctx, pool, nil)
}
func mustPool(ctx context.Context, sqlPool *sql.DB) *pgxpool.Pool {
	var name string
	sqlPool.QueryRowContext(context.Background(), "SELECT current_database()").Scan(&name)
	u, _ := url.Parse(os.Getenv("BASELINE_TEST_ADMIN_DSN"))
	u.Path = "/" + name
	pool, err := pgxpool.New(context.Background(), u.String())
	if err != nil {
		panic(err)
	}
	return pool
}
