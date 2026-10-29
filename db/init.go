// Package db owns atomic schema creation, upgrades and validation for PostgreSQL 17 and 18.
package db

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

//go:embed schema.sql
var Schema string

const Backend = "doctorchart"
const LockNamespace = 17209
const LockKey = 3

func digest(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ReplaceAll(value, "\r\n", "\n"))))
}

// Ensure accepts an empty database or a recognized, intact schema version.
// Baseline 1 is upgraded through the embedded migration before seed DML, under
// the same transaction and lock. Unknown, partial or altered schemas are refused.
func Ensure(ctx context.Context, pool *pgxpool.Pool, seed func(context.Context, pgx.Tx) error) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("%s begin baseline: %w", Backend, err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if e := tx.Rollback(cleanup); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback baseline: %w", e))
		}
	}()
	if _, err = tx.Exec(ctx, "SET LOCAL search_path = public, pg_catalog; SET LOCAL lock_timeout = '10s'; SET LOCAL statement_timeout = '20s'"); err != nil {
		return fmt.Errorf("configure baseline: %w", err)
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1,$2)", LockNamespace, LockKey); err != nil {
		return fmt.Errorf("lock baseline: %w", err)
	}
	pins, err := expectedCatalogs(ctx, tx)
	if err != nil {
		return err
	}
	var marker bool
	if err = tx.QueryRow(ctx, "SELECT to_regclass('public.app_schema_state') IS NOT NULL").Scan(&marker); err != nil {
		return err
	}
	version := 1
	if !marker {
		var empty bool
		if err = tx.QueryRow(ctx, emptySQL).Scan(&empty); err != nil {
			return err
		}
		if !empty {
			return fmt.Errorf("%s incompatible or partial schema: baseline 1 marker absent; no changes performed", Backend)
		}
		if _, err = tx.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS public"); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, Schema); err != nil {
			return fmt.Errorf("create %s baseline: %w", Backend, err)
		}
		if err = validateCatalog(ctx, tx, pins.baseline); err != nil {
			return err
		}
	} else {
		var backend, sqlHash, catalogHash string
		var count int
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM public.app_schema_state").Scan(&count); err != nil {
			return fmt.Errorf("invalid baseline metadata: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("%s invalid baseline metadata row count", Backend)
		}
		if err = tx.QueryRow(ctx, "SELECT backend,baseline_version,sql_sha256,catalog_sha256 FROM public.app_schema_state WHERE id=1").Scan(&backend, &version, &sqlHash, &catalogHash); err != nil {
			return fmt.Errorf("invalid baseline metadata: %w", err)
		}
		if backend != Backend || (version < 1 || version > currentVersion) || sqlHash != digest(Schema) {
			return fmt.Errorf("%s incompatible baseline identity/version/checksum", Backend)
		}
		expected := pins.baseline
		if version == 2 {
			expected = pins.clinics
		}
		if version == 3 {
			expected = pins.entitlements
		}
		if catalogHash != expected {
			return fmt.Errorf("%s schema drift: recorded catalog differs from expected version %d", Backend, version)
		}
		if err = validateCatalog(ctx, tx, expected); err != nil {
			return err
		}
		if version >= 2 {
			if err = validateMigration(ctx, tx, pins, version); err != nil {
				return err
			}
		}
	}
	if version == 1 {
		if err = migrateClinics(ctx, tx, pins); err != nil {
			return err
		}
	}
	if version < 3 {
		if err = migrateEntitlements(ctx, tx, pins); err != nil {
			return err
		}
	}
	if seed != nil {
		if err = seed(ctx, tx); err != nil {
			return fmt.Errorf("seed %s baseline: %w", Backend, err)
		}
	}
	// Seed hooks may change managed rows, never the structural contract.
	if err = validateCatalog(ctx, tx, pins.entitlements); err != nil {
		return err
	}
	if !marker {
		if _, err = tx.Exec(ctx, "INSERT INTO public.app_schema_state(id,backend,baseline_version,sql_sha256,catalog_sha256) VALUES(1,$1,$2,$3,$4)", Backend, currentVersion, digest(Schema), pins.entitlements); err != nil {
			return err
		}
	} else if version < currentVersion {
		if _, err = tx.Exec(ctx, "UPDATE public.app_schema_state SET baseline_version=$1,catalog_sha256=$2 WHERE id=1", currentVersion, pins.entitlements); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s baseline: %w", Backend, err)
	}
	return nil
}
