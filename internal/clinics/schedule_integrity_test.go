package clinics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func scheduleFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.limit.Store(-1)
	mux := http.NewServeMux()
	NewWithSchedules(f.pool, f.rights, true).Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	return f
}
func scheduleClinicFixture(t *testing.T, f *fixture) Clinic {
	t.Helper()
	reply := f.request("POST", "/clinics", owner, createBody)
	assertCode(t, reply, 201, "")
	var c Clinic
	if err := json.Unmarshal(reply.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestScheduleCommercialValidationAndStateChanges(t *testing.T) {
	f := scheduleFixture(t)
	first, second := scheduleClinicFixture(t, f), scheduleClinicFixture(t, f)
	// Use UTC so cross-zone changes have a deterministic, testable effect.
	for _, c := range []Clinic{first, second} {
		assertCode(t, f.request("PATCH", "/clinics/"+c.ID, owner, `{"expectedVersion":"0","timezone":"UTC"}`), 200, "")
	}
	path := func(c Clinic) string { return "/clinics/" + c.ID + "/schedule" }
	assertCode(t, f.request("PUT", path(first), owner, scheduleBody("0", 1, "09:00", "10:00")), 200, "")
	assertCode(t, f.request("PUT", path(second), owner, scheduleBody("0", 1, "12:00", "13:00")), 200, "")
	assertCode(t, f.request("PATCH", "/clinics/"+second.ID, owner, `{"expectedVersion":"1","timezone":"Europe/Moscow"}`), 409, "SCHEDULE_CONFLICT")
	var zone string
	if err := f.pool.QueryRow(t.Context(), "SELECT timezone FROM clinics WHERE id=$1", second.ID).Scan(&zone); err != nil || zone != "UTC" {
		t.Fatal("timezone conflict changed clinic", zone, err)
	}
	assertCode(t, f.request("PATCH", "/clinics/"+second.ID+"/status", owner, `{"expectedVersion":"1","status":"ARCHIVED"}`), 200, "")
	assertCode(t, f.request("PUT", path(first), owner, scheduleBody("1", 1, "12:00", "13:00")), 200, "")
	assertCode(t, f.request("PATCH", "/clinics/"+second.ID+"/status", owner, `{"expectedVersion":"2","status":"INACTIVE"}`), 409, "SCHEDULE_CONFLICT")
	for _, body := range []string{
		`{"expectedVersion":"2","slotMinutes":0,"minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":[],"exceptions":[]}`,
		`{"expectedVersion":"2","slotMinutes":30,"minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":null,"exceptions":[]}`,
		`{"expectedVersion":"2","slotMinutes":30,"minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":[{"weekday":1,"ranges":[{"start":"09:00","end":"12:00"},{"start":"11:00","end":"13:00"}]}],"exceptions":[]}`,
		`{"expectedVersion":"2","slotMinutes":30,"minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":[],"exceptions":[{"from":"2026-02-30","through":"2026-03-01","label":"","ranges":[]}]}`,
	} {
		assertCode(t, f.request("PUT", path(first), owner, body), 400, "INVALID_REQUEST")
	}
	// A denied or unverifiable right must not change the stored version.
	for _, tc := range []struct {
		mode, status int
		code         string
	}{{1, 503, "ENTITLEMENTS_UNAVAILABLE"}, {2, 403, "CLINIC_MANAGEMENT_DISABLED"}, {3, 503, "ENTITLEMENTS_UNAVAILABLE"}, {4, 503, "ENTITLEMENTS_UNAVAILABLE"}} {
		f.mode.Store(int32(tc.mode))
		if err := f.rights.Invalidate(t.Context()); err != nil {
			t.Fatal(err)
		}
		assertCode(t, f.request("PUT", path(first), owner, scheduleBody("2", 1, "15:00", "16:00")), tc.status, tc.code)
	}
	var version int
	if err := f.pool.QueryRow(t.Context(), "SELECT version FROM clinic_schedules WHERE clinic_id=$1", first.ID).Scan(&version); err != nil || version != 2 {
		t.Fatal("rejected mutations changed schedule", version, err)
	}
}
func TestScheduleConcurrentConflictingWrites(t *testing.T) {
	f := scheduleFixture(t)
	first, second := scheduleClinicFixture(t, f), scheduleClinicFixture(t, f)
	results := make(chan int, 2)
	for _, c := range []Clinic{first, second} {
		go func(id string) {
			results <- f.request("PUT", "/clinics/"+id+"/schedule", owner, scheduleBody("0", 1, "09:00", "12:00")).Code
		}(c.ID)
	}
	a, b := <-results, <-results
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatal("conflicting concurrent configuration admitted", a, b)
	}
	var count int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM clinic_schedules").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	var id string
	if err := f.pool.QueryRow(t.Context(), "SELECT clinic_id::text FROM clinic_schedules").Scan(&id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		go func(i int) {
			results <- f.request("PUT", "/clinics/"+id+"/schedule", owner, scheduleBody("1", 1, fmt.Sprintf("%02d:00", 14+i), fmt.Sprintf("%02d:00", 15+i))).Code
		}(i)
	}
	a, b = <-results, <-results
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatal("stale write admitted", a, b)
	}
}
