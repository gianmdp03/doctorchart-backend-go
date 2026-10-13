// Package db owns baseline creation and structural validation for PostgreSQL 17.
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

// Ensure accepts only a genuinely empty database or an intact baseline 1.
// The hook performs idempotent seed DML under the same transaction and lock.
// No ALTER, adoption, repair or deletion is performed on existing databases.
func Ensure(ctx context.Context, pool *pgxpool.Pool, seed func(context.Context, pgx.Tx) error) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("%s begin baseline: %w", Backend, err)
	}
	defer func() {
		if e := tx.Rollback(ctx); e != nil && !errors.Is(e, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback baseline: %w", e))
		}
	}()
	if _, err = tx.Exec(ctx, "SET LOCAL search_path = public, pg_catalog; SET LOCAL lock_timeout = '10s'; SET LOCAL statement_timeout = '20s'"); err != nil {
		return fmt.Errorf("configure baseline: %w", err)
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1,$2)", LockNamespace, LockKey); err != nil {
		return fmt.Errorf("lock baseline: %w", err)
	}
	var marker bool
	if err = tx.QueryRow(ctx, "SELECT to_regclass('public.app_schema_state') IS NOT NULL").Scan(&marker); err != nil {
		return err
	}
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
	} else {
		var backend, sqlHash, catalogHash string
		var version, count int
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM public.app_schema_state").Scan(&count); err != nil {
			return fmt.Errorf("invalid baseline metadata: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("%s invalid baseline metadata row count", Backend)
		}
		if err = tx.QueryRow(ctx, "SELECT backend,baseline_version,sql_sha256,catalog_sha256 FROM public.app_schema_state WHERE id=1").Scan(&backend, &version, &sqlHash, &catalogHash); err != nil {
			return fmt.Errorf("invalid baseline metadata: %w", err)
		}
		if backend != Backend || version != 1 || sqlHash != digest(Schema) {
			return fmt.Errorf("%s incompatible baseline identity/version/checksum", Backend)
		}
		var live string
		if err = tx.QueryRow(ctx, catalogSQL, []string{}).Scan(&live); err != nil {
			return fmt.Errorf("inspect baseline catalog: %w", err)
		}
		if digest(live) != catalogHash {
			return fmt.Errorf("%s schema drift: live PostgreSQL catalog differs from baseline; no repair performed", Backend)
		}
	}
	if seed != nil {
		if err = seed(ctx, tx); err != nil {
			return fmt.Errorf("seed %s baseline: %w", Backend, err)
		}
	}
	if !marker {
		var live string
		if err = tx.QueryRow(ctx, catalogSQL, []string{}).Scan(&live); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO public.app_schema_state(id,backend,baseline_version,sql_sha256,catalog_sha256) VALUES(1,$1,1,$2,$3)", Backend, digest(Schema), digest(live)); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s baseline: %w", Backend, err)
	}
	return nil
}
