package clinics

import (
	"encoding/json"
	"fmt"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"net/http"
	"testing"
	"time"
)

func scheduleBody(version string, day int, start, end string) string {
	return fmt.Sprintf(`{"expectedVersion":%q,"slotMinutes":30,"minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":[{"weekday":%d,"ranges":[{"start":%q,"end":%q}]}],"exceptions":[]}`, version, day, start, end)
}
func TestScheduleRoutesOwnershipConflictsAndVersion(t *testing.T) {
	f := newFixture(t)
	mux := http.NewServeMux()
	NewWithSchedules(f.pool, f.rights, true).Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	f.limit.Store(-1)
	makeClinic := func() Clinic {
		t.Helper()
		reply := f.request("POST", "/clinics", owner, createBody)
		assertCode(t, reply, 201, "")
		var c Clinic
		if err := json.Unmarshal(reply.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	first, second := makeClinic(), makeClinic()
	path := "/clinics/" + first.ID + "/schedule"
	assertCode(t, f.request("GET", path, other, ""), 404, "CLINIC_NOT_FOUND")
	assertCode(t, f.request("PUT", path, other, scheduleBody("0", 1, "09:00", "12:00")), 404, "CLINIC_NOT_FOUND")
	initial := f.request("GET", path, owner, "")
	assertCode(t, initial, 200, "")
	var initialDTO map[string]any
	if err := json.Unmarshal(initial.Body.Bytes(), &initialDTO); err != nil || initialDTO["configured"] != false || initialDTO["slotMinutes"] != nil {
		t.Fatal(initial.Body.String(), err)
	}
	start := time.Now()
	assertCode(t, f.request("PUT", path, owner, scheduleBody("0", 1, "09:00", "12:00")), 200, "")
	assertCode(t, f.request("PUT", path, owner, scheduleBody("0", 1, "10:00", "12:00")), 409, "VERSION_CONFLICT")
	assertCode(t, f.request("PUT", "/clinics/"+second.ID+"/schedule", owner, scheduleBody("0", 1, "11:00", "13:00")), 409, "SCHEDULE_CONFLICT")
	var count int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM clinic_schedules").Scan(&count); err != nil || count != 1 {
		t.Fatal("conflict persisted", count, err)
	}
	assertCode(t, f.request("PUT", "/clinics/"+second.ID+"/schedule", owner, scheduleBody("0", 1, "12:00", "13:00")), 200, "")
	t.Logf("Cross-clinic validation elapsed %s", time.Since(start))
	assertCode(t, f.request("GET", "/clinics/"+first.ID+"/availability?from=2026-11-02&days=7", owner, ""), 200, "")
	assertCode(t, f.request("GET", "/clinics/"+first.ID+"/availability?from=bad&days=7", owner, ""), 400, "INVALID_REQUEST")
	assertCode(t, f.request("GET", "/clinics/"+first.ID+"/availability?from=2026-11-02&days=8", owner, ""), 400, "INVALID_REQUEST")
	assertCode(t, f.request("GET", "/clinics/"+first.ID+"/availability?from=2026-11-02&days=1&timeZone=UTC", owner, ""), 400, "INVALID_REQUEST")
}
