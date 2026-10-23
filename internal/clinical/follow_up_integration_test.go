package clinical

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func followUpRequest(patientID int64, kind string, date any) map[string]any {
	request := integrityEncounterRequest(patientID, kind)
	request["values"].(map[string]any)["followUpDate"] = date
	return request
}

func followUpDashboard(t *testing.T, s *Service, owner, zone string) encounterDashboard {
	t.Helper()
	mux := http.NewServeMux()
	s.EncounterRoutes(mux)
	request := httptest.NewRequest("GET", "/api/doctorchart/encounters/dashboard?timeZone="+zone, nil)
	request.Header.Set("X-Gateway-Secret", "follow-up-test")
	request.Header.Set("X-User-Id", owner)
	request.Header.Set("X-User-Role", "SUPER_ADMIN")
	response := httptest.NewRecorder()
	api.Gateway("follow-up-test", api.Dispatch(mux)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard: %d %s", response.Code, response.Body.String())
	}
	var dashboard encounterDashboard
	if err := json.Unmarshal(response.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if dashboard.Upcoming == nil {
		t.Fatal("upcomingFollowUps must be an array, including when empty")
	}
	return dashboard
}

func TestFollowUpDatePersistenceAndValidation(t *testing.T) {
	services, pool := integrityFixture(t, 100, 100)
	s := services[0]
	ctx := zoneContext(t, "America/Argentina/Buenos_Aires")
	identity := api.Identity{UserID: "integrity-owner", Plan: "BASIC"}
	patient := integrityPatient(t, s)
	created, err := s.CreateEncounter(ctx, identity, followUpRequest(patient.ID, "FIRST_VISIT", "2028-02-29"))
	if err != nil {
		t.Fatal(err)
	}
	assertStored := func(want *string, version int64) {
		t.Helper()
		var date *string
		var values []byte
		var savedVersion int64
		if err := pool.QueryRow(ctx, `SELECT follow_up_date::text,form_values,version FROM clinical_encounters WHERE id=$1`, created.ID).Scan(&date, &values, &savedVersion); err != nil {
			t.Fatal(err)
		}
		var answers map[string]any
		if err := json.Unmarshal(values, &answers); err != nil {
			t.Fatal(err)
		}
		if savedVersion != version || (date == nil) != (want == nil) || (want != nil && (*date != *want || answers["followUpDate"] != *want)) || (want == nil && answers["followUpDate"] != nil) {
			t.Fatal("date and form values did not round trip together", date, answers, savedVersion)
		}
		// The actual read contract must expose the stored date and saved form,
		// rather than merely returning the in-memory create/update result.
		mux := http.NewServeMux()
		s.EncounterRoutes(mux)
		request := httptest.NewRequest("GET", fmt.Sprintf("/api/doctorchart/encounters/%d", created.ID), nil)
		request.Header.Set("X-Gateway-Secret", "follow-up-test")
		request.Header.Set("X-User-Id", identity.UserID)
		request.Header.Set("X-User-Role", "SUPER_ADMIN")
		response := httptest.NewRecorder()
		api.Gateway("follow-up-test", api.Dispatch(mux)).ServeHTTP(response, request)
		var read Encounter
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &read) != nil || (read.FollowUpDate == nil) != (want == nil) || (want != nil && *read.FollowUpDate != *want) {
			t.Fatal("detail lost the follow-up date", response.Code, response.Body.String())
		}
	}
	date := "2028-02-29"
	assertStored(&date, 0)
	edit := map[string]any{"expectedVersion": json.Number("0"), "consultationType": "FIRST_VISIT", "values": map[string]any{"chiefComplaint": "Edited", "followUpDate": "2028-03-01"}}
	if _, err := s.UpdateEncounter(zoneContext(t, "Pacific/Kiritimati"), identity.UserID, created.ID, edit); err != nil {
		t.Fatal(err)
	}
	date = "2028-03-01"
	assertStored(&date, 1)
	for _, invalid := range []any{"2027-02-29", "01/03/2028", "2028-13-01", "2028-03-01T00:00:00Z", "0000-01-01", "", 123} {
		edit["expectedVersion"] = json.Number("1")
		edit["values"].(map[string]any)["followUpDate"] = invalid
		_, err := s.UpdateEncounter(ctx, identity.UserID, created.ID, edit)
		var failure *api.Error
		if !errors.As(err, &failure) || failure.Status != 400 {
			t.Fatalf("invalid date must produce a validation error: %v, got %v", invalid, err)
		}
		assertStored(&date, 1)
	}
	edit["values"].(map[string]any)["followUpDate"] = nil
	if _, err := s.UpdateEncounter(ctx, identity.UserID, created.ID, edit); err != nil {
		t.Fatal(err)
	}
	assertStored(nil, 2)
}

func TestFollowUpDashboardLatestDateWindowOwnershipAndRefresh(t *testing.T) {
	services, pool := integrityFixture(t, 100, 100)
	s := services[0]
	zone := "America/Argentina/Buenos_Aires"
	ctx := zoneContext(t, zone)
	identity := api.Identity{UserID: "integrity-owner", Plan: "BASIC"}
	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(location)
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }
	serial := 0
	create := func(owner, name string, followUp any) Encounter {
		t.Helper()
		serial++
		request := integrityPatientRequest()
		values := request["values"].(map[string]any)
		values["sys_firstName"], values["sys_lastName"], values["sys_dni"] = name, "Prueba", fmt.Sprintf("%08d", 20000000+serial)
		patient, err := s.CreatePatient(ctx, api.Identity{UserID: owner, Plan: "BASIC"}, request)
		if err != nil {
			t.Fatal(err)
		}
		encounter, err := s.CreateEncounter(ctx, api.Identity{UserID: owner, Plan: "BASIC"}, followUpRequest(patient.ID, "FIRST_VISIT", followUp))
		if err != nil {
			t.Fatal(err)
		}
		return encounter
	}
	// More than the dashboard's eight recent records: reminders must use all
	// patients, not a page of history. Every patient appears at most once.
	for i := 0; i < 12; i++ {
		create(identity.UserID, fmt.Sprintf("Paciente %02d", i), day(i%3))
	}
	canceled := create(identity.UserID, "Cancelado", day(1))
	later := create(identity.UserID, "Reprogramado", day(1))
	changed := create(identity.UserID, "Actualizado", day(1))
	for _, item := range []struct {
		encounter Encounter
		date      any
	}{{canceled, nil}, {later, day(7)}, {changed, day(2)}} {
		if _, err := s.CreateEncounter(ctx, identity, followUpRequest(item.encounter.PatientID, "FOLLOW_UP", item.date)); err != nil {
			t.Fatal(err)
		}
	}
	create(identity.UserID, "Vencido", day(-1))
	create(identity.UserID, "Fuera del rango", day(3))
	create("integrity-other", "Paciente privado", day(1))
	if _, err := pool.Exec(ctx, "UPDATE patients SET full_name='Nombre Actual Apellido' WHERE id=$1", changed.PatientID); err != nil {
		t.Fatal(err)
	}
	dashboard := followUpDashboard(t, s, identity.UserID, zone)
	if dashboard.Date != day(0) || len(dashboard.Upcoming) != 13 {
		t.Fatalf("wrong date window or truncated reminders: %s, %d", dashboard.Date, len(dashboard.Upcoming))
	}
	seen := map[int64]bool{}
	for i, item := range dashboard.Upcoming {
		if seen[item.PatientID] || item.PatientID == canceled.PatientID || item.PatientID == later.PatientID || item.Date < day(0) || item.Date > day(2) {
			t.Fatal("duplicate, stale, or out-of-window reminder", item)
		}
		if i > 0 && dashboard.Upcoming[i-1].Date > item.Date {
			t.Fatal("reminders not chronologically ordered")
		}
		if item.PatientID == changed.PatientID && (item.Date != day(2) || item.PatientName != "Nombre Actual Apellido") {
			t.Fatal("reminder does not use latest encounter and current full name", item)
		}
		seen[item.PatientID] = true
	}
	if !seen[changed.PatientID] {
		t.Fatal("updated follow-up missing")
	}
	private := followUpDashboard(t, s, "integrity-other", zone)
	if len(private.Upcoming) != 1 || private.Upcoming[0].PatientName != "Paciente privado Prueba" {
		t.Fatal("dashboard leaked another doctor's patients", private.Upcoming)
	}
	// A saved edit is reflected on the next aggregate read; no stale cache.
	edit := map[string]any{"expectedVersion": json.Number("0"), "consultationType": "FIRST_VISIT", "values": map[string]any{"chiefComplaint": "Control", "followUpDate": nil}}
	if _, err := s.UpdateEncounter(ctx, identity.UserID, later.ID, edit); err != nil {
		t.Fatal(err)
	}
	if got := followUpDashboard(t, s, identity.UserID, zone); len(got.Upcoming) != 13 {
		t.Fatal("editing an older encounter revived its reminder")
	}
	for _, item := range dashboard.Upcoming {
		if item.PatientID != changed.PatientID {
			continue
		}
		edit["consultationType"] = "FOLLOW_UP"
		if _, err := s.UpdateEncounter(ctx, identity.UserID, item.EncounterID, edit); err != nil {
			t.Fatal(err)
		}
		if got := followUpDashboard(t, s, identity.UserID, zone); len(got.Upcoming) != 12 {
			t.Fatal("clearing latest date did not remove the reminder")
		}
		edit["expectedVersion"] = json.Number("1")
		edit["values"].(map[string]any)["followUpDate"] = day(1)
		if _, err := s.UpdateEncounter(ctx, identity.UserID, item.EncounterID, edit); err != nil {
			t.Fatal(err)
		}
		if got := followUpDashboard(t, s, identity.UserID, zone); len(got.Upcoming) != 13 {
			t.Fatal("rescheduling latest date did not restore the reminder")
		}
	}
	for _, otherZone := range []string{"Pacific/Kiritimati", "Pacific/Pago_Pago"} {
		location, err := time.LoadLocation(otherZone)
		if err != nil {
			t.Fatal(err)
		}
		local := time.Now().In(location)
		got := followUpDashboard(t, s, identity.UserID, otherZone)
		if got.Date != local.Format("2006-01-02") {
			t.Fatal("dashboard ignored the doctor's calendar day", got.Date)
		}
		for _, item := range got.Upcoming {
			if item.Date < got.Date || item.Date > local.AddDate(0, 0, 2).Format("2006-01-02") {
				t.Fatal("date-only reminder shifted across time zones", item)
			}
		}
	}
	// Empty accounts still receive [], so the UI can distinguish no controls
	// from an unavailable response.
	if got := followUpDashboard(t, s, "empty-owner", zone); len(got.Upcoming) != 0 {
		t.Fatal("empty owner has reminders")
	}
}
