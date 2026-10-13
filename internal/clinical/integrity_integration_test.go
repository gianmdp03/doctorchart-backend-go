package clinical

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
	"github.com/syntah/doctorchart-backend-go/internal/bootstrap"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

// Explicit opt-in; this database contains disposable fixtures only. Separate
// pools represent independent backend instances, sharing no application locks.
func integrityFixture(t *testing.T, patients, records int) ([2]*Service, *pgxpool.Pool) {
	t.Helper()
	var services [2]*Service
	raw := os.Getenv("DOCTORCHART_INTEGRITY_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set DOCTORCHART_INTEGRITY_TEST_DATABASE_URL to a disposable local doctorchart_integrity_test database")
	}
	config, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "doctorchart_integrity_test" || (config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost" && config.ConnConfig.Host != "::1") {
		t.Fatal("integrity tests require a loopback doctorchart_integrity_test database")
	}
	config.ConnConfig.RuntimeParams["search_path"] = "public"
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.EnsureSchema(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	validator, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.Run(context.Background(), pool, validator); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"integrity-owner", "integrity-other"} {
		_, err := pool.Exec(context.Background(), `INSERT INTO doctor_workspaces(created_at,current_step_key,definition,onboarding_completed,updated_at,user_id,version,specialty_id) SELECT now(),'COMPLETE',w.published_definition,true,now(),$1,0,s.id FROM specialties s JOIN wizard_defaults w ON w.specialty_id=s.id ORDER BY s.id LIMIT 1`, owner)
		if err != nil {
			t.Fatal(err)
		}
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"BASIC":{"max_patients":%d,"max_records_per_patient":%d}}`, patients, records)
	}))
	t.Cleanup(fake.Close)
	for i := range services {
		instancePool, err := pgxpool.NewWithConfig(context.Background(), config.Copy())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(instancePool.Close)
		rules := billing.New(fake.URL)
		rules.Load(context.Background())
		directory := t.TempDir()
		if err := os.Chmod(directory, 0700); err != nil {
			t.Fatal(err)
		}
		files, err := storage.New(storage.Config{Type: "local", Directory: directory})
		if err != nil {
			t.Fatal(err)
		}
		services[i], err = New(instancePool, workspace.New(instancePool, validator), validator, rules, files)
		if err != nil {
			t.Fatal(err)
		}
	}
	return services, pool
}

func integrityPatientRequest() map[string]any {
	return map[string]any{"workspaceVersion": json.Number("0"), "values": map[string]any{"sys_firstName": "Synthetic", "sys_lastName": "Integrity", "sys_dni": "12345678", "sys_dob": "2020-01-01", "sys_sex": "MALE"}}
}

func integrityEncounterRequest(id int64, kind string) map[string]any {
	return map[string]any{"patientId": json.Number(fmt.Sprint(id)), "workspaceVersion": json.Number("0"), "consultationType": kind, "values": map[string]any{"chiefComplaint": "Synthetic", "weightKg": json.Number("20"), "heightCm": json.Number("100")}}
}

func integrityPatient(t *testing.T, service *Service) Patient {
	t.Helper()
	p, err := service.CreatePatient(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityPatientRequest())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func integrityPause(t *testing.T, pool *pgxpool.Pool, table, operation string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `CREATE FUNCTION integrity_pause() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_sleep(0.4); RETURN NEW; END$$; CREATE TRIGGER integrity_pause BEFORE `+operation+` ON `+table+` FOR EACH ROW EXECUTE FUNCTION integrity_pause()`); err != nil {
		t.Fatal(err)
	}
}

func integrityConcurrent(t *testing.T, action func(context.Context, int) error) []error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ready := make(chan struct{})
	result := make([]error, 2)
	var wg sync.WaitGroup
	for i := range result {
		wg.Add(1)
		go func() { defer wg.Done(); <-ready; result[i] = action(ctx, i) }()
	}
	close(ready)
	wg.Wait()
	return result
}

func integrityError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Status != status || failure.Code != code {
		t.Fatalf("expected %d/%s, got %v", status, code, err)
	}
}

func integrityOneRejected(t *testing.T, results []error, status int, code string) {
	t.Helper()
	success := 0
	for _, err := range results {
		if err == nil {
			success++
		} else {
			integrityError(t, err, status, code)
		}
	}
	if success != 1 {
		t.Fatalf("expected exactly one committed request, results=%v", results)
	}
}

func TestPatientQuotaConcurrentInstances(t *testing.T) {
	s, pool := integrityFixture(t, 1, 5)
	integrityPause(t, pool, "patients", "INSERT")
	results := integrityConcurrent(t, func(ctx context.Context, i int) error {
		_, err := s[i].CreatePatient(ctx, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityPatientRequest())
		return err
	})
	integrityOneRejected(t, results, 403, "FORBIDDEN")
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM patients WHERE user_id='integrity-owner'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("persisted patients=%d err=%v", count, err)
	}
	if _, err := s[1].CreatePatient(context.Background(), api.Identity{UserID: "integrity-other", Plan: "BASIC"}, integrityPatientRequest()); err != nil {
		t.Fatalf("other owner has an independent quota: %v", err)
	}
}

func TestFirstVisitConcurrentInstances(t *testing.T) {
	// An unlimited record quota isolates FIRST_VISIT uniqueness from quota checks.
	s, pool := integrityFixture(t, -1, -1)
	p := integrityPatient(t, s[0])
	integrityPause(t, pool, "clinical_encounters", "INSERT")
	results := integrityConcurrent(t, func(ctx context.Context, i int) error {
		_, err := s[i].CreateEncounter(ctx, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, "FIRST_VISIT"))
		return err
	})
	integrityOneRejected(t, results, 400, "FIRST_VISIT_ALREADY_EXISTS")
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM clinical_encounters WHERE patient_id=$1", p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("persisted records=%d err=%v", count, err)
	}
}

func TestEncounterQuotaConcurrentFollowUps(t *testing.T) {
	s, pool := integrityFixture(t, -1, 2)
	p := integrityPatient(t, s[0])
	if _, err := s[0].CreateEncounter(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, "FIRST_VISIT")); err != nil {
		t.Fatal(err)
	}
	integrityPause(t, pool, "clinical_encounters", "INSERT")
	results := integrityConcurrent(t, func(ctx context.Context, i int) error {
		_, err := s[i].CreateEncounter(ctx, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, "FOLLOW_UP"))
		return err
	})
	integrityOneRejected(t, results, 403, "FORBIDDEN")
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM clinical_encounters WHERE patient_id=$1", p.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("persisted records=%d err=%v", count, err)
	}
}

func TestClinicalIntegrityAuthorizationAndUnlimited(t *testing.T) {
	s, _ := integrityFixture(t, -1, -1)
	p := integrityPatient(t, s[0])
	_, err := s[0].CreateEncounter(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, "FOLLOW_UP"))
	integrityError(t, err, 400, "FIRST_VISIT_REQUIRED")
	_, err = s[1].CreateEncounter(context.Background(), api.Identity{UserID: "integrity-other", Plan: "BASIC"}, integrityEncounterRequest(p.ID, "FIRST_VISIT"))
	integrityError(t, err, 403, "FORBIDDEN")
	for _, kind := range []string{"FIRST_VISIT", "FOLLOW_UP", "FOLLOW_UP"} {
		if _, err := s[0].CreateEncounter(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, kind)); err != nil {
			t.Fatal(err)
		}
	}
	request := integrityPatientRequest()
	request["workspaceVersion"] = json.Number("1")
	_, err = s[1].CreatePatient(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, request)
	integrityError(t, err, 409, "WORKSPACE_VERSION_CONFLICT")
	_, err = s[1].CreatePatient(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "NONE"}, integrityPatientRequest())
	integrityError(t, err, 403, "FORBIDDEN")
	_, err = s[1].CreatePatient(context.Background(), api.Identity{UserID: "no-workspace", Plan: "BASIC"}, integrityPatientRequest())
	integrityError(t, err, 409, "ONBOARDING_REQUIRED")
}

func TestFirstVisitConcurrentUpdates(t *testing.T) {
	s, pool := integrityFixture(t, -1, -1)
	p := integrityPatient(t, s[0])
	var encounters [2]Encounter
	for i, kind := range []string{"FIRST_VISIT", "FOLLOW_UP"} {
		var err error
		encounters[i], err = s[i].CreateEncounter(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, kind))
		if err != nil {
			t.Fatal(err)
		}
	}
	// Existing behavior permits changing the first visit's type; keep that rule.
	if _, err := pool.Exec(context.Background(), "UPDATE clinical_encounters SET consultation_type='FOLLOW_UP' WHERE patient_id=$1", p.ID); err != nil {
		t.Fatal(err)
	}
	integrityPause(t, pool, "clinical_encounters", "UPDATE")
	results := integrityConcurrent(t, func(ctx context.Context, i int) error {
		_, err := s[i].UpdateEncounter(ctx, "integrity-owner", encounters[i].ID, map[string]any{"expectedVersion": json.Number("0"), "encounterDate": encounters[i].Date, "consultationType": "FIRST_VISIT", "values": integrityEncounterRequest(p.ID, "FIRST_VISIT")["values"]})
		return err
	})
	integrityOneRejected(t, results, 400, "FIRST_VISIT_ALREADY_EXISTS")
}

func TestFirstVisitDatabaseConstraint(t *testing.T) {
	s, pool := integrityFixture(t, -1, -1)
	p := integrityPatient(t, s[0])
	first, err := s[0].CreateEncounter(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, "FIRST_VISIT"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(context.Background(), `INSERT INTO clinical_encounters(patient_id,specialty_id,encounter_date,consultation_type,version,workspace_version,form_values,formula_results,form_definition_snapshot,formula_context,updated_at) SELECT patient_id,specialty_id,encounter_date,consultation_type,version,workspace_version,form_values,formula_results,form_definition_snapshot,formula_context,updated_at FROM clinical_encounters WHERE id=$1`, first.ID)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" || pg.ConstraintName != "clinical_encounters_one_first_visit" {
		t.Fatalf("expected FIRST_VISIT unique violation, got %v", err)
	}
	integrityError(t, clinicalDBError(err), 400, "FIRST_VISIT_ALREADY_EXISTS")
	if _, err := s[1].CreateEncounter(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, "FOLLOW_UP")); err != nil {
		t.Fatal(err)
	}
}

func TestClinicalQuotaZeroAndAdmin(t *testing.T) {
	s, pool := integrityFixture(t, 0, 0)
	identity := api.Identity{UserID: "integrity-owner", Plan: "BASIC"}
	_, err := s[0].CreatePatient(context.Background(), identity, integrityPatientRequest())
	integrityError(t, err, 403, "FORBIDDEN")
	identity.Roles = []string{"ROLE_SUPER_ADMIN"}
	p, err := s[0].CreatePatient(context.Background(), identity, integrityPatientRequest())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s[1].CreateEncounter(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, "FIRST_VISIT"))
	integrityError(t, err, 403, "FORBIDDEN")
	if _, err = s[1].CreateEncounter(context.Background(), identity, integrityEncounterRequest(p.ID, "FIRST_VISIT")); err != nil {
		t.Fatalf("admin retains quota exemption: %v", err)
	}
	_, err = s[0].CreateEncounter(context.Background(), identity, integrityEncounterRequest(p.ID, "FIRST_VISIT"))
	integrityError(t, err, 400, "FIRST_VISIT_ALREADY_EXISTS")
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM clinical_encounters").Scan(&count); err != nil || count != 1 {
		t.Fatalf("unexpected admin records=%d err=%v", count, err)
	}
}

func TestPatientQuotaLockIsOwnerScopedAndCancelable(t *testing.T) {
	s, pool := integrityFixture(t, 1, 1)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT id FROM doctor_workspaces WHERE user_id='integrity-owner' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := s[1].CreatePatient(bounded, api.Identity{UserID: "integrity-other", Plan: "BASIC"}, integrityPatientRequest()); err != nil {
		t.Fatalf("another owner's quota must not block: %v", err)
	}
	waiting, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	if _, err := s[0].CreatePatient(waiting, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityPatientRequest()); err == nil {
		t.Fatal("owner lock was not respected")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM patients WHERE user_id='integrity-owner'").Scan(&count); err != nil || count != 0 {
		t.Fatal("canceled wait persisted a patient", count, err)
	}
}

func TestEncounterOptimisticVersionAfterPatientWait(t *testing.T) {
	s, pool := integrityFixture(t, -1, -1)
	p := integrityPatient(t, s[0])
	e, err := s[0].CreateEncounter(context.Background(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, "FIRST_VISIT"))
	if err != nil {
		t.Fatal(err)
	}
	integrityPause(t, pool, "clinical_encounters", "UPDATE")
	results := integrityConcurrent(t, func(ctx context.Context, i int) error {
		_, err := s[i].UpdateEncounter(ctx, "integrity-owner", e.ID, map[string]any{"expectedVersion": json.Number("0"), "encounterDate": e.Date, "consultationType": "FIRST_VISIT", "values": integrityEncounterRequest(p.ID, "FIRST_VISIT")["values"]})
		return err
	})
	integrityOneRejected(t, results, 409, "VERSION_CONFLICT")
}

func TestEncounterCreateAndPatientDeleteSerialize(t *testing.T) {
	s, pool := integrityFixture(t, -1, -1)
	p := integrityPatient(t, s[0])
	integrityPause(t, pool, "clinical_encounters", "INSERT")
	results := integrityConcurrent(t, func(ctx context.Context, i int) error {
		if i == 0 {
			_, err := s[i].CreateEncounter(ctx, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(p.ID, "FIRST_VISIT"))
			return err
		}
		return s[i].DeletePatient(ctx, "integrity-owner", p.ID)
	})
	if results[1] != nil {
		t.Fatalf("patient deletion failed: %v", results[1])
	}
	if results[0] != nil {
		integrityError(t, results[0], 404, "PATIENT_NOT_FOUND")
	}
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM clinical_encounters WHERE patient_id=$1", p.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("delete left an encounter behind", count, err)
	}
}

func TestClinicalIntegrityHTTPContract(t *testing.T) {
	s, _ := integrityFixture(t, 1, 1)
	mux := http.NewServeMux()
	s[0].Routes(mux)
	s[0].EncounterRoutes(mux)
	app := api.Gateway("integrity-fixture-gateway-secret", api.Dispatch(mux))
	call := func(path, owner string, body map[string]any, status int, code string) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Gateway-Secret", "integrity-fixture-gateway-secret")
		r.Header.Set("X-User-Id", owner)
		r.Header.Set("X-User-Role", "COMPANY")
		r.Header.Set("X-User-Plan", "BASIC")
		r.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
		if code != "" {
			var failure api.Error
			if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil || failure.Code != code || failure.Errors == nil {
				t.Fatalf("invalid clinical error contract: %s (%v)", w.Body.String(), err)
			}
		}
		return w
	}
	patient := call("/api/doctorchart/patients", "integrity-owner", integrityPatientRequest(), 201, "")
	var p Patient
	if err := json.Unmarshal(patient.Body.Bytes(), &p); err != nil || p.ID == 0 || p.WorkspaceVersion != 0 {
		t.Fatal("patient camelCase response contract", err)
	}
	if patient.Header().Get("Location") != fmt.Sprintf("/api/doctorchart/patients/%d", p.ID) {
		t.Fatal("patient Location contract changed")
	}
	call("/api/doctorchart/patients", "integrity-owner", integrityPatientRequest(), 403, "FORBIDDEN")
	call("/api/doctorchart/encounters", "integrity-other", integrityEncounterRequest(p.ID, "FIRST_VISIT"), 403, "FORBIDDEN")
	first := call("/api/doctorchart/encounters", "integrity-owner", integrityEncounterRequest(p.ID, "FIRST_VISIT"), 201, "")
	var e Encounter
	if err := json.Unmarshal(first.Body.Bytes(), &e); err != nil || e.ID == 0 || e.PatientID != p.ID || e.Type != "FIRST_VISIT" {
		t.Fatal("encounter camelCase response contract", err)
	}
	if first.Header().Get("Location") != fmt.Sprintf("/api/doctorchart/encounters/%d", e.ID) {
		t.Fatal("encounter Location contract changed")
	}
	call("/api/doctorchart/encounters", "integrity-owner", integrityEncounterRequest(p.ID, "FIRST_VISIT"), 400, "FIRST_VISIT_ALREADY_EXISTS")
	call("/api/doctorchart/encounters", "integrity-owner", integrityEncounterRequest(p.ID, "FOLLOW_UP"), 403, "FORBIDDEN")
}
