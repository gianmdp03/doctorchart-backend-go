package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestClinicalRLSWithoutQueryFilters(t *testing.T) {
	pool := initializedFixture(t)
	clinicalFixture(t, pool)
	if _, err := pool.Exec(`
		INSERT INTO specialties(id,active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version)
		 VALUES(2,true,'RLS_SECOND',now(),'Fixture two','[]','[]','[]',now(),0);
		INSERT INTO doctor_specialty_contexts(id,workspace_id,specialty_id,definition,version,current_step_key,onboarding_completed,created_at,updated_at)
		 VALUES(3,1,2,'{}',0,'COMPLETE',true,now(),now());
		INSERT INTO patients(id,full_name,birth_date,user_id,version,workspace_version,updated_at,form_definition_snapshot,
		 form_values,formula_context,formula_results,workspace_id,specialty_context_id,national_id)
		 VALUES(3,'Second context','2000-01-01','m1-owner',0,0,now(),'{}','{}','{}','{}',1,3,'12345678');
		UPDATE patients SET national_id='12345678' WHERE id=1;
		INSERT INTO patient_calculation_logs(patient_id,user_id,encounter_id,calculation_code,calculation_name,input_snapshot,formatted_result,created_at)
		 VALUES(1,'m1-owner',1,'fixture','Fixture','{}','Synthetic',now());
		INSERT INTO study_file_deletions(storage_key,requested_at) SELECT stored_file_name,now() FROM patient_medical_studies;
	`); err != nil {
		t.Fatal(err)
	}
	owner := mustPool(t.Context(), pool)
	defer owner.Close()
	if _, err := CheckRuntime(t.Context(), owner, AppRole); err == nil {
		t.Fatal("privileged startup login was accepted")
	}
	cfg := owner.Config()
	cfg.ConnConfig.User = AppRole
	cfg.ConnConfig.Password = os.Getenv("DOCTORCHART_TEST_APP_PASSWORD")
	cfg.MaxConns = 1
	runtime, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx := t.Context()
	if _, err := CheckRuntime(ctx, runtime, AppRole); err != nil {
		t.Fatal(err)
	}
	mediaCfg := owner.Config()
	mediaCfg.ConnConfig.User = MediaRole
	mediaCfg.ConnConfig.Password = os.Getenv("DOCTORCHART_TEST_MEDIA_PASSWORD")
	media, err := pgxpool.NewWithConfig(ctx, mediaCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer media.Close()
	if _, err := CheckRuntime(ctx, media, MediaRole); err != nil {
		t.Fatal(err)
	}
	if _, err := media.Exec(ctx, "SELECT * FROM patients"); err == nil {
		t.Fatal("media login could read patient rows")
	}
	if _, err := media.Exec(ctx, "SELECT patient_id,study_name FROM patient_medical_studies"); err == nil {
		t.Fatal("media login could read clinical study details")
	}
	var mediaReference bool
	if err := media.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM patient_medical_studies WHERE media_asset_id='00000000-0000-0000-0000-000000000001')").Scan(&mediaReference); err != nil || !mediaReference {
		t.Fatalf("recovery could not check its asset reference: %v", err)
	}
	if _, err := runtime.Exec(ctx, "SELECT * FROM media_assets"); err == nil {
		t.Fatal("app login could read the global media catalog")
	}
	if _, err := runtime.Exec(ctx, "SET ROLE migration_fixture"); err == nil {
		t.Fatal("app login could assume the schema owner's role")
	}
	for _, permission := range []struct{ grant, revoke string }{
		{"GRANT TRIGGER ON patients TO doctorchart_app", "REVOKE TRIGGER ON patients FROM doctorchart_app"},
		{"GRANT SELECT (document) ON media_assets TO doctorchart_app", "REVOKE SELECT (document) ON media_assets FROM doctorchart_app"},
		{"GRANT REFERENCES (id) ON patients TO doctorchart_app", "REVOKE REFERENCES (id) ON patients FROM doctorchart_app"},
		{"GRANT EXECUTE ON FUNCTION patient_quota_count() TO PUBLIC", "REVOKE EXECUTE ON FUNCTION patient_quota_count() FROM PUBLIC"},
	} {
		if _, err := pool.Exec(permission.grant); err != nil {
			t.Fatal(err)
		}
		_, validationErr := CheckRuntime(ctx, runtime, AppRole)
		if _, err := pool.Exec(permission.revoke); err != nil {
			t.Fatal(err)
		}
		if validationErr == nil {
			t.Fatal("runtime accepted an overprivileged login")
		}
	}
	conn, err := runtime.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	count := func(q interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}, table string, expected int) {
		t.Helper()
		var got int
		query := "SELECT count(*) FROM public." + pgx.Identifier{table}.Sanitize()
		if err := q.QueryRow(ctx, query).Scan(&got); err != nil || got != expected {
			t.Fatalf("%s without WHERE: got %d want %d: %v", table, got, expected, err)
		}
	}
	tables := []string{"patients", "clinical_encounters", "patient_calculation_logs", "patient_medical_studies",
		"patient_prescription_records", "patient_vaccinations", "prescription_diagnostic_orders", "prescription_medications"}
	for _, table := range tables {
		count(conn, table, 0)
	}
	for _, table := range append(tables, "clinical_revisions") {
		_, err := conn.Exec(ctx, "DELETE FROM public."+pgx.Identifier{table}.Sanitize())
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatalf("ordinary runtime role can delete from %s: %v", table, err)
		}
	}
	for _, statement := range []string{"UPDATE clinical_revisions SET reason='not allowed'", "INSERT INTO clinical_revisions DEFAULT VALUES"} {
		if _, err := conn.Exec(ctx, statement); err == nil {
			t.Fatalf("ordinary runtime role can write history: %s", statement)
		}
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := BindClinicalScope(ctx, tx, "m1-owner", 1); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		count(tx, table, 1)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('doctorchart.revision_actor','m1-owner',true)"); err != nil {
		t.Fatal(err)
	}
	if tag, err := tx.Exec(ctx, "UPDATE patients SET phone='5551234567', version=version+1"); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("unfiltered update exceeded the selected scope: %v %v", tag, err)
	}
	var quota int
	if err := tx.QueryRow(ctx, "SELECT public.patient_quota_count()").Scan(&quota); err != nil || quota != 2 {
		t.Fatalf("quota must include both contexts of this owner: %d, %v", quota, err)
	}
	if tag, err := tx.Exec(ctx, "UPDATE patients SET full_name='not allowed' WHERE id=3"); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("cross-context update: %v %v", tag, err)
	}
	count(tx, "clinical_revisions", 1)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	count(conn, "patients", 0)
	count(conn, "clinical_revisions", 0)
	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := BindClinicalScope(ctx, tx, "m1-owner", 3); err != nil {
		t.Fatal(err)
	}
	count(tx, "patients", 1)
	count(tx, "clinical_revisions", 0)
	count(tx, "patient_medical_studies", 0)
	var referenced bool
	if err := tx.QueryRow(ctx, "SELECT public.study_file_referenced('00000000-0000-0000-0000-000000000003')").Scan(&referenced); err != nil || !referenced {
		t.Fatalf("cleanup lost a reference hidden by RLS: %t, %v", referenced, err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO prescription_diagnostic_orders(record_id,study_description,item_position)
		VALUES(1,'cross-scope',1)`)
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatalf("cross-scope insert was not rejected by RLS: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	count(conn, "patients", 0)

	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := BindClinicalScope(ctx, tx, "m1-other", 1); err != nil {
		t.Fatal(err)
	}
	count(tx, "patients", 0)
	canceled, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := tx.Exec(canceled, "SELECT pg_sleep(2)"); err == nil {
		t.Fatal("canceled statement succeeded")
	}
	_ = tx.Rollback(ctx)
	conn.Release()
	count(runtime, "patients", 0)
}
