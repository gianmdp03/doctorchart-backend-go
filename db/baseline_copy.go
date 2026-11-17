package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const baseline16Checksum = "56bebe352e8398439ddee2693b87ba851bf258878231e793d914e28700ff60a0"

var baseline16Catalogs = map[int]string{
	17: "bf375ef91ad4536433488538acc0ec9ea3bdd99cfd101fd02e8d33a9c8a9a7b2",
	18: "560e680b5152477b572da881dfe1013545b75bf4996d16f9883e8717f788d09f",
}

const baseline17Checksum = "30fc8f2ce32f7d0ed6bcd288b434deec06cd74a864ebf141d1b6baa947ece67a"

var baseline17Catalogs = map[int]string{
	17: "928a27e36b208c7182b905504aa199d5e1d01a97b67d22d66782c38595f2b0d0",
	18: "03e0fa856e30b8c57de80541fab68ab3ce4a78654f693d17fbf125568dbda5a8",
}

const baseline18Checksum = "663032774a372a56781313894d4292837eee9688bfaaf271476b8c5bcd5b236a"

var baseline18Catalogs = map[int]string{
	17: "0241ce302decf5f7466148b83e2928145253a818dd056e59445d991331faaa37",
	18: "3cc3811ec4bb2aeace74f2733e2a8e17b19e11c78bd5d33c8a5d0ffebec4bf25",
}

const baseline19Checksum = "496b178c2e868635e087f246f9d034982a8b2ce1640d75820d7ef56173c40efb"

var baseline19Catalogs = map[int]string{
	17: "6eb6bd991351ffb07adba81aa048c32407f30d18126a10b161dcb6f7c81c94dc",
	18: "0d2594b737cdabf70309e1d1c1eafb0dd0dbd67cfe08415c91efe71d35be2844",
}

type BaselineCopyResult struct {
	Tables    int   `json:"tables"`
	Rows      int64 `json:"rows"`
	Sequences int   `json:"sequences"`
}

// CopyBaseline16 opens the source read-only and builds the current baseline in an empty
// destination. The operator must quiesce source writers, including media jobs,
// and preserve the corresponding storage separately.
func CopyBaseline16(ctx context.Context, source, target *pgxpool.Pool) (BaselineCopyResult, error) {
	return copyBaselinePools(ctx, source, target, 16)
}

// CopyBaseline17 copies the verified baseline 17 without changing its source.
func CopyBaseline17(ctx context.Context, source, target *pgxpool.Pool) (BaselineCopyResult, error) {
	return copyBaselinePools(ctx, source, target, 17)
}

// CopyBaseline18 preserves the approved template history in a new baseline.
func CopyBaseline18(ctx context.Context, source, target *pgxpool.Pool) (BaselineCopyResult, error) {
	return copyBaselinePools(ctx, source, target, 18)
}

// CopyBaseline19 preserves the scoped access audit in the new baseline.
func CopyBaseline19(ctx context.Context, source, target *pgxpool.Pool) (BaselineCopyResult, error) {
	return copyBaselinePools(ctx, source, target, 19)
}

func copyBaselinePools(ctx context.Context, source, target *pgxpool.Pool, sourceVersion int) (BaselineCopyResult, error) {
	sourceTx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return BaselineCopyResult{}, err
	}
	defer rollbackCopy(sourceTx)
	targetTx, err := target.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return BaselineCopyResult{}, err
	}
	defer rollbackCopy(targetTx)
	return copyBaseline(ctx, sourceTx, targetTx, sourceVersion, nil)
}

func rollbackCopy(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

type copyDatabase struct {
	major  int
	locale string
}

func inspectCopyDatabase(ctx context.Context, tx pgx.Tx) (copyDatabase, error) {
	var database copyDatabase
	err := tx.QueryRow(ctx, `SELECT current_setting('server_version_num')::integer/10000,
		jsonb_build_array(encoding, datlocprovider, datcollate, datctype, datlocale, daticurules, datcollversion)::text
		FROM pg_database WHERE datname=current_database()`).Scan(&database.major, &database.locale)
	return database, err
}

func copyBaseline16(ctx context.Context, source, target pgx.Tx, beforeCommit func(context.Context, pgx.Tx) error) (BaselineCopyResult, error) {
	return copyBaseline(ctx, source, target, 16, beforeCommit)
}

func copyBaseline(ctx context.Context, source, target pgx.Tx, sourceVersion int, beforeCommit func(context.Context, pgx.Tx) error) (BaselineCopyResult, error) {
	var result BaselineCopyResult
	for _, tx := range []pgx.Tx{source, target} {
		if _, err := tx.Exec(ctx, "SET LOCAL search_path=public,pg_catalog; SET LOCAL lock_timeout='10s'; SET LOCAL statement_timeout='20s'"); err != nil {
			return result, fmt.Errorf("configure copy transaction: %w", err)
		}
	}
	if _, err := source.Exec(ctx, "SELECT pg_advisory_xact_lock_shared($1,$2)", LockNamespace, LockKey); err != nil {
		return result, fmt.Errorf("lock source baseline: %w", err)
	}
	// Fail immediately on startup or another copy of the same target, including
	// the accidental case where both connections point at the source database.
	var locked bool
	if err := target.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1,$2)", LockNamespace, LockKey).Scan(&locked); err != nil {
		return result, fmt.Errorf("lock target baseline: %w", err)
	}
	if !locked {
		return result, fmt.Errorf("target baseline lock unavailable")
	}
	initial, err := inspectCopyDatabase(ctx, source)
	if err != nil {
		return result, fmt.Errorf("inspect source: %w", err)
	}
	destination, err := inspectCopyDatabase(ctx, target)
	if err != nil {
		return result, fmt.Errorf("inspect target: %w", err)
	}
	checksum, catalogs := baseline16Checksum, baseline16Catalogs
	switch sourceVersion {
	case 17:
		checksum, catalogs = baseline17Checksum, baseline17Catalogs
	case 18:
		checksum, catalogs = baseline18Checksum, baseline18Catalogs
	case 19:
		checksum, catalogs = baseline19Checksum, baseline19Catalogs
	}
	if initial != destination || catalogs[initial.major] == "" {
		return result, fmt.Errorf("source and target must share a supported PostgreSQL major, encoding and collation")
	}
	expected, err := catalogForMajor(destination.major)
	if err != nil {
		return result, err
	}
	var empty bool
	if err := target.QueryRow(ctx, emptySQL).Scan(&empty); err != nil {
		return result, fmt.Errorf("inspect empty target: %w", err)
	}
	if !empty {
		return result, fmt.Errorf("target must be empty")
	}
	previousCatalog := catalogs[initial.major]
	if err := validateMarkerVersion(ctx, source, sourceVersion, checksum, previousCatalog); err != nil {
		return result, fmt.Errorf("verify source marker: %w", err)
	}
	if err := validateCatalog(ctx, source, previousCatalog); err != nil {
		return result, fmt.Errorf("verify source structure: %w", err)
	}

	// Load this verified snapshot after the tables and indexes exist and before
	// installing the foreign keys and write triggers. This preserves historical
	// booking snapshots, and the subsequent DDL validates all foreign keys.
	const boundary = "CREATE TRIGGER bookings_context_readiness "
	tablesDDL, remainingDDL, ok := strings.Cut(Schema, boundary)
	if !ok || currentVersion != 20 {
		return result, fmt.Errorf("copy requires the baseline 20 schema")
	}
	if _, err := target.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS public"); err != nil {
		return result, fmt.Errorf("prepare target schema: %w", err)
	}
	if _, err := target.Exec(ctx, tablesDDL); err != nil {
		return result, fmt.Errorf("create target tables: %w", err)
	}
	tables, err := copyTables(ctx, source)
	if err != nil {
		return result, fmt.Errorf("inspect source tables: %w", err)
	}
	for _, table := range tables {
		count, err := copyRows(ctx, source, target, table)
		if err != nil {
			return BaselineCopyResult{}, fmt.Errorf("copy %s: %w", table.name, err)
		}
		result.Tables++
		result.Rows += count
	}
	result.Sequences, err = copySequencePositions(ctx, source, target)
	if err != nil {
		return BaselineCopyResult{}, fmt.Errorf("copy sequence positions: %w", err)
	}
	if _, err := target.Exec(ctx, boundary+remainingDDL); err != nil {
		return BaselineCopyResult{}, fmt.Errorf("install and validate target constraints: %w", err)
	}
	if err := validateCatalog(ctx, target, expected); err != nil {
		return BaselineCopyResult{}, err
	}
	var grantsOK bool
	if err := target.QueryRow(ctx, `SELECT has_table_privilege('doctorchart_app', 'public.clinical_revisions', 'SELECT')
		AND NOT has_table_privilege('doctorchart_app', 'public.clinical_revisions', 'INSERT,UPDATE,DELETE,TRUNCATE')
		AND NOT has_table_privilege('doctorchart_media', 'public.clinical_revisions', 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE')`).Scan(&grantsOK); err != nil {
		return BaselineCopyResult{}, fmt.Errorf("verify revision privileges: %w", err)
	}
	if !grantsOK {
		return BaselineCopyResult{}, fmt.Errorf("unsafe revision privileges on target")
	}
	if _, err := target.Exec(ctx, `INSERT INTO public.app_schema_state(id,backend,baseline_version,sql_sha256,catalog_sha256)
		VALUES(1,$1,$2,$3,$4)`, Backend, currentVersion, digest(Schema), expected); err != nil {
		return BaselineCopyResult{}, fmt.Errorf("record target baseline: %w", err)
	}
	if beforeCommit != nil {
		if err := beforeCommit(ctx, target); err != nil {
			return BaselineCopyResult{}, err
		}
	}
	if err := target.Commit(ctx); err != nil {
		return BaselineCopyResult{}, fmt.Errorf("commit target baseline: %w", err)
	}
	return result, nil
}

type baselineTable struct {
	name    string
	columns []string
	key     []string
}

func copyTables(ctx context.Context, tx pgx.Tx) ([]baselineTable, error) {
	rows, err := tx.Query(ctx, `SELECT t.relname,
		ARRAY(SELECT a.attname::text FROM pg_attribute a WHERE a.attrelid=t.oid
			AND a.attnum>0 AND NOT a.attisdropped AND a.attgenerated='' ORDER BY a.attnum),
		ARRAY(SELECT a.attname::text FROM pg_index i CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY k(num,ord)
			JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.num
			WHERE i.indrelid=t.oid AND i.indisprimary AND k.ord<=i.indnkeyatts ORDER BY k.ord),
		row_security_active(t.oid)
		FROM pg_class t JOIN pg_namespace n ON n.oid=t.relnamespace
		WHERE n.nspname='public' AND t.relkind='r' AND t.relname<>'app_schema_state' ORDER BY t.relname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []baselineTable
	for rows.Next() {
		var table baselineTable
		var filtered bool
		if err := rows.Scan(&table.name, &table.columns, &table.key, &filtered); err != nil {
			return nil, err
		}
		if filtered {
			return nil, fmt.Errorf("source role is filtered by RLS on %s", table.name)
		}
		if len(table.columns) == 0 || len(table.key) == 0 {
			return nil, fmt.Errorf("table %s has no copyable columns or primary key", table.name)
		}
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

func sqlColumns(names []string) string {
	columns := make([]string, len(names))
	for i, name := range names {
		columns[i] = pgx.Identifier{name}.Sanitize()
	}
	return strings.Join(columns, ",")
}

func copyRows(ctx context.Context, source, target pgx.Tx, table baselineTable) (int64, error) {
	name := pgx.Identifier{"public", table.name}.Sanitize()
	columns := sqlColumns(table.columns)
	export := "COPY (SELECT " + columns + " FROM " + name + " ORDER BY " + sqlColumns(table.key) + ") TO STDOUT (FORMAT binary)"
	importSQL := "COPY " + name + " (" + columns + ") FROM STDIN (FORMAT binary)"
	digestBefore, digestAfter := sha256.New(), sha256.New()
	input, output := io.Pipe()
	type transfer struct {
		count int64
		err   error
	}
	done := make(chan transfer, 1)
	go func() {
		tag, err := source.Conn().PgConn().CopyTo(ctx, io.MultiWriter(digestBefore, output), export)
		_ = output.CloseWithError(err)
		done <- transfer{count: tag.RowsAffected(), err: err}
	}()
	loaded, loadErr := target.Conn().PgConn().CopyFrom(ctx, input, importSQL)
	_ = input.CloseWithError(loadErr)
	sent := <-done
	if loadErr != nil {
		return 0, loadErr
	}
	if sent.err != nil {
		return 0, sent.err
	}
	verified, err := target.Conn().PgConn().CopyTo(ctx, digestAfter, export)
	if err != nil {
		return 0, err
	}
	if sent.count != loaded.RowsAffected() || sent.count != verified.RowsAffected() || !bytes.Equal(digestBefore.Sum(nil), digestAfter.Sum(nil)) {
		return 0, fmt.Errorf("destination rows differ from source")
	}
	return sent.count, nil
}

func copySequencePositions(ctx context.Context, source, target pgx.Tx) (int, error) {
	rows, err := source.Query(ctx, "SELECT sequencename FROM pg_sequences WHERE schemaname='public' ORDER BY sequencename")
	if err != nil {
		return 0, err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	for _, name := range names {
		sequence := pgx.Identifier{"public", name}.Sanitize()
		var value int64
		var called bool
		if err := source.QueryRow(ctx, "SELECT last_value, is_called FROM "+sequence).Scan(&value, &called); err != nil {
			return 0, err
		}
		if _, err := target.Exec(ctx, "SELECT setval($1::regclass,$2,$3)", sequence, value, called); err != nil {
			return 0, err
		}
	}
	return len(names), nil
}
