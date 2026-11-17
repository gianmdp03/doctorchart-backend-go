package clinical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/testfixture"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSameDoctorMultipleContextClinicalHTTPIsolation(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	s := services[0]
	ctx := t.Context()
	a, err := s.workspaces.Setup(ctx, "integrity-owner")
	if err != nil {
		t.Fatal(err)
	}
	var dermatology int64
	if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code='dermatology'").Scan(&dermatology); err != nil {
		t.Fatal(err)
	}
	b, err := s.workspaces.AddContext(ctx, "integrity-owner", map[string]any{"specialtyId": json.Number(fmt.Sprint(dermatology))})
	if err != nil {
		t.Fatal(err)
	}
	b, err = s.workspaces.Edit(ctx, "integrity-owner", "finish", map[string]any{"specialtyContextId": json.Number(fmt.Sprint(b.ContextID)), "expectedVersion": json.Number(fmt.Sprint(b.Version))})
	if err != nil {
		t.Fatal(err)
	}
	ctxA := api.WithSpecialtyContext(ctx, a.ContextID)
	ctxB := api.WithSpecialtyContext(ctx, b.ContextID)
	pA, err := s.CreatePatient(ctxA, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityPatientRequest())
	if err != nil {
		t.Fatal(err)
	}
	bodyB := integrityPatientRequest()
	bodyB["workspaceVersion"] = json.Number(fmt.Sprint(b.Version))
	pB, err := s.CreatePatient(ctxB, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, bodyB)
	if err != nil {
		t.Fatal(err)
	}
	if pA.ID == pB.ID || pA.ContextID != a.ContextID || pB.ContextID != b.ContextID || *pA.NationalID != *pB.NationalID {
		t.Fatal("records merged or missing context")
	}
	eA, err := s.CreateEncounter(ctxA, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(pA.ID, "FIRST_VISIT"))
	if err != nil {
		t.Fatal(err)
	}
	encounterB := integrityEncounterRequest(pB.ID, "FIRST_VISIT")
	encounterB["workspaceVersion"] = json.Number(fmt.Sprint(b.Version))
	encounterB["values"] = map[string]any{"chiefComplaint": "Dermatología sintética"}
	if _, err = s.CreateEncounter(ctxB, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, encounterB); err != nil {
		t.Fatal("independent first visit", err)
	}
	if _, err = s.CreateEncounter(ctxA, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, integrityEncounterRequest(pA.ID, "FOLLOW_UP")); err != nil {
		t.Fatal(err)
	}
	rx, err := s.CreatePrescription(ctxA, "integrity-owner", pA.ID, map[string]any{"type": "STUDY_ORDERS", "diagnosisDescription": "Synthetic", "generalInstructions": "Preserve", "prescriptions": []any{}, "diagnosticOrders": []any{"Synthetic study"}})
	if err != nil {
		t.Fatal(err)
	}
	config := pool.Config()
	config.ConnConfig.User = db.AppRole
	config.ConnConfig.Password = os.Getenv("DOCTORCHART_TEST_APP_PASSWORD")
	runtimePool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimePool.Close()
	if _, err := db.CheckRuntime(ctx, runtimePool, db.AppRole); err != nil {
		t.Fatal(err)
	}
	restricted, err := New(runtimePool, workspace.New(runtimePool, s.forms), s.forms, s.rules, s.files)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	restricted.Routes(mux)
	restricted.DirectoryRoutes(mux)
	restricted.EncounterRoutes(mux)
	restricted.PrescriptionRoutes(mux)
	restricted.GrowthRoutes(mux)
	restricted.VaccinationRoutes(mux)
	restricted.CalculatorRoutes(mux)
	restricted.StudyRoutes(mux)
	restricted.PrintRoutes(mux)
	handler := api.Gateway("context-fixture", api.Dispatch(mux))
	call := func(method, path, owner, scope string, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/doctorchart"+path, bytes.NewReader(data))
		if scope != "" {
			q := r.URL.Query()
			q.Set("specialtyContextId", scope)
			r.URL.RawQuery = q.Encode()
		}
		r.Header.Set("X-Gateway-Secret", "context-fixture")
		r.Header.Set("X-User-Id", owner)
		r.Header.Set("X-User-Role", "SUPER_ADMIN")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	assertCode := func(w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status || code != "" && !strings.Contains(w.Body.String(), `"code":"`+code+`"`) {
			t.Fatalf("HTTP %d %s", w.Code, w.Body)
		}
	}
	assertCode(call("GET", "/patients", "integrity-owner", "", nil), 400, "SPECIALTY_CONTEXT_REQUIRED")
	assertCode(call("GET", "/patients", "integrity-owner", "bad", nil), 400, "INVALID_REQUEST")
	assertCode(call("GET", "/patients", "integrity-other", fmt.Sprint(b.ContextID), nil), 404, "SPECIALTY_CONTEXT_NOT_FOUND")
	assertCode(call("POST", "/patients", "integrity-other", fmt.Sprint(b.ContextID), integrityPatientRequest()), 404, "SPECIALTY_CONTEXT_NOT_FOUND")
	for _, path := range []string{
		fmt.Sprintf("/patients/%d", pA.ID), fmt.Sprintf("/patients/%d/clinical-summary", pA.ID), fmt.Sprintf("/patients/%d/studies", pA.ID), fmt.Sprintf("/patients/%d/prescriptions", pA.ID),
		fmt.Sprintf("/patients/%d/vaccinations", pA.ID), fmt.Sprintf("/patients/%d/growth", pA.ID), fmt.Sprintf("/patients/%d/measurements", pA.ID),
		fmt.Sprintf("/encounters/patient/%d", pA.ID), fmt.Sprintf("/encounters/%d", eA.ID), fmt.Sprintf("/calculators/patients/%d/history", pA.ID), fmt.Sprintf("/calculators/patients/%d/autofill", pA.ID),
		fmt.Sprintf("/patients/%d/prescriptions/%d/print", pA.ID, rx.ID), fmt.Sprintf("/patients/%d/prescriptions/%d/pdf", pB.ID, rx.ID),
	} {
		w := call("GET", path, "integrity-owner", fmt.Sprint(b.ContextID), nil)
		assertCode(w, 404, "")
		if strings.Contains(w.Body.String(), "Preserve") {
			t.Fatal("document disclosure")
		}
	}
	assertCode(call("DELETE", fmt.Sprintf("/patients/%d", pA.ID), "integrity-owner", fmt.Sprint(b.ContextID), map[string]any{"reason": "Fixture"}), 404, "PATIENT_NOT_FOUND")
	assertCode(call("PUT", fmt.Sprintf("/patients/%d", pA.ID), "integrity-owner", fmt.Sprint(a.ContextID), map[string]any{"expectedVersion": 0, "specialtyContextId": b.ContextID, "values": integrityPatientRequest()["values"]}), 400, "INVALID_REQUEST")
	list := call("GET", "/patients?search=12345678", "integrity-owner", fmt.Sprint(a.ContextID), nil)
	assertCode(list, 200, "")
	var directory struct {
		Content []PatientList `json:"content"`
	}
	if err = json.Unmarshal(list.Body.Bytes(), &directory); err != nil || len(directory.Content) != 1 || directory.Content[0].ID != pA.ID || directory.Content[0].ContextID != a.ContextID {
		t.Fatal("directory mixed contexts", err, list.Body)
	}
	w := call("GET", "/encounters/dashboard", "integrity-owner", fmt.Sprint(b.ContextID), nil)
	assertCode(w, 200, "")
	var dashboard encounterDashboard
	if err = json.Unmarshal(w.Body.Bytes(), &dashboard); err != nil || dashboard.Patients != 1 || len(dashboard.Recent) != 1 || dashboard.Recent[0].Encounter.PatientID != pB.ID {
		t.Fatal("dashboard mixed contexts", err, w.Body)
	}
	if _, err = s.GetPatient(ctxA, "integrity-owner", pA.ID); err != nil {
		t.Fatal("denied writes changed patient", err)
	}
	if _, err = s.CreatePatient(ctxB, api.Identity{UserID: "integrity-other", Plan: "BASIC"}, integrityPatientRequest()); err == nil {
		t.Fatal("internal foreign context creation allowed")
	}

	patientPath := fmt.Sprintf("/patients/%d", pA.ID)
	encounterPath := fmt.Sprintf("/encounters/%d", eA.ID)
	rxPath := fmt.Sprintf("%s/prescriptions/%d", patientPath, rx.ID)
	historyPath := patientPath + "/revisions"
	scope := fmt.Sprint(a.ContextID)
	owner := "integrity-owner"
	// A persisted pre-SP-04B version starts with an empty history. No authors
	// or revisions are generated for earlier version numbers.
	if _, err := pool.Exec(ctx, "UPDATE patients SET version=4 WHERE id=$1", pA.ID); err != nil {
		t.Fatal(err)
	}
	readHistory := func() []revisionItem {
		t.Helper()
		response := call("GET", historyPath, owner, scope, nil)
		assertCode(response, 200, "")
		var page revisionPage[revisionItem]
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page.Items
	}
	if len(readHistory()) != 0 {
		t.Fatal("invented history for a pre-audit version")
	}
	for index, name := range []string{"Primera corrección", "Segunda corrección"} {
		values := integrityPatientRequest()["values"].(map[string]any)
		values["sys_firstName"] = name
		assertCode(call("PUT", patientPath, owner, scope, map[string]any{
			"expectedVersion": 4 + index, "values": values, "reason": "Identificación verificada",
		}), 200, "")
	}
	revisions := readHistory()
	if len(revisions) != 2 || revisions[0].PreviousVersion != 5 || revisions[0].NewVersion != 6 ||
		revisions[1].PreviousVersion != 4 || revisions[0].Actor != owner ||
		!strings.Contains(string(revisions[0].Previous), "Primera corrección") ||
		!strings.Contains(string(revisions[1].Previous), "Synthetic Integrity") {
		t.Fatal("successive revisions did not preserve the ordered previous values")
	}

	assertCode(call("PUT", encounterPath, owner, scope, map[string]any{"expectedVersion": 0,
		"consultationType": "FIRST_VISIT", "values": map[string]any{"chiefComplaint": "Motivo corregido", "weightKg": 20, "heightCm": 100}}), 200, "")
	assertCode(call("GET", historyPath, owner, fmt.Sprint(b.ContextID), nil), 404, "PATIENT_NOT_FOUND")
	other, err := s.workspaces.Setup(ctx, "integrity-other")
	if err != nil {
		t.Fatal(err)
	}
	assertCode(call("GET", historyPath, "integrity-other", fmt.Sprint(other.ContextID), nil), 404, "PATIENT_NOT_FOUND")
	staff := httptest.NewRequest("GET", "/api/doctorchart"+historyPath+"?specialtyContextId="+scope, nil)
	staff.Header.Set("X-Gateway-Secret", "context-fixture")
	staff.AddCookie(&http.Cookie{Name: "doctorchart_staff", Value: "synthetic"})
	staffResponse := httptest.NewRecorder()
	handler.ServeHTTP(staffResponse, staff)
	assertCode(staffResponse, 401, "")

	assertCode(call("GET", rxPath+"/pdf", owner, scope, nil), 200, "")
	assertCode(call("GET", encounterPath, owner, scope, nil), 200, "")
	assertCode(call("DELETE", rxPath, owner, scope, map[string]any{"reason": "Orden reemplazada"}), 204, "")
	assertCode(call("GET", rxPath+"/pdf", owner, scope, nil), 404, "PRESCRIPTION_NOT_FOUND")
	assertCode(call("DELETE", encounterPath, owner, scope, map[string]any{"reason": "Consulta duplicada", "expectedVersion": 1}), 204, "")
	assertCode(call("GET", encounterPath, owner, scope, nil), 404, "ENCOUNTER_NOT_FOUND")
	assertCode(call("GET", fmt.Sprintf("%s/encounters/%d/print", patientPath, eA.ID), owner, scope, nil), 404, "ENCOUNTER_NOT_FOUND")
	summary := call("GET", patientPath+"/clinical-summary", owner, scope, nil)
	assertCode(summary, 200, "")
	if strings.Contains(summary.Body.String(), "Motivo corregido") || strings.Contains(summary.Body.String(), "Synthetic study") {
		t.Fatal("annulled content appeared in current summary")
	}

	var unchanged bool
	verifySnapshot := func() {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT previous_content->'form_definition_snapshot'=$2::jsonb
			FROM clinical_revisions WHERE entity='ENCOUNTER' AND entity_id=$1 AND operation='CORRECT'`,
			eA.ID, eA.Snapshot).Scan(&unchanged); err != nil || !unchanged {
			t.Fatal("encounter revision lost its original form", err)
		}
	}
	verifySnapshot()
	var wizardVersion, specialtyVersion int64
	if err := pool.QueryRow(ctx, "SELECT version FROM wizard_defaults WHERE specialty_id=$1", a.Specialty.ID).Scan(&wizardVersion); err != nil {
		t.Fatal(err)
	}
	if err := testfixture.ApproveDraft(ctx, pool, a.Specialty.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.workspaces.SaveWizard(ctx, a.Specialty.ID, map[string]any{"expectedVersion": json.Number(fmt.Sprint(wizardVersion))}, true); err != nil {
		t.Fatal(err)
	}
	verifySnapshot()
	if err := pool.QueryRow(ctx, "SELECT version FROM specialties WHERE id=$1", a.Specialty.ID).Scan(&specialtyVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := s.workspaces.UpdateSpecialty(ctx, a.Specialty.ID, map[string]any{"expectedVersion": json.Number(fmt.Sprint(specialtyVersion)), "active": false}); err != nil {
		t.Fatal(err)
	}
	if len(readHistory()) != 5 {
		t.Fatal("history changed after republishing or deactivating a specialty")
	}

	assertCode(call("DELETE", patientPath, owner, scope, map[string]any{"reason": "Ficha duplicada", "expectedVersion": 5}), 409, "VERSION_CONFLICT")
	assertCode(call("GET", patientPath, owner, scope, nil), 200, "")
	if len(readHistory()) != 5 {
		t.Fatal("partial history survived failed annulment")
	}
	assertCode(call("DELETE", patientPath, owner, scope, map[string]any{"reason": "Ficha duplicada", "expectedVersion": 6}), 204, "")
	assertCode(call("GET", patientPath, owner, scope, nil), 404, "PATIENT_NOT_FOUND")
	assertCode(call("GET", patientPath+"/clinical-summary", owner, scope, nil), 404, "PATIENT_NOT_FOUND")
	archive := call("GET", "/patients/archived", owner, scope, nil)
	assertCode(archive, 200, "")
	if !strings.Contains(archive.Body.String(), "Segunda corrección") || len(readHistory()) != 7 {
		t.Fatal("annulled patient or dependent encounter missing in history")
	}
	var pdfRecorded, encounterRecorded bool
	if err := pool.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM clinical_access_events WHERE operation='PRESCRIPTION_PDF'
			AND entity_id=$1 AND patient_id=$2 AND record_version=0 AND outcome='ARTIFACT_READY'),
		EXISTS(SELECT 1 FROM clinical_access_events WHERE operation='ENCOUNTER_READ'
			AND entity_id=$3 AND patient_id=$2 AND record_version=1 AND outcome='AUTHORIZED')`,
		rx.ID, pA.ID, eA.ID).Scan(&pdfRecorded, &encounterRecorded); err != nil || !pdfRecorded || !encounterRecorded {
		t.Fatal("missing document or encounter audit after annulment", err)
	}
	auditResponse := call("GET", "/workspace/clinical-access", owner, fmt.Sprint(b.ContextID), nil)
	assertCode(auditResponse, 200, "")
	var accessPage struct{ Items []accessEvent }
	if err := json.Unmarshal(auditResponse.Body.Bytes(), &accessPage); err != nil {
		t.Fatal(err)
	}
	for _, row := range accessPage.Items {
		if row.Patient != nil && *row.Patient == pA.ID && row.Outcome == "AUTHORIZED" {
			t.Fatal("another context returned an authorized access to the first context's patient")
		}
	}
}
