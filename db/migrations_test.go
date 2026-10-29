package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Construct an actual pre-M1 installation, not a v2 database with its marker
// edited backwards. The baseline file is the published, unchanged artifact.
func baselineFixture(t *testing.T) *sql.DB {
	t.Helper()
	pool := fixture(t)
	if _, err := pool.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	var catalog string
	if err := pool.QueryRow(catalogSQL, []string{}).Scan(&catalog); err != nil {
		t.Fatal(err)
	}
	if digest(catalog) != fixtureCatalogPins(t, pool).baseline {
		t.Fatal("published baseline no longer matches the independent catalog pin")
	}
	if _, err := pool.Exec("INSERT INTO app_schema_state(id,backend,baseline_version,sql_sha256,catalog_sha256) VALUES(1,$1,1,$2,$3)", Backend, digest(Schema), digest(catalog)); err != nil {
		t.Fatal(err)
	}
	return pool
}

func readString(t *testing.T, pool *sql.DB, query string, args ...any) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func clinicalFixture(t *testing.T, pool *sql.DB) {
	t.Helper()
	// Synthetic data only, including two owners and a complete attachment chain.
	_, err := pool.Exec(`
INSERT INTO specialties(id,active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version)
 VALUES(1,true,'M1_FIXTURE',now(),'Fixture','[]','[]','[]',now(),7);
INSERT INTO doctor_workspaces(id,created_at,current_step_key,onboarding_completed,updated_at,user_id,version,specialty_id,quick_notes)
 VALUES(1,now(),'COMPLETE',true,now(),'m1-owner',3,1,'Preserve my notes'),(2,now(),'COMPLETE',true,now(),'m1-other',2,1,'Other owner');
INSERT INTO patients(id,birth_date,form_definition_snapshot,form_values,formula_context,formula_results,full_name,updated_at,user_id,version,workspace_version,personal_history)
 VALUES(1,'1990-01-01','{}','{}','{}','{}','Preserve patient',now(),'m1-owner',4,3,'{"history":"preserve"}'),
       (2,'2000-01-01','{}','{}','{}','{}','Other patient',now(),'m1-other',1,2,null);
INSERT INTO clinical_encounters(id,consultation_type,encounter_date,form_definition_snapshot,form_values,formula_context,formula_results,updated_at,version,workspace_version,patient_id,specialty_id)
 VALUES(1,'FIRST_VISIT',now(),'{}','{}','{}','{}',now(),2,3,1,1);
INSERT INTO patient_prescription_records(id,author_user_id,created_at,diagnosis_description,general_instructions,type,patient_id)
 VALUES(1,'m1-owner',now(),'Synthetic diagnosis','Preserve instructions','PRESCRIPTION',1);
INSERT INTO prescription_medications(record_id,administration_route,dosage,frequency,instructions,medication_name,item_position)
 VALUES(1,'oral','synthetic','daily','preserve','Fixture medicine',0);
INSERT INTO prescription_diagnostic_orders(record_id,study_description,item_position) VALUES(1,'Preserve diagnostic order',0);
INSERT INTO media_assets(asset_id,document) VALUES('00000000-0000-0000-0000-000000000001','{"asset_id":"00000000-0000-0000-0000-000000000001","version":"1","service":"doctorchart","storage_key":"synthetic-preserved-file"}');
INSERT INTO media_events(event_id,document) VALUES('00000000-0000-0000-0000-000000000002','{"event":"preserve"}');
INSERT INTO patient_medical_studies(id,study_name,media_asset_id,content_type,file_size,original_file_name,stored_file_name,uploaded_at,patient_id)
 VALUES(1,'Synthetic study','00000000-0000-0000-0000-000000000001','application/pdf',42,'original.pdf','00000000-0000-0000-0000-000000000003',now(),1);
INSERT INTO vaccination_calendars(id,definition) VALUES('fixture','{}');
INSERT INTO vaccination_doses(id,calendar_id) VALUES('fixture-dose','fixture');
INSERT INTO patient_vaccinations(id,applied_at,dose_id,patient_id) VALUES(1,'2020-01-01','fixture-dose',1);
INSERT INTO study_file_deletions(storage_key,requested_at) VALUES('00000000-0000-0000-0000-000000000004',now());
SELECT nextval('patients_id_seq');
`)
	if err != nil {
		t.Fatal(err)
	}
}

func dataSnapshot(t *testing.T, pool *sql.DB) map[string]string {
	t.Helper()
	rows, err := pool.Query("SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename NOT IN ('app_schema_state','app_schema_migrations','clinics') ORDER BY tablename")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot := make(map[string]string, len(names))
	for _, name := range names {
		snapshot[name] = readString(t, pool, "SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM "+pgx.Identifier{"public", name}.Sanitize()+" r")
	}
	return snapshot
}

func TestMigrationConvergesAndPreservesClinicalData(t *testing.T) {
	upgraded := baselineFixture(t)
	clinicalFixture(t, upgraded)
	before := dataSnapshot(t, upgraded)
	oldCatalog := readString(t, upgraded, catalogSQL, []string{})
	marker := readString(t, upgraded, "SELECT initialized_at::text FROM app_schema_state")
	sequence := readString(t, upgraded, "SELECT last_value::text || ':' || is_called::text FROM patients_id_seq")
	if err := ensureFixture(t.Context(), upgraded, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, dataSnapshot(t, upgraded)) {
		t.Fatal("upgrade changed existing rows or attachment references")
	}
	if marker != readString(t, upgraded, "SELECT initialized_at::text FROM app_schema_state") || sequence != readString(t, upgraded, "SELECT last_value::text || ':' || is_called::text FROM patients_id_seq") {
		t.Fatal("upgrade replaced original initialization time or clinical sequence state")
	}
	if readString(t, upgraded, "SELECT count(*)::text FROM clinics") != "0" {
		t.Fatal("upgrade invented a place for existing doctors")
	}
	if readString(t, upgraded, "SELECT count(*)::text FROM information_schema.columns WHERE table_schema='public' AND table_name='patients' AND column_name='clinic_id'") != "0" {
		t.Fatal("upgrade associated patients with places")
	}
	newCatalog := readString(t, upgraded, catalogSQL, []string{})
	type object struct {
		Kind       string          `json:"kind"`
		Name       string          `json:"name"`
		Definition json.RawMessage `json:"definition"`
	}
	var oldObjects, newObjects []object
	if err := json.Unmarshal([]byte(oldCatalog), &oldObjects); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(newCatalog), &newObjects); err != nil {
		t.Fatal(err)
	}
	// The legacy catalog contains truncated, repeated constraint names. Compare
	// full object definitions as a multiset, not a map keyed only by name.
	index := map[string]int{}
	for _, obj := range newObjects {
		index[obj.Kind+":"+obj.Name+":"+string(obj.Definition)]++
	}
	for _, obj := range oldObjects {
		key := obj.Kind + ":" + obj.Name + ":" + string(obj.Definition)
		if index[key] == 0 {
			t.Fatalf("upgrade changed a baseline object: %s %s", obj.Kind, obj.Name)
		}
		index[key]--
	}
	fresh := fixture(t)
	if err := ensureFixture(t.Context(), fresh, nil); err != nil {
		t.Fatal(err)
	}
	if readString(t, fresh, catalogSQL, []string{}) != newCatalog {
		t.Fatal("empty installation and conservative upgrade diverged")
	}
	if readString(t, upgraded, "SELECT baseline_version::text FROM app_schema_state") != "2" {
		t.Fatal("old binaries would not reject the upgraded schema version")
	}
}

func TestMigrationRollbackAndRetry(t *testing.T) {
	for _, mode := range []string{"sql failure", "unexpected catalog", "late seed failure"} {
		t.Run(mode, func(t *testing.T) {
			pool := baselineFixture(t)
			clinicalFixture(t, pool)
			before := dataSnapshot(t, pool)
			marker := readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s")
			original := clinicsMigration
			t.Cleanup(func() { clinicsMigration = original })
			switch mode {
			case "sql failure":
				clinicsMigration += "; CREATE TABLE public.clinics(id integer);"
			case "unexpected catalog":
				clinicsMigration += "; CREATE TABLE public.unexpected(id integer);"
			}
			instance := mustPool(t.Context(), pool)
			defer instance.Close()
			var hook func(context.Context, pgx.Tx) error
			if mode == "late seed failure" {
				hook = func(ctx context.Context, tx pgx.Tx) error {
					if _, err := tx.Exec(ctx, "UPDATE patients SET full_name='must roll back'"); err != nil {
						return err
					}
					return errors.New("synthetic seed failure after migration")
				}
			}
			if err := Ensure(t.Context(), instance, hook); err == nil {
				t.Fatal("failed transition accepted")
			}
			if readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s") != marker || !reflect.DeepEqual(before, dataSnapshot(t, pool)) {
				t.Fatal("failed transition changed marker or clinical rows")
			}
			if digest(readString(t, pool, catalogSQL, []string{})) != fixtureCatalogPins(t, pool).baseline {
				t.Fatal("failed transition leaked DDL")
			}
			clinicsMigration = original
			if err := Ensure(t.Context(), instance, nil); err != nil {
				t.Fatal("retry failed", err)
			}
		})
	}
}

func TestMigrationRejectsTamperedHistoryAndDrift(t *testing.T) {
	for _, mutation := range []string{
		"DELETE FROM app_schema_migrations",
		"UPDATE app_schema_migrations SET version=3",
		"UPDATE app_schema_migrations SET sql_sha256=repeat('0',64)",
		"UPDATE app_schema_migrations SET previous_catalog_sha256=repeat('0',64)",
		"UPDATE app_schema_migrations SET catalog_sha256=repeat('0',64)",
		"INSERT INTO app_schema_migrations SELECT 3,sql_sha256,previous_catalog_sha256,catalog_sha256,now() FROM app_schema_migrations WHERE version=2",
		"DROP INDEX clinics_workspace_status_idx",
		"ALTER TABLE clinics DROP CONSTRAINT clinics_workspace_id_fkey",
		"UPDATE app_schema_state SET baseline_version=1",
	} {
		t.Run(mutation, func(t *testing.T) {
			pool := fixture(t)
			if err := ensureFixture(t.Context(), pool, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			before := readString(t, pool, catalogSQL, []string{})
			state := readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s")
			if err := ensureFixture(t.Context(), pool, nil); err == nil {
				t.Fatal("tampered migration accepted")
			}
			if before != readString(t, pool, catalogSQL, []string{}) || state != readString(t, pool, "SELECT to_jsonb(s)::text FROM app_schema_state s") {
				t.Fatal("rejected schema repaired")
			}
		})
	}
	t.Run("forged baseline fingerprint", func(t *testing.T) {
		pool := baselineFixture(t)
		if _, err := pool.Exec("CREATE TABLE unexpected(id integer)"); err != nil {
			t.Fatal(err)
		}
		live := readString(t, pool, catalogSQL, []string{})
		if _, err := pool.Exec("UPDATE app_schema_state SET catalog_sha256=$1", digest(live)); err != nil {
			t.Fatal(err)
		}
		if err := ensureFixture(t.Context(), pool, nil); err == nil {
			t.Fatal("drift adopted from recomputed fingerprint")
		}
		if live != readString(t, pool, catalogSQL, []string{}) {
			t.Fatal("invalid baseline modified")
		}
	})
}

func TestMigrationRejectsLegacyDriftBeforeDDL(t *testing.T) {
	for _, mutation := range []string{
		"ALTER TABLE patients DROP COLUMN phone",
		"UPDATE app_schema_state SET baseline_version=999",
		"UPDATE app_schema_state SET sql_sha256=repeat('0',64)",
		"UPDATE app_schema_state SET backend='other'",
		"DELETE FROM app_schema_state",
	} {
		t.Run(mutation, func(t *testing.T) {
			pool := baselineFixture(t)
			clinicalFixture(t, pool)
			if _, err := pool.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			before := dataSnapshot(t, pool)
			catalog := readString(t, pool, catalogSQL, []string{})
			marker := readString(t, pool, "SELECT COALESCE(jsonb_agg(to_jsonb(s)),'[]'::jsonb)::text FROM app_schema_state s")
			if err := ensureFixture(t.Context(), pool, nil); err == nil {
				t.Fatal("invalid legacy schema accepted")
			}
			if catalog != readString(t, pool, catalogSQL, []string{}) || !reflect.DeepEqual(before, dataSnapshot(t, pool)) || marker != readString(t, pool, "SELECT COALESCE(jsonb_agg(to_jsonb(s)),'[]'::jsonb)::text FROM app_schema_state s") {
				t.Fatal("rejected upgrade changed legacy state")
			}
		})
	}
}

func TestMigrationConcurrentUpgradeAndCancellation(t *testing.T) {
	pool := baselineFixture(t)
	instance := mustPool(t.Context(), pool)
	defer instance.Close()
	blocker, err := instance.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(t.Context(), "SELECT pg_advisory_xact_lock($1,$2)", LockNamespace, LockKey); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	err = Ensure(ctx, instance, nil)
	cancel()
	if err == nil {
		t.Fatal("cancelled upgrade acquired blocked lock")
	}
	if readString(t, pool, "SELECT baseline_version::text FROM app_schema_state") != "1" {
		t.Fatal("cancellation changed version")
	}
	if err := blocker.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 16)
	for range 16 {
		go func() { done <- Ensure(t.Context(), instance, nil) }()
	}
	for range 16 {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	if readString(t, pool, "SELECT count(*)::text FROM app_schema_migrations") != "1" {
		t.Fatal("migration applied more than once")
	}
}

func TestClinicsIntegrityAndRestart(t *testing.T) {
	pool := baselineFixture(t)
	clinicalFixture(t, pool)
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal(err)
	}
	insert := "INSERT INTO clinics(workspace_id,name,country,province,city,address,timezone,status) VALUES($1,$2,'Argentina','Buenos Aires','Mar del Plata','Synthetic 123',$3,$4) RETURNING id"
	var first, second int64
	if err := pool.QueryRow(insert, 1, "A", "America/Argentina/Buenos_Aires", "ACTIVE").Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(insert, 1, "B", "UTC", "INACTIVE").Scan(&second); err != nil || second <= first {
		t.Fatal("identity sequence failed", err)
	}
	if _, err := pool.Exec(insert, 2, "A", "Europe/Madrid", "ARCHIVED"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, query, code string
		args              []any
	}{
		{"unknown owner", insert, "23503", []any{999, "C", "UTC", "ACTIVE"}},
		{"empty name", insert, "23514", []any{1, "  ", "UTC", "ACTIVE"}},
		{"unknown zone", insert, "23514", []any{1, "C", "America/Unknown", "ACTIVE"}},
		{"offset zone", insert, "23514", []any{1, "C", "+03:00", "ACTIVE"}},
		{"abbreviation zone", insert, "23514", []any{1, "C", "ART", "ACTIVE"}},
		{"invalid status", insert, "23514", []any{1, "C", "UTC", "DELETED"}},
		{"negative version", "UPDATE clinics SET version=-1 WHERE id=$1", "23514", []any{first}},
		{"empty address", "UPDATE clinics SET address=' ' WHERE id=$1", "23514", []any{first}},
		{"backwards timestamp", "UPDATE clinics SET updated_at=created_at-interval '1 second' WHERE id=$1", "23514", []any{first}},
		{"owner deletion", "DELETE FROM doctor_workspaces WHERE id=$1", "23503", []any{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(tc.query, tc.args...)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != tc.code {
				t.Fatalf("expected %s, got %v", tc.code, err)
			}
		})
	}
	before := readString(t, pool, "SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM clinics c")
	if err := ensureFixture(t.Context(), pool, nil); err != nil {
		t.Fatal(err)
	}
	if before != readString(t, pool, "SELECT jsonb_agg(to_jsonb(c) ORDER BY id)::text FROM clinics c") {
		t.Fatal("restart changed places")
	}
	if readString(t, pool, "SELECT count(*)::text FROM patients WHERE user_id='m1-owner'") != "1" {
		t.Fatal("creating multiple places changed the owner's patient list")
	}
}

func TestMigrationCanonicalLineEndingsAndTransactionSettings(t *testing.T) {
	pool := baselineFixture(t)
	original := clinicsMigration
	t.Cleanup(func() { clinicsMigration = original })
	clinicsMigration = strings.ReplaceAll(strings.ReplaceAll(original, "\r\n", "\n"), "\n", "\r\n")
	instance := mustPool(t.Context(), pool)
	defer instance.Close()
	if err := Ensure(t.Context(), instance, func(ctx context.Context, tx pgx.Tx) error {
		for setting, expected := range map[string]string{"transaction_isolation": "read committed", "lock_timeout": "10s", "statement_timeout": "20s"} {
			var value string
			if err := tx.QueryRow(ctx, "SHOW "+setting).Scan(&value); err != nil {
				return err
			}
			if value != expected {
				return fmt.Errorf("%s=%s, expected %s", setting, value, expected)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	clinicsMigration = original
	if err := Ensure(t.Context(), instance, nil); err != nil {
		t.Fatal("equivalent migration line endings rejected", err)
	}
}

func fixtureCatalogPins(t *testing.T, pool *sql.DB) catalogPins {
	t.Helper()
	major, err := strconv.Atoi(readString(t, pool, "SELECT (current_setting('server_version_num')::integer / 10000)::text"))
	if err != nil {
		t.Fatal(err)
	}
	pins, err := catalogsForMajor(major)
	if err != nil {
		t.Fatal(err)
	}
	return pins
}

func TestUnknownPostgreSQLMajorFailsClosed(t *testing.T) {
	for _, major := range []int{16, 19} {
		if _, err := catalogsForMajor(major); err == nil {
			t.Fatalf("unsupported major %d accepted", major)
		}
	}
}
