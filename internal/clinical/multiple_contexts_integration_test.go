package clinical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/api"
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
	assertCode(call("DELETE", fmt.Sprintf("/patients/%d", pA.ID), "integrity-owner", fmt.Sprint(b.ContextID), nil), 404, "PATIENT_NOT_FOUND")
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
}
