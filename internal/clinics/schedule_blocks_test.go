package clinics

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func agendaDTO(t *testing.T, f *fixture, c Clinic) clinicSchedule {
	t.Helper()
	w := f.request("GET", "/clinics/"+c.ID+"/schedule", owner, "")
	assertCode(t, w, 200, "")
	var a clinicSchedule
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	return a
}
func putBlocks(t *testing.T, f *fixture, c Clinic, a clinicSchedule, status int, code string) {
	t.Helper()
	body, _ := json.Marshal(struct {
		Expected string `json:"expectedVersion"`
		ScheduleConfig
	}{a.Version, a.ScheduleConfig})
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "slotMinutes")
	body, _ = json.Marshal(m)
	assertCode(t, f.request("PUT", "/clinics/"+c.ID+"/schedule", owner, string(body)), status, code)
}
func contextID(t *testing.T, f *fixture, user string) string {
	t.Helper()
	var id string
	if err := f.pool.QueryRow(t.Context(), "SELECT c.id::text FROM doctor_specialty_contexts c JOIN doctor_workspaces w ON w.id=c.workspace_id WHERE w.user_id=$1", user).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestScheduleBlocksDurationsIdentityAndThreeClinics(t *testing.T) {
	f := scheduleFixture(t)
	first, second, third := scheduleClinicFixture(t, f), scheduleClinicFixture(t, f), scheduleClinicFixture(t, f)
	scope := contextID(t, f, owner)
	for _, c := range []Clinic{first, second, third} {
		assertCode(t, f.request("PATCH", "/clinics/"+c.ID, owner, `{"expectedVersion":"0","timezone":"UTC"}`), 200, "")
	}
	a := clinicSchedule{Version: "0", ScheduleConfig: ScheduleConfig{MinNoticeMinutes: 0, MaxAdvanceDays: 30, Weekly: []ScheduleDay{{Weekday: 1, Ranges: []ScheduleRange{{Start: "09:00", End: "10:00", SlotMinutes: 20, ContextID: scope}, {Start: "10:00", End: "11:10", SlotMinutes: 30, ContextID: scope}}}}, Exceptions: []ScheduleException{}}}
	putBlocks(t, f, first, a, 200, "")
	saved := agendaDTO(t, f, first)
	if saved.Weekly[0].Ranges[0].ID == "" || saved.Weekly[0].Ranges[0].ID == saved.Weekly[0].Ranges[1].ID {
		t.Fatal("missing stable block IDs")
	}
	date, _ := civilDate("2026-11-16")
	slots, err := saved.slots(t.Context(), date, time.UTC, date.Add(-time.Hour))
	if err != nil || len(slots) != 5 {
		t.Fatal("mixed duration/incomplete slots", slots, err)
	}
	for i, slot := range slots {
		want := 20
		if i >= 3 {
			want = 30
		}
		if slot.End.Sub(slot.Start) != time.Duration(want)*time.Minute {
			t.Fatal("wrong duration")
		}
	}
	before := saved.Weekly[0].Ranges[0].ID
	saved.Weekly[0].Ranges[0].SlotMinutes = 15
	putBlocks(t, f, first, saved, 200, "")
	saved = agendaDTO(t, f, first)
	if saved.Weekly[0].Ranges[0].ID != before {
		t.Fatal("edit replaced identity")
	}
	a.Weekly[0].Ranges = []ScheduleRange{{Start: "11:10", End: "12:00", SlotMinutes: 10, ContextID: scope}}
	putBlocks(t, f, second, a, 200, "")
	a.Weekly[0].Ranges[0].Start = "12:00"
	a.Weekly[0].Ranges[0].End = "13:00"
	putBlocks(t, f, third, a, 200, "")
	conflict := agendaDTO(t, f, third)
	conflict.Weekly[0].Ranges[0].Start = "10:30"
	putBlocks(t, f, third, conflict, 409, "SCHEDULE_CONFLICT")
	bad := agendaDTO(t, f, first)
	bad.Weekly[0].Ranges[0].ContextID = contextID(t, f, other)
	putBlocks(t, f, first, bad, 404, "SPECIALTY_CONTEXT_NOT_FOUND")
	bad = agendaDTO(t, f, first)
	bad.Weekly[0].Ranges[0].ID = agendaDTO(t, f, second).Weekly[0].Ranges[0].ID
	putBlocks(t, f, first, bad, 404, "SCHEDULE_BLOCK_NOT_FOUND")
	bad = agendaDTO(t, f, first)
	bad.Weekly[0].Ranges[0].ID = ""
	bad.Weekly[0].Ranges[0].ContextID = ""
	putBlocks(t, f, first, bad, 404, "SPECIALTY_CONTEXT_NOT_FOUND")
	bad = agendaDTO(t, f, first)
	bad.Weekly[0].Ranges[0].End = "10:01"
	putBlocks(t, f, first, bad, 400, "INVALID_REQUEST")
	saved = agendaDTO(t, f, first)
	saved.Exceptions = []ScheduleException{{From: "2026-11-16", Through: "2026-11-17", Label: "Closed", Ranges: []ScheduleRange{}}, {From: "2026-11-18", Through: "2026-11-18", Label: "Special", Ranges: []ScheduleRange{{Start: "14:00", End: "15:00", SlotMinutes: 25, ContextID: scope}}}}
	putBlocks(t, f, first, saved, 200, "")
	saved = agendaDTO(t, f, first)
	slots, err = saved.slots(t.Context(), date, time.UTC, date.Add(-time.Hour))
	if err != nil || len(slots) != 0 {
		t.Fatal("closure did not replace weekly")
	}
	special, _ := civilDate("2026-11-18")
	slots, err = saved.slots(t.Context(), special, time.UTC, special.Add(-time.Hour))
	if err != nil || len(slots) != 2 {
		t.Fatal("special exception slots", err)
	}
	oldException := saved.Exceptions[1].ID
	oldBlock := saved.Exceptions[1].Ranges[0].ID
	saved.Exceptions[1].Label = "Updated"
	putBlocks(t, f, first, saved, 200, "")
	saved = agendaDTO(t, f, first)
	if saved.Exceptions[1].ID != oldException || saved.Exceptions[1].Ranges[0].ID != oldBlock {
		t.Fatal("exception edit replaced identity")
	}
}
func TestScheduleBlocksFutureBookingCannotMoveToReplacement(t *testing.T) {
	f, c, start := bookingFixture(t)
	a := agendaDTO(t, f, c)
	block := a.Weekly[0].Ranges[0]
	block.SlotMinutes = 20
	a.Weekly[0].Ranges[0] = block
	putBlocks(t, f, c, a, 200, "")
	w := bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start))
	assertCode(t, w, 201, "")
	b := booked(t, w)
	if b.End.Sub(b.Start) != 20*time.Minute {
		t.Fatal("booking used clinic duration")
	}
	saved := agendaDTO(t, f, c)
	for _, mutation := range []func(*clinicSchedule){
		func(a *clinicSchedule) { a.Weekly[0].Ranges[0].SlotMinutes = 30 },
		func(a *clinicSchedule) { a.Weekly[0].Ranges[0].ID = "" },
		func(a *clinicSchedule) { a.Weekly[0].Ranges = nil },
		func(a *clinicSchedule) {
			a.Exceptions = []ScheduleException{{From: start.Format("2006-01-02"), Through: start.Format("2006-01-02"), Ranges: []ScheduleRange{{Start: "09:00", End: "12:00", SlotMinutes: 20, ContextID: block.ContextID}}}}
		},
	} {
		raw, _ := json.Marshal(saved)
		var changed clinicSchedule
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		mutation(&changed)
		if changed.Weekly[0].Ranges == nil {
			changed.Weekly[0].Ranges = []ScheduleRange{}
		}
		putBlocks(t, f, c, changed, 409, "BOOKING_SCHEDULE_CONFLICT")
	}
	saved.MinNoticeMinutes = 120
	putBlocks(t, f, c, saved, 200, "")
	if agendaDTO(t, f, c).Weekly[0].Ranges[0].ID != block.ID {
		t.Fatal("notice edit changed booked block")
	}
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings/"+b.ID+"/reschedule", owner, newBookingKey(), fmt.Sprintf(`{"expectedVersion":"1","clinicId":%q,"start":%q}`, c.ID, start.Add(20*time.Minute).Format(time.RFC3339))), 200, "")
	// M4 now prevents manufacturing an inconsistent slot even with direct SQL.
	if _, err := f.pool.Exec(t.Context(), "UPDATE bookings SET start_at=start_at+interval '5 minutes',end_at=end_at+interval '5 minutes',version=version+1,updated_at=clock_timestamp() WHERE id=$1", b.ID); err == nil {
		t.Fatal("SQL accepted a booking inconsistent with its provenance")
	}
	saved = agendaDTO(t, f, c)
	saved.Weekly[0].Ranges[0].Start = "09:05"
	putBlocks(t, f, c, saved, 409, "BOOKING_SCHEDULE_CONFLICT")
}

func TestScheduleBlocksConcurrentEditAndReschedule(t *testing.T) {
	f, c, start := bookingFixture(t)
	w := bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start))
	assertCode(t, w, 201, "")
	b := booked(t, w)
	a := agendaDTO(t, f, c)
	// Keep the current booking valid, but remove the proposed destination.
	a.Weekly[0].Ranges[0].End = "10:00"
	body, err := json.Marshal(struct {
		ExpectedVersion  string              `json:"expectedVersion"`
		MinNoticeMinutes int                 `json:"minNoticeMinutes"`
		MaxAdvanceDays   int                 `json:"maxAdvanceDays"`
		Weekly           []ScheduleDay       `json:"weekly"`
		Exceptions       []ScheduleException `json:"exceptions"`
	}{a.Version, a.MinNoticeMinutes, a.MaxAdvanceDays, a.Weekly, a.Exceptions})
	if err != nil {
		t.Fatal(err)
	}
	target := start.Add(time.Hour)
	gate := make(chan struct{})
	done := make(chan *httptest.ResponseRecorder, 2)
	go func() {
		<-gate
		done <- f.request("PUT", "/clinics/"+c.ID+"/schedule", owner, string(body))
	}()
	go func() {
		<-gate
		done <- bookingRequestFixture(f, "POST", "/bookings/"+b.ID+"/reschedule", owner, newBookingKey(), fmt.Sprintf(`{"expectedVersion":%q,"clinicId":%q,"start":%q}`, b.Version, c.ID, target.Format(time.RFC3339)))
	}()
	close(gate)
	first, second := <-done, <-done
	if !((first.Code == 200 && second.Code == 409) || (first.Code == 409 && second.Code == 200)) {
		t.Fatalf("edit/reschedule committed incompatible states: %d %s; %d %s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	current := booked(t, f.request("GET", "/bookings/"+b.ID, owner, ""))
	final := agendaDTO(t, f, c)
	slot, found, err := slotAt(t.Context(), final.ScheduleConfig, c, current.Start)
	if err != nil || !found || !slot.End.Equal(current.End) || slot.blockID != a.Weekly[0].Ranges[0].ID {
		t.Fatal("final booking lost its valid original block", current, slot, err)
	}
}
