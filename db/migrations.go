package db

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Ordered embedded transitions: published baseline 1 -> clinics 2 -> invalidation fence 3.
// The immutable baseline SQL checksum remains in app_schema_state.sql_sha256;
// its historical baseline_version column now identifies the installed version.
const currentVersion = 3

// Canonical catalogs built independently from the published SQL artifacts.
// PostgreSQL 18 exposes additional NOT NULL constraints, so its contract differs.
// Never regenerate a pin from an existing database to accept drift.
type catalogPins struct {
	baseline     string
	clinics      string
	entitlements string
}

func expectedCatalogs(ctx context.Context, tx pgx.Tx) (catalogPins, error) {
	var major int
	if err := tx.QueryRow(ctx, "SELECT current_setting('server_version_num')::integer / 10000").Scan(&major); err != nil {
		return catalogPins{}, fmt.Errorf("inspect PostgreSQL major version: %w", err)
	}
	return catalogsForMajor(major)
}

func catalogsForMajor(major int) (catalogPins, error) {
	switch major {
	case 17:
		return catalogPins{
			baseline:     "6f58df736f5a4398ee20dc0e5abad8c8913eb7bea7ecc060bb8e1971d14843cc",
			clinics:      "0e02988f698cab459288ef2f65ac99d7a9809e459e2e6eb588ae52dd8a8b5bf2",
			entitlements: "f639a7acae86d355848376c1e677d9cb55b39a35847d23a2887d09e2d676c7e6",
		}, nil
	case 18:
		return catalogPins{
			baseline:     "e992a8133a1af9f510914e5958165a32e7f1cfbddf5b920ec4ae15fdab4f3d2f",
			clinics:      "113e25c68d85cad493b13ac90a07195c9a0be25e75bdfee911f0eb1dc767e34a",
			entitlements: "38079048779b6a01fdd13110d2f154f20159511408fef9d18a88376f77b1eb3b",
		}, nil
	default:
		return catalogPins{}, fmt.Errorf("unsupported PostgreSQL major version %d; no schema changes performed", major)
	}
}

//go:embed migrations/0002_clinics.sql
var clinicsMigration string

func validateCatalog(ctx context.Context, tx pgx.Tx, expected string) error {
	var live string
	if err := tx.QueryRow(ctx, catalogSQL, []string{}).Scan(&live); err != nil {
		return fmt.Errorf("inspect %s schema catalog: %w", Backend, err)
	}
	if digest(live) != expected {
		return fmt.Errorf("%s schema drift: live PostgreSQL catalog differs from expected version; no repair performed", Backend)
	}
	return nil
}

func validateMigration(ctx context.Context, tx pgx.Tx, pins catalogPins, version int) error {
	rows, err := tx.Query(ctx, "SELECT version,sql_sha256,previous_catalog_sha256,catalog_sha256 FROM public.app_schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("read migration history: %w", err)
	}
	defer rows.Close()
	expectedVersion := 2
	for rows.Next() {
		var v int
		var hash, before, after string
		if err := rows.Scan(&v, &hash, &before, &after); err != nil {
			return err
		}
		script, prev, target := clinicsMigration, pins.baseline, pins.clinics
		if v == 3 {
			script, prev, target = entitlementMigration, pins.clinics, pins.entitlements
		}
		if v != expectedVersion || v > version || hash != digest(script) || before != prev || after != target {
			return fmt.Errorf("%s migration version/checksum/catalog mismatch", Backend)
		}
		expectedVersion++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if expectedVersion != version+1 {
		return fmt.Errorf("%s incomplete migration history", Backend)
	}
	return nil
}

func migrateClinics(ctx context.Context, tx pgx.Tx, pins catalogPins) error {
	if _, err := tx.Exec(ctx, clinicsMigration); err != nil {
		return fmt.Errorf("apply %s migration 2: %w", Backend, err)
	}
	if err := validateCatalog(ctx, tx, pins.clinics); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO public.app_schema_migrations(version,sql_sha256,previous_catalog_sha256,catalog_sha256) VALUES($1,$2,$3,$4)", 2, digest(clinicsMigration), pins.baseline, pins.clinics); err != nil {
		return fmt.Errorf("record migration 2: %w", err)
	}
	return nil
}

//go:embed migrations/0003_entitlement_epoch.sql
var entitlementMigration string

func migrateEntitlements(ctx context.Context, tx pgx.Tx, pins catalogPins) error {
	if _, err := tx.Exec(ctx, entitlementMigration); err != nil {
		return fmt.Errorf("apply entitlement migration 3: %w", err)
	}
	if err := validateCatalog(ctx, tx, pins.entitlements); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO public.app_schema_migrations(version,sql_sha256,previous_catalog_sha256,catalog_sha256) VALUES(3,$1,$2,$3)", digest(entitlementMigration), pins.clinics, pins.entitlements)
	return err
}
