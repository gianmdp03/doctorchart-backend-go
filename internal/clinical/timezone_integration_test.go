package clinical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func zoneContext(t *testing.T, name string) context.Context {
	t.Helper()
	var ctx context.Context
	api.WithTimeZone(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ctx = r.Context() })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/?timeZone="+url.QueryEscape(name), nil))
	if ctx == nil {
		t.Fatal("zone rejected")
	}
	return ctx
}

func TestTimeZoneCreationImmutabilityAndDocumentReprint(t *testing.T) {
	services, pool := integrityFixture(t, 100, 100)
	s := services[0]
	madrid := zoneContext(t, "Europe/Madrid")
	argentina := zoneContext(t, "America/Argentina/Buenos_Aires")
	identity := api.Identity{UserID: "integrity-owner", Plan: "BASIC", Roles: []string{"ROLE_COMPANY"}}
	patient, err := s.CreatePatient(madrid, identity, integrityPatientRequest())
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	encounter, err := s.CreateEncounter(madrid, identity, integrityEncounterRequest(patient.ID, "FIRST_VISIT"))
	if err != nil {
		t.Fatal(err)
	}
	instant, err := time.Parse(time.RFC3339Nano, encounter.Date)
	if err != nil || instant.Before(before) || instant.After(time.Now().UTC()) {
		t.Fatal("creation did not use server time", encounter.Date, err)
	}
	var saved time.Time
	if err := pool.QueryRow(context.Background(), "SELECT encounter_date FROM clinical_encounters WHERE id=$1", encounter.ID).Scan(&saved); err != nil || !saved.Equal(instant) {
		t.Fatal("UTC round trip changed instant", err)
	}
	var formula map[string]any
	if err := json.Unmarshal(encounter.Context, &formula); err != nil || formula["zone"] != "Europe/Madrid" {
		t.Fatal("missing origin zone", err)
	}
	illegal := integrityEncounterRequest(patient.ID, "FOLLOW_UP")
	illegal["encounterDate"] = "2000-01-01T00:00:00Z"
	if _, err := s.CreateEncounter(madrid, identity, illegal); err == nil {
		t.Fatal("client supplied a creation timestamp")
	}
	edit := map[string]any{"expectedVersion": json.Number("0"), "encounterDate": instant.Add(-time.Hour).Format(time.RFC3339Nano), "consultationType": "FIRST_VISIT", "values": integrityEncounterRequest(patient.ID, "FIRST_VISIT")["values"]}
	_, err = s.UpdateEncounter(argentina, identity.UserID, encounter.ID, edit)
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Code != "IMMUTABLE_ENCOUNTER_DATE" {
		t.Fatal("mutable creation instant", err)
	}
	delete(edit, "encounterDate")
	updated, err := s.UpdateEncounter(argentina, identity.UserID, encounter.ID, edit)
	if err != nil || updated.Date != encounter.Date {
		t.Fatal("ordinary edit changed time", err)
	}
	body := map[string]any{"type": "STUDY_ORDERS", "diagnosisDescription": "Prueba", "generalInstructions": "", "prescriptions": []any{}, "diagnosticOrders": []any{"Laboratorio"}}
	prescription, err := s.CreatePrescription(madrid, identity.UserID, patient.ID, body)
	if err != nil {
		t.Fatal(err)
	}
	if prescription.IssuanceZone != "Europe/Madrid" {
		t.Fatal("origin zone not saved")
	}
	first, err := s.printable(madrid, identity.UserID, patient.ID, prescription.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.printable(argentina, identity.UserID, patient.ID, prescription.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.IssuanceZone != second.IssuanceZone || first.Issued != second.Issued || first.IssuanceZone != "Europe/Madrid" {
		t.Fatal("travel changed emitted document")
	}
	if _, err := s.printPDF(second, url.Values{}); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardUsesRequestCalendarDay(t *testing.T) {
	services, pool := integrityFixture(t, 100, 100)
	s := services[0]
	ctx := zoneContext(t, "Pacific/Kiritimati")
	identity := api.Identity{UserID: "integrity-owner", Plan: "BASIC", Roles: []string{"ROLE_COMPANY"}}
	patient, err := s.CreatePatient(ctx, identity, integrityPatientRequest())
	if err != nil {
		t.Fatal(err)
	}
	encounter, err := s.CreateEncounter(ctx, identity, integrityEncounterRequest(patient.ID, "FIRST_VISIT"))
	if err != nil {
		t.Fatal(err)
	}
	zone := s.viewZone(ctx)
	now := time.Now().In(zone)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	// Fixture DML only: exact boundary is included, the preceding instant excluded.
	if _, err := pool.Exec(context.Background(), "UPDATE clinical_encounters SET encounter_date=$2 WHERE id=$1", encounter.ID, start); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.EncounterRoutes(mux)
	app := api.Gateway("timezone-test", api.Dispatch(mux))
	request := httptest.NewRequest("GET", "/api/doctorchart/encounters/dashboard?timeZone=Pacific%2FKiritimati", nil)
	request.Header.Set("X-Gateway-Secret", "timezone-test")
	request.Header.Set("X-User-Id", identity.UserID)
	request.Header.Set("X-User-Role", "SUPER_ADMIN")
	call := func(want int64) {
		t.Helper()
		response := httptest.NewRecorder()
		app.ServeHTTP(response, request)
		var result encounterDashboard
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Today != want || result.Date != now.Format("2006-01-02") {
			t.Fatal("wrong day boundary", result, err)
		}
	}
	call(1)
	if _, err := pool.Exec(context.Background(), "UPDATE clinical_encounters SET encounter_date=$2 WHERE id=$1", encounter.ID, start.Add(-time.Microsecond)); err != nil {
		t.Fatal(err)
	}
	call(0)
}
