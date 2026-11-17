package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedBaseline16(t *testing.T, source *sql.DB) {
	t.Helper()
	schema, err := exec.Command("git", "show", "1763153:db/schema.sql").Output()
	if err != nil || digest(string(schema)) != baseline16Checksum {
		t.Fatalf("baseline 16 fixture must match its pinned revision: %v", err)
	}
	seedCopySource(t, source, schema, 16, baseline16Checksum, baseline16Catalogs)
}

func seedCopySource(t *testing.T, source *sql.DB, schema []byte, version int, checksum string, catalogs map[int]string) {
	t.Helper()
	tx, err := source.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("SET LOCAL search_path=public,pg_catalog"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	var major int
	if err := tx.QueryRow("SELECT current_setting('server_version_num')::integer/10000").Scan(&major); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO app_schema_state(id,backend,baseline_version,sql_sha256,catalog_sha256) VALUES(1,$1,$2,$3,$4)", Backend, version, checksum, catalogs[major]); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	clinicalFixture(t, source)
	seedScheduleFixture(t, source)
	if _, err := source.Exec(`
INSERT INTO clinic_entitlement_epoch(id,generation) VALUES(1,14);
UPDATE bookings SET patient_id=1,version=version+1,updated_at=clock_timestamp() WHERE id=1;
UPDATE clinical_encounters SET chief_complaint='Born in baseline 16',
 form_values='{"custom_note":"historical value","followUpDate":"2028-02-29"}',
 form_definition_snapshot='{"categories":[{"key":"old","fields":[{"key":"custom_note","label":"Old field","type":"TEXTAREA"}]}]}'
 WHERE id=1;
UPDATE doctor_specialty_contexts SET definition='{"customFixture":true}' WHERE id=1;
INSERT INTO specialties(id,active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version)
 VALUES(2,false,'INACTIVE_FIXTURE',now(),'Inactive','[]','[]','[]',now(),5);
INSERT INTO doctor_specialty_contexts(workspace_id,specialty_id,definition,version,current_step_key,onboarding_completed,created_at,updated_at)
 VALUES(1,2,'{"inactiveFixture":true}',1,'COMPLETE',true,now(),now());
INSERT INTO patient_calculation_logs(patient_id,user_id,encounter_id,calculation_code,calculation_name,input_snapshot,
 result_value,formatted_result,plain_text_evolution,created_at)
 VALUES(1,'m1-owner',1,'synthetic','Synthetic calculation','{"n":2}',2,'2','Original calculation',now());
SELECT setval('patients_id_seq',75,true);
`); err != nil {
		t.Fatal(err)
	}
}

func TestBaseline17CopyPreservesClinicalRevisionsAndLegacyPublications(t *testing.T) {
	path := os.Getenv("DOCTORCHART_BASELINE17_FIXTURE_SQL")
	if path == "" {
		t.Skip("provide the original baseline 17 SQL artifact")
	}
	schema, err := os.ReadFile(path)
	if err != nil || digest(string(schema)) != baseline17Checksum {
		t.Fatalf("baseline 17 fixture must match its pinned checksum: %v", err)
	}
	source, target := fixture(t), fixture(t)
	seedCopySource(t, source, schema, 17, baseline17Checksum, baseline17Catalogs)
	if _, err := source.Exec(`
INSERT INTO clinical_revisions(entity,entity_id,patient_id,workspace_id,specialty_context_id,
 owner_user_id,actor_user_id,operation,previous_version,new_version,previous_content)
 SELECT 'ENCOUNTER',e.id,p.id,p.workspace_id,p.specialty_context_id,p.user_id,p.user_id,
  'CORRECT',0,1,'{"syntheticRevision":true}'
 FROM clinical_encounters e JOIN patients p ON p.id=e.patient_id WHERE e.id=1;
INSERT INTO wizard_defaults(specialty_id,version,published_version,draft_definition,published_definition,updated_at)
 VALUES(1,1,1,'{"legacy":true}','{"legacy":true}',now());`); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	src, dst := mustPool(ctx, source), mustPool(ctx, target)
	defer src.Close()
	defer dst.Close()
	if err := Ensure(ctx, src, nil); err == nil {
		t.Fatal("startup accepted an older baseline")
	}
	result, err := CopyBaseline17(ctx, src, dst)
	if err != nil || result.Tables != 48 || result.Sequences == 0 {
		t.Fatalf("copy baseline 17: %+v %v", result, err)
	}
	if err := Ensure(ctx, dst, nil); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM clinical_revisions r",
		"SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM clinical_encounters e",
		"SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM doctor_specialty_contexts c",
		"SELECT jsonb_agg(published_definition ORDER BY specialty_id) FROM wizard_defaults",
	} {
		if readString(t, source, query) != readString(t, target, query) {
			t.Fatalf("baseline copy changed %s", query)
		}
	}
	if readString(t, source, "SELECT baseline_version FROM app_schema_state") != "17" ||
		readString(t, target, "SELECT baseline_version FROM app_schema_state") != "20" ||
		readString(t, target, "SELECT count(*) FROM wizard_approvals") != "0" ||
		readString(t, target, "SELECT public.template_available(1)") != "false" {
		t.Fatal("copy changed source version or manufactured a template approval")
	}
}

func TestBaseline18CopyPreservesTemplateDecisionsAndClinicalRevisions(t *testing.T) {
	path := os.Getenv("DOCTORCHART_BASELINE18_FIXTURE_SQL")
	if path == "" {
		t.Skip("provide the original baseline 18 SQL artifact")
	}
	schema, err := os.ReadFile(path)
	if err != nil || digest(string(schema)) != baseline18Checksum {
		t.Fatalf("baseline 18 fixture checksum mismatch: %v", err)
	}
	source, target := fixture(t), fixture(t)
	seedCopySource(t, source, schema, 18, baseline18Checksum, baseline18Catalogs)
	if _, err := source.Exec(`
INSERT INTO clinical_revisions(entity,entity_id,patient_id,workspace_id,specialty_context_id,
 owner_user_id,actor_user_id,operation,previous_version,new_version,previous_content)
 SELECT 'ENCOUNTER',e.id,p.id,p.workspace_id,p.specialty_context_id,p.user_id,p.user_id,
  'CORRECT',0,1,'{"copyFixture":true}'
 FROM clinical_encounters e JOIN patients p ON p.id=e.patient_id WHERE e.id=1;
INSERT INTO wizard_defaults(specialty_id,version,published_version,draft_definition,published_definition,updated_at)
 VALUES(1,1,1,'{"copyFixture":true}','{"copyFixture":true}',now());
WITH current AS (
 SELECT w.specialty_id, public.template_review_content(w.published_definition,
   public.template_experience(s.navigation,s.patient_record_tabs,s.patient_record_actions)) AS content
 FROM wizard_defaults w JOIN specialties s ON s.id=w.specialty_id
), review AS (
 INSERT INTO wizard_approvals(specialty_id,revision,content,fingerprint,actor,kind,reference)
 SELECT specialty_id,0,content,encode(sha256(convert_to(content::text,'UTF8')),'hex'),
  'copy-fixture-reviewer','SYNTHETIC','Disposable copy test' FROM current RETURNING id,specialty_id
) UPDATE wizard_defaults w SET published_approval_id=review.id FROM review WHERE w.specialty_id=review.specialty_id;
INSERT INTO wizard_approval_revocations(approval_id,actor,reason)
 SELECT id,'copy-fixture-admin','Revoked in fixture' FROM wizard_approvals;`); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	src, dst := mustPool(ctx, source), mustPool(ctx, target)
	defer src.Close()
	defer dst.Close()
	copied, err := CopyBaseline18(ctx, src, dst)
	if err != nil || copied.Tables != 50 || copied.Sequences == 0 {
		t.Fatalf("unexpected baseline 18 copy: %+v %v", copied, err)
	}
	if err := Ensure(ctx, dst, nil); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM clinical_revisions r",
		"SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM wizard_approvals r",
		"SELECT jsonb_agg(to_jsonb(r) ORDER BY approval_id) FROM wizard_approval_revocations r",
		"SELECT jsonb_agg(to_jsonb(r) ORDER BY specialty_id) FROM wizard_defaults r",
		"SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM clinical_encounters r",
	} {
		if readString(t, source, query) != readString(t, target, query) {
			t.Fatalf("copy changed %s", query)
		}
	}
	if readString(t, source, "SELECT baseline_version FROM app_schema_state") != "18" ||
		readString(t, target, "SELECT baseline_version FROM app_schema_state") != "20" ||
		readString(t, target, "SELECT count(*) FROM clinical_access_events") != "0" {
		t.Fatal("transition changed the source or created retrospective access events")
	}
}

func TestBaseline19CopyPreservesAccessEvents(t *testing.T) {
	path := os.Getenv("DOCTORCHART_BASELINE19_FIXTURE_SQL")
	if path == "" {
		t.Skip("provide the original baseline 19 SQL artifact")
	}
	schema, err := os.ReadFile(path)
	if err != nil || digest(string(schema)) != baseline19Checksum {
		t.Fatalf("baseline 19 fixture checksum mismatch: %v", err)
	}
	source, target := fixture(t), fixture(t)
	seedCopySource(t, source, schema, 19, baseline19Checksum, baseline19Catalogs)
	if _, err := source.Exec(`INSERT INTO clinical_access_events
		(actor_user_id,actor_kind,operation,entity_id,patient_id,owner_user_id,specialty_context_id,outcome,request_id)
		SELECT user_id,'DOCTOR','PATIENT_READ',id,id,user_id,specialty_context_id,'AUTHORIZED',repeat('a',32)
		FROM patients WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	src, dst := mustPool(ctx, source), mustPool(ctx, target)
	defer src.Close()
	defer dst.Close()
	copied, err := CopyBaseline19(ctx, src, dst)
	if err != nil || copied.Tables != 51 {
		t.Fatalf("unexpected baseline 19 copy %+v: %v", copied, err)
	}
	if err := Ensure(ctx, dst, nil); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM clinical_access_events a",
		"SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM patients p",
		"SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM clinical_encounters e",
	} {
		if readString(t, source, query) != readString(t, target, query) {
			t.Fatalf("copy changed %s", query)
		}
	}
	if readString(t, target, "SELECT count(*) FROM clinical_document_issues") != "0" {
		t.Fatal("transition invented document issues")
	}
}

func TestBaseline16CopyPreservesRowsAndRollsBack(t *testing.T) {
	source := fixture(t)
	target := fixture(t)
	seedBaseline16(t, source)
	ctx := t.Context()
	src, dst := mustPool(ctx, source), mustPool(ctx, target)
	defer src.Close()
	defer dst.Close()
	if err := Ensure(ctx, src, nil); err == nil || !strings.Contains(err.Error(), "identity/version/checksum") {
		t.Fatalf("normal startup accepted baseline 16: %v", err)
	}
	if _, err := CopyBaseline16(ctx, src, src); err == nil {
		t.Fatal("source was accepted as destination")
	}
	busy, err := dst.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackCopy(busy)
	if _, err := busy.Exec(ctx, "SELECT pg_advisory_xact_lock($1,$2)", LockNamespace, LockKey); err != nil {
		t.Fatal(err)
	}
	if _, err := CopyBaseline16(ctx, src, dst); err == nil || !strings.Contains(err.Error(), "target baseline lock unavailable") {
		t.Fatalf("busy target accepted another copy: %v", err)
	}
	rollbackCopy(busy)

	sourceTx, err := src.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackCopy(sourceTx)
	targetTx, err := dst.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackCopy(targetTx)
	failure := errors.New("synthetic pre-commit failure")
	_, err = copyBaseline16(ctx, sourceTx, targetTx, func(_ context.Context, _ pgx.Tx) error {
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("did not reach the final failure checkpoint: %v", err)
	}
	rollbackCopy(targetTx)
	rollbackCopy(sourceTx)
	if readString(t, target, "SELECT to_regclass('public.app_schema_state') IS NULL AND to_regclass('public.patients') IS NULL") != "true" {
		t.Fatal("failed copy left an initialized or partial target")
	}

	copied, err := CopyBaseline16(ctx, src, dst)
	if err != nil || copied.Tables != 47 || copied.Rows < 20 || copied.Sequences == 0 {
		t.Fatalf("unexpected copy result %+v: %v", copied, err)
	}
	t.Logf("preserved %d tables, %d rows, %d sequences", copied.Tables, copied.Rows, copied.Sequences)
	if err := Ensure(ctx, dst, nil); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT full_name || '/' || version FROM patients WHERE id=1",
		"SELECT form_definition_snapshot::text || form_values::text || version FROM clinical_encounters WHERE id=1",
		"SELECT patient_id || '/' || provenance::text FROM bookings WHERE id=1",
		"SELECT specialty_context_id || '/' || definition::text FROM patients p JOIN doctor_specialty_contexts c ON c.id=p.specialty_context_id WHERE p.id=1",
		"SELECT storage_key FROM study_file_deletions",
		"SELECT last_value || '/' || is_called FROM patients_id_seq",
		"SELECT last_value || '/' || is_called FROM clinical_calculation_definitions_id_seq",
	} {
		if readString(t, source, query) != readString(t, target, query) {
			t.Fatalf("baseline copy changed %s", query)
		}
	}
	if readString(t, target, "SELECT count(*) FROM clinical_revisions") != "0" ||
		readString(t, source, "SELECT baseline_version FROM app_schema_state") != "16" {
		t.Fatal("copy invented audit history or updated source")
	}
	if _, err := CopyBaseline16(ctx, src, dst); err == nil || !strings.Contains(err.Error(), "target must be empty") {
		t.Fatalf("repeat copy was not rejected safely: %v", err)
	}
	if _, err := source.Exec("CREATE TABLE public.synthetic_drift(id bigint)"); err != nil {
		t.Fatal(err)
	}
	rejected := fixture(t)
	rejectedPool := mustPool(ctx, rejected)
	defer rejectedPool.Close()
	if _, err := CopyBaseline16(ctx, src, rejectedPool); err == nil || !strings.Contains(err.Error(), "verify source structure") {
		t.Fatalf("source catalog drift accepted: %v", err)
	}

	// Exercise the copied row using production privileges, including the first
	// attributed revision and a cross-context read.
	cfg := dst.Config()
	cfg.ConnConfig.User = AppRole
	cfg.ConnConfig.Password = os.Getenv("DOCTORCHART_TEST_APP_PASSWORD")
	app, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, err := CheckRuntime(ctx, app, AppRole); err != nil {
		t.Fatal(err)
	}
	tx, err := app.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackCopy(tx)
	if err := BindClinicalScope(ctx, tx, "m1-owner", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('doctorchart.revision_actor','m1-owner',true);
		UPDATE clinical_encounters SET chief_complaint='Corrected after copy', version=version+1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	var previous string
	if err := tx.QueryRow(ctx, "SELECT previous_content->>'chief_complaint' FROM clinical_revisions WHERE previous_version=2 AND new_version=3").Scan(&previous); err != nil || previous != "Born in baseline 16" {
		t.Fatalf("first revision lost legacy content: %v", err)
	}
	if err := BindClinicalScope(ctx, tx, "m1-owner", 3); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM clinical_revisions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("revision exposed across contexts: %d, %v", count, err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM clinical_revisions"); err == nil {
		t.Fatal("runtime role deleted revision")
	} else {
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Fatal(err)
		}
	}
}

func BaselineCopyFixture(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	source := fixture(t)
	target := fixture(t)
	seedBaseline16(t, source)
	src, dst := mustPool(t.Context(), source), mustPool(t.Context(), target)
	t.Cleanup(src.Close)
	t.Cleanup(dst.Close)
	return src, dst
}
