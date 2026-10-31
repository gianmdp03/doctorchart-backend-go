package db

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Ordered transitions: baseline 1 -> clinics 2 -> fence 3 -> timezone policy 4.
// The immutable baseline SQL checksum remains in app_schema_state.sql_sha256;
// its historical baseline_version column now identifies the installed version.
const currentVersion = 9

// Canonical catalogs built independently from the published SQL artifacts.
// PostgreSQL 18 exposes additional NOT NULL constraints, so its contract differs.
// Never regenerate a pin from an existing database to accept drift.
type catalogPins struct {
	baseline       string
	clinics        string
	entitlements   string
	timezones      string
	schedules      string
	bookings       string
	attendee       string
	publicBookings string
	secretaries    string
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
			baseline:       "6f58df736f5a4398ee20dc0e5abad8c8913eb7bea7ecc060bb8e1971d14843cc",
			clinics:        "0e02988f698cab459288ef2f65ac99d7a9809e459e2e6eb588ae52dd8a8b5bf2",
			entitlements:   "f639a7acae86d355848376c1e677d9cb55b39a35847d23a2887d09e2d676c7e6",
			timezones:      "85cbcbb463c81876f40babf1d156444bb64793e41388c511bd5c661159b362d6",
			schedules:      "bd320db87155ffabccdc544e1aa5358d36f522cc5708c37613717cf253dd8e08",
			secretaries:    "485cfb1f92969b5c648651c4bee4579799cc05b41dacff101d9a5583b741ee1f",
			publicBookings: "1c8a843fd50a7fbeb64890397d6a22a4853ba30cc8f85e10f285c69eb3bbc973",
			attendee:       "c2956e5b19d3d605b89bc94916cd5e9e6c061faa4ee4b776aa94e71e3279f06c",
			bookings:       "9e6c01047fe66b6f5db8735d0c141f5da98433c31ab3fb631f110709bce013aa",
		}, nil
	case 18:
		return catalogPins{
			baseline:       "e992a8133a1af9f510914e5958165a32e7f1cfbddf5b920ec4ae15fdab4f3d2f",
			clinics:        "113e25c68d85cad493b13ac90a07195c9a0be25e75bdfee911f0eb1dc767e34a",
			entitlements:   "38079048779b6a01fdd13110d2f154f20159511408fef9d18a88376f77b1eb3b",
			timezones:      "1eb0e7bb7b9e3f10aa39c0eaa0101727f304d834ecd5ce9fdbbf77764e73ae7f",
			schedules:      "791155c6db3d29619ae6be407e12fd929249dfc2fd6dddbe130358513be5592a",
			secretaries:    "7f4795bd498e2910a746a9e8f02cceedce5529e92f89c9f1aab46ae71b40ca80",
			publicBookings: "85ab5ab6bc2fea17c5b06ee8328c4bb838bbb8e82e9f875b14b844a94770aeaf",
			attendee:       "39069383dc0633dc8dedb3980de63057f98ad576cab1525e396f1b6ac7a7ce5b",
			bookings:       "a5df22e0e9a90537ed33a4db1c5da8bf68751e733d209af342f14058ba56023b",
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
		if v == 4 {
			script, prev, target = timezoneMigration, pins.entitlements, pins.timezones
		}
		if v == 5 {
			script, prev, target = scheduleMigration, pins.timezones, pins.schedules
		}
		if v == 6 {
			script, prev, target = bookingMigration, pins.schedules, pins.bookings
		}
		if v == 7 {
			script, prev, target = attendeeMigration, pins.bookings, pins.attendee
		}
		if v == 8 {
			script, prev, target = publicBookingMigration, pins.attendee, pins.publicBookings
		}
		if v == 9 {
			script, prev, target = secretaryMigration, pins.publicBookings, pins.secretaries
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

//go:embed migrations/0004_clinic_timezone_policy.sql
var timezoneMigration string

func migrateTimezones(ctx context.Context, tx pgx.Tx, pins catalogPins) error {
	if _, err := tx.Exec(ctx, timezoneMigration); err != nil {
		return fmt.Errorf("apply clinic timezone migration 4: %w", err)
	}
	if err := validateCatalog(ctx, tx, pins.timezones); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO public.app_schema_migrations(version,sql_sha256,previous_catalog_sha256,catalog_sha256) VALUES(4,$1,$2,$3)", digest(timezoneMigration), pins.entitlements, pins.timezones)
	return err
}

//go:embed migrations/0005_clinic_schedules.sql
var scheduleMigration string

func migrateSchedules(ctx context.Context, tx pgx.Tx, pins catalogPins) error {
	if _, err := tx.Exec(ctx, scheduleMigration); err != nil {
		return fmt.Errorf("apply schedule migration 5: %w", err)
	}
	if err := validateCatalog(ctx, tx, pins.schedules); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO public.app_schema_migrations(version,sql_sha256,previous_catalog_sha256,catalog_sha256) VALUES(5,$1,$2,$3)", digest(scheduleMigration), pins.timezones, pins.schedules)
	return err
}

//go:embed migrations/0006_bookings.sql
var bookingMigration string

func migrateBookings(ctx context.Context, tx pgx.Tx, pins catalogPins) error {
	if _, err := tx.Exec(ctx, bookingMigration); err != nil {
		return fmt.Errorf("apply booking migration 6: %w", err)
	}
	if err := validateCatalog(ctx, tx, pins.bookings); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO public.app_schema_migrations(version,sql_sha256,previous_catalog_sha256,catalog_sha256) VALUES(6,$1,$2,$3)", digest(bookingMigration), pins.schedules, pins.bookings)
	return err
}

//go:embed migrations/0007_booking_attendee.sql
var attendeeMigration string

func migrateAttendee(ctx context.Context, tx pgx.Tx, pins catalogPins) error {
	if _, err := tx.Exec(ctx, attendeeMigration); err != nil {
		return fmt.Errorf("apply attendee migration 7: %w", err)
	}
	if err := validateCatalog(ctx, tx, pins.attendee); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO public.app_schema_migrations(version,sql_sha256,previous_catalog_sha256,catalog_sha256) VALUES(7,$1,$2,$3)", digest(attendeeMigration), pins.bookings, pins.attendee)
	return err
}

//go:embed migrations/0008_public_appointments.sql
var publicBookingMigration string

func migratePublicBookings(ctx context.Context, tx pgx.Tx, pins catalogPins) error {
	if _, err := tx.Exec(ctx, publicBookingMigration); err != nil {
		return fmt.Errorf("apply public booking migration 8: %w", err)
	}
	if err := validateCatalog(ctx, tx, pins.publicBookings); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO public.app_schema_migrations(version,sql_sha256,previous_catalog_sha256,catalog_sha256) VALUES(8,$1,$2,$3)", digest(publicBookingMigration), pins.attendee, pins.publicBookings)
	return err
}

//go:embed migrations/0009_secretaries.sql
var secretaryMigration string

func migrateSecretaries(ctx context.Context, tx pgx.Tx, pins catalogPins) error {
	if _, err := tx.Exec(ctx, secretaryMigration); err != nil {
		return fmt.Errorf("apply secretary migration 9: %w", err)
	}
	if err := validateCatalog(ctx, tx, pins.secretaries); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "INSERT INTO public.app_schema_migrations(version,sql_sha256,previous_catalog_sha256,catalog_sha256) VALUES(9,$1,$2,$3)", digest(secretaryMigration), pins.publicBookings, pins.secretaries)
	return err
}
