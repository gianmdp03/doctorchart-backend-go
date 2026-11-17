package clinical

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestClinicalAccessAuditOwnScopeAndFailure(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	s := services[0]
	patient := integrityPatient(t, s)
	ctx := t.Context()
	var otherContext int64
	if err := pool.QueryRow(ctx, `SELECT c.id FROM doctor_specialty_contexts c
		JOIN doctor_workspaces w ON w.id=c.workspace_id WHERE w.user_id='integrity-other'`).Scan(&otherContext); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Routes(mux)
	handler := api.Gateway("audit-test", api.Dispatch(mux))
	get := func(path, actor, role string, scope int64, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", fmt.Sprintf("/api/doctorchart%s?specialtyContextId=%d", path, scope), nil)
		r.Header.Set("X-Gateway-Secret", "audit-test")
		r.Header.Set("X-User-Id", actor)
		r.Header.Set("X-User-Role", role)
		r.Header.Set("X-User-Plan", "BASIC")
		r.Header.Set("X-User-Plan-Expires-At", "2099-01-01T00:00:00Z")
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	patientPath := fmt.Sprintf("/patients/%d", patient.ID)
	const marker = "synthetic-sensitive-audit-marker"
	authorized := get(patientPath, "integrity-owner", "COMPANY", patient.ContextID, map[string]string{"X-Request-ID": marker, "X-Audit-Actor": marker})
	if authorized.Code != 200 || len(authorized.Header().Get("X-Clinical-Audit-ID")) != 32 {
		t.Fatalf("missing authorized response or correlation: %d", authorized.Code)
	}
	for _, request := range []struct {
		actor, role string
		scope       int64
	}{
		{"integrity-other", "COMPANY", patient.ContextID},
		{"integrity-other", "COMPANY", otherContext},
		{"integrity-owner", "COMPANY", otherContext},
		{"audit-catalog-admin", "SUPER_ADMIN", patient.ContextID},
	} {
		if got := get(patientPath, request.actor, request.role, request.scope, nil); got.Code != 404 {
			t.Fatalf("foreign clinical access: %d", got.Code)
		}
	}
	if got := get(patientPath+"/revisions", "integrity-owner", "COMPANY", patient.ContextID, nil); got.Code != 200 {
		t.Fatal("could not audit historical read", got.Code)
	}
	events := get("/workspace/clinical-access", "integrity-owner", "COMPANY", patient.ContextID, nil)
	var page struct{ Items []accessEvent }
	if err := json.Unmarshal(events.Body.Bytes(), &page); err != nil || events.Code != 200 || len(page.Items) != 2 {
		t.Fatalf("audit scope returned %d with %d events: %v", events.Code, len(page.Items), err)
	}
	if page.Items[0].Operation != "CLINICAL_REVISIONS" || page.Items[1].Operation != "PATIENT_READ" ||
		page.Items[1].Outcome != "AUTHORIZED" || page.Items[1].Actor != "integrity-owner" ||
		page.Items[1].RequestID != authorized.Header().Get("X-Clinical-Audit-ID") ||
		page.Items[1].Version == nil || *page.Items[1].Version != patient.Version ||
		strings.Contains(events.Body.String(), marker) || strings.Contains(events.Body.String(), patient.FullName) {
		t.Fatal("audit projection contains wrong metadata")
	}
	if got := get("/workspace/clinical-access", "audit-catalog-admin", "SUPER_ADMIN", patient.ContextID, nil); got.Code != 404 {
		t.Fatal("catalog admin gained audit access", got.Code)
	}
	if got := get("/workspace/clinical-access", "integrity-other", "COMPANY", patient.ContextID, nil); got.Code != 404 {
		t.Fatal("other doctor gained audit access", got.Code)
	}
	var rejects int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM clinical_access_events WHERE operation='PATIENT_READ' AND outcome='REJECTED'").Scan(&rejects); err != nil || rejects != 4 {
		t.Fatal("rejected accesses were not captured", rejects, err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE doctorchart_app"); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM clinical_access_events").Scan(&visible); err != nil || visible != 0 {
		t.Fatal("unbound audit role saw events", visible, err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM clinical_access_events"); err == nil {
		t.Fatal("runtime role could delete audit history")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE FUNCTION fail_fixture_access() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'synthetic audit failure'; END $$;
		CREATE TRIGGER fail_fixture_access BEFORE INSERT ON clinical_access_events
		FOR EACH ROW EXECUTE FUNCTION fail_fixture_access()`); err != nil {
		t.Fatal(err)
	}
	blocked := get(patientPath, "integrity-owner", "COMPANY", patient.ContextID, nil)
	if blocked.Code != 503 || !strings.Contains(blocked.Body.String(), "CLINICAL_AUDIT_UNAVAILABLE") || strings.Contains(blocked.Body.String(), patient.FullName) {
		t.Fatalf("clinical response was not withheld when audit failed: %d", blocked.Code)
	}
}
