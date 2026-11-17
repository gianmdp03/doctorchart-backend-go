package clinical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestClinicalPatientRevisionHTTPConflictAndRollback(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	patient := integrityPatient(t, services[0])
	ctx := t.Context()
	locked, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback(ctx)
	if _, err := locked.Exec(ctx, "SELECT id FROM patients WHERE id=$1 FOR UPDATE", patient.ID); err != nil {
		t.Fatal(err)
	}

	path := fmt.Sprintf("/api/doctorchart/patients/%d?specialtyContextId=%d", patient.ID, patient.ContextID)
	var handlers [2]http.Handler
	for i, service := range services {
		mux := http.NewServeMux()
		service.Routes(mux)
		handlers[i] = api.Gateway("revision-test", api.Dispatch(mux))
	}
	update := func(i, version int) *httptest.ResponseRecorder {
		values := integrityPatientRequest()["values"].(map[string]any)
		values["sys_firstName"] = fmt.Sprintf("Correction %d", i)
		data, _ := json.Marshal(map[string]any{"expectedVersion": version, "values": values, "reason": "Confirmed entry"})
		request := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(data))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Gateway-Secret", "revision-test")
		request.Header.Set("X-User-Id", "integrity-owner")
		request.Header.Set("X-User-Role", "SUPER_ADMIN")
		response := httptest.NewRecorder()
		handlers[i].ServeHTTP(response, request)
		return response
	}
	type outcome struct {
		status int
		code   string
	}
	responses := make(chan outcome, 2)
	for i := range handlers {
		go func() {
			response := update(i, 0)
			var failure api.Error
			_ = json.Unmarshal(response.Body.Bytes(), &failure)
			responses <- outcome{status: response.Code, code: failure.Code}
		}()
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock'
			AND query LIKE 'UPDATE patients SET full_name=%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == len(handlers) {
			break // Both passed the first version check and reached the SQL UPDATE.
		}
		if time.Now().After(deadline) {
			t.Fatal("concurrent requests did not both reach the optimistic UPDATE")
		}
		time.Sleep(15 * time.Millisecond)
	}
	if err := locked.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var successes, conflicts int
	for range handlers {
		select {
		case result := <-responses:
			if result.status == 200 {
				successes++
			} else if result.status == 409 && result.code == "VERSION_CONFLICT" {
				conflicts++
			} else {
				t.Fatalf("unexpected HTTP result %+v", result)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for concurrent HTTP updates")
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent writes: successes=%d conflicts=%d", successes, conflicts)
	}
	var before string
	var version, count int
	assertState := func() {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT p.version, count(r.id), min(r.previous_content->>'full_name')
			FROM patients p LEFT JOIN clinical_revisions r ON r.patient_id=p.id
			WHERE p.id=$1 GROUP BY p.version`, patient.ID).Scan(&version, &count, &before); err != nil ||
			version != 1 || count != 1 || before != patient.FullName {
			t.Fatalf("inconsistent revision rollback: version=%d revisions=%d err=%v", version, count, err)
		}
	}
	assertState()
	if _, err := pool.Exec(ctx, `CREATE FUNCTION reject_fixture_revision() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'Synthetic revision persistence failure'; END $$;
		CREATE TRIGGER reject_fixture_revision BEFORE INSERT ON clinical_revisions
		FOR EACH ROW EXECUTE FUNCTION reject_fixture_revision()`); err != nil {
		t.Fatal(err)
	}
	if response := update(0, 1); response.Code != 500 {
		t.Fatalf("expected an atomic failure, received %d", response.Code)
	}
	assertState()
}

func TestVaccinationCorrectionAndAnnulmentPreserveDose(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	service := services[0]
	patient := integrityPatient(t, service)
	ctx := t.Context()
	var dose string
	if err := pool.QueryRow(ctx, `SELECT a->>'id' FROM vaccination_calendars c
		CROSS JOIN LATERAL jsonb_array_elements(c.definition->'vacunas') v
		CROSS JOIN LATERAL jsonb_array_elements(v->'aplicaciones') a
		WHERE c.id=$1 AND a->'ageRule'<>'null'::jsonb ORDER BY a->>'id' LIMIT 1`, calendarID).Scan(&dose); err != nil {
		t.Fatal(err)
	}
	save := func(version int, applied bool, date, reason string) (SavedVaccination, error) {
		t.Helper()
		body := map[string]any{"expectedVersion": json.Number(fmt.Sprint(version)), "applied": applied, "calendarId": calendarID}
		if date != "" {
			body["appliedAt"] = date
		}
		if reason != "" {
			body["reason"] = reason
		}
		return service.SaveVaccination(ctx, "integrity-owner", patient.ID, dose, body)
	}
	if _, err := save(0, true, "2024-01-02", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := save(1, true, "2024-01-03", "Fecha verificada"); err != nil {
		t.Fatal(err)
	}
	if _, err := save(2, false, "", ""); err == nil {
		t.Fatal("annulment without a reason was accepted")
	}
	if _, err := save(2, false, "", "Aplicación duplicada"); err != nil {
		t.Fatal(err)
	}
	if _, err := save(3, true, "2024-01-04", ""); err != nil {
		t.Fatal(err)
	}
	var amount, active int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE annulled_at IS NULL)
		FROM patient_vaccinations WHERE patient_id=$1 AND dose_id=$2`, patient.ID, dose).Scan(&amount, &active); err != nil || amount != 2 || active != 1 {
		t.Fatalf("dose was lost or duplicated as current: rows=%d current=%d err=%v", amount, active, err)
	}
	var correct, annul int
	if err := pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE operation='CORRECT' AND previous_content->>'applied_at'='2024-01-02'),
		count(*) FILTER (WHERE operation='ANNUL' AND previous_content->>'applied_at'='2024-01-03')
		FROM clinical_revisions WHERE patient_id=$1 AND entity='VACCINATION'`, patient.ID).Scan(&correct, &annul); err != nil || correct != 1 || annul != 1 {
		t.Fatalf("missing vaccination changes: corrections=%d annulments=%d err=%v", correct, annul, err)
	}
}
