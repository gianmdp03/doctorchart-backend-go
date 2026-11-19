package clinics

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func virtualClinic(t *testing.T, f *fixture) Clinic {
	t.Helper()
	w := f.request("POST", "/clinics", owner, `{"name":"Teleconsultas privadas","timezone":"UTC","modality":"TELECONSULTATION"}`)
	assertCode(t, w, 201, "")
	var clinic Clinic
	if err := json.Unmarshal(w.Body.Bytes(), &clinic); err != nil || clinic.Modality != teleconsultation || clinic.Address != "" {
		t.Fatal(w.Body.String(), err)
	}
	toggleTeleconsultation(t, f, contextID(t, f, owner), true)
	return clinic
}

func toggleTeleconsultation(t *testing.T, f *fixture, context string, enabled bool) teleconsultationSetting {
	t.Helper()
	w := f.request("PUT", "/teleconsultations/"+context, owner, fmt.Sprintf(`{"enabled":%t}`, enabled))
	assertCode(t, w, 200, "")
	var setting teleconsultationSetting
	if err := json.Unmarshal(w.Body.Bytes(), &setting); err != nil || setting.Enabled != enabled {
		t.Fatal(w.Body.String(), err)
	}
	return setting
}

func TestFixedTeleconsultationPerSpecialty(t *testing.T) {
	f, _, start := bookingFixture(t)
	first := contextID(t, f, owner)
	foreign := contextID(t, f, other)
	list := f.request("GET", "/teleconsultations", owner, "")
	assertCode(t, list, 200, "")
	if strings.Contains(list.Body.String(), `"clinicId"`) || !strings.Contains(list.Body.String(), `"enabled":false`) {
		t.Fatal(list.Body.String())
	}
	assertCode(t, f.request("PUT", "/teleconsultations/"+foreign, owner, `{"enabled":true}`), 404, "SPECIALTY_CONTEXT_NOT_FOUND")
	setting := toggleTeleconsultation(t, f, first, true)
	if setting.ClinicID == "" || toggleTeleconsultation(t, f, first, true).ClinicID != setting.ClinicID {
		t.Fatal("activation must create one shared virtual clinic")
	}
	var second string
	if err := f.pool.QueryRow(t.Context(), `INSERT INTO doctor_specialty_contexts(workspace_id,created_at,current_step_key,onboarding_completed,definition,updated_at,version,specialty_id)
		SELECT workspace_id,now(),'COMPLETE',true,'{}',now(),0,(SELECT min(id) FROM specialties WHERE id<>c.specialty_id)
		FROM doctor_specialty_contexts c WHERE id=$1 RETURNING id::text`, first).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if toggleTeleconsultation(t, f, second, true).ClinicID != setting.ClinicID {
		t.Fatal("specialties must share the same virtual clinic")
	}
	weekday := (int(start.Weekday())+6)%7 + 1
	path := "/clinics/" + setting.ClinicID + "/schedule"
	schedule := func(version, secondStart string) string {
		return fmt.Sprintf(`{"expectedVersion":%q,"minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":[{"weekday":%d,"ranges":[{"start":"15:00","end":"17:00","specialtyContextId":%q,"slotMinutes":30},{"start":%q,"end":"19:00","specialtyContextId":%q,"slotMinutes":30}]}],"exceptions":[]}`, version, weekday, first, secondStart, second)
	}
	assertCode(t, f.request("POST", path+"/validate", owner, schedule("0", "16:30")), 400, "INVALID_REQUEST")
	assertCode(t, f.request("PUT", path, owner, schedule("0", "16:30")), 400, "INVALID_REQUEST")
	assertCode(t, f.request("PUT", path, owner, schedule("0", "17:00")), 200, "")
	toggleTeleconsultation(t, f, first, false)
	availability := "/clinics/" + setting.ClinicID + "/availability?days=1&from=" + start.Format(civilLayout)
	w := f.request("GET", availability, owner, "")
	assertCode(t, w, 200, "")
	if strings.Contains(w.Body.String(), `"specialtyContextId":"`+first+`"`) || !strings.Contains(w.Body.String(), `"specialtyContextId":"`+second+`"`) {
		t.Fatal(w.Body.String())
	}
	at := time.Date(start.Year(), start.Month(), start.Day(), 15, 0, 0, 0, time.UTC)
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(),
		createBookingBody(setting.ClinicID, at)), 409, "BOOKING_SLOT_UNAVAILABLE")
	toggleTeleconsultation(t, f, first, true)
	w = bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(setting.ClinicID, at))
	assertCode(t, w, 201, "")
	booking := booked(t, w)
	toggleTeleconsultation(t, f, first, false)
	var status string
	if err := f.pool.QueryRow(t.Context(), "SELECT status FROM bookings WHERE id=$1", booking.ID).Scan(&status); err != nil || status != "CONFIRMED" {
		t.Fatalf("existing booking status: %q, error: %v", status, err)
	}
}

func TestTeleconsultationUnifiedScheduleAndBookingHistory(t *testing.T) {
	f, physical, start := bookingFixture(t)
	f.limit.Store(1)
	if err := f.rights.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	virtual := virtualClinic(t, f)
	caps := f.request("GET", "/clinics/capabilities", owner, "")
	if !strings.Contains(caps.Body.String(), `"counted":1`) {
		t.Fatal(caps.Body.String())
	}
	assertCode(t, f.request("POST", "/clinics", owner, `{"name":"Segunda","timezone":"UTC","modality":"TELECONSULTATION"}`), 409, "TELECONSULTATION_EXISTS")
	assertCode(t, f.request("POST", "/clinics", owner, `{"name":"Con dirección","timezone":"UTC","modality":"TELECONSULTATION","address":"Calle"}`), 400, "INVALID_REQUEST")

	var general, second string
	if err := f.pool.QueryRow(t.Context(), "SELECT id::text FROM doctor_specialty_contexts WHERE workspace_id=(SELECT id FROM doctor_workspaces WHERE user_id=$1)", owner).Scan(&general); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(t.Context(), `INSERT INTO doctor_specialty_contexts(workspace_id,created_at,current_step_key,onboarding_completed,definition,updated_at,version,specialty_id)
		SELECT workspace_id,now(),'COMPLETE',true,'{}',now(),0,(SELECT min(id) FROM specialties WHERE id<>c.specialty_id)
		FROM doctor_specialty_contexts c WHERE id=$1 RETURNING id::text`, general).Scan(&second); err != nil {
		t.Fatal(err)
	}
	toggleTeleconsultation(t, f, second, true)
	weekday := (int(start.Weekday())+6)%7 + 1
	path := func(c Clinic) string { return "/clinics/" + c.ID + "/schedule" }
	put := func(c Clinic, version, from, to, context string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"expectedVersion":%q,"minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":[{"weekday":%d,"ranges":[{"start":%q,"end":%q,"specialtyContextId":%q,"slotMinutes":30}]}],"exceptions":[]}`, version, weekday, from, to, context)
		return f.request("PUT", path(c), owner, body)
	}
	w := put(virtual, "0", "11:30", "15:00", second)
	assertCode(t, w, 409, "SCHEDULE_CONFLICT")
	var failure struct {
		Details struct {
			Requested    agendaWindow
			Existing     agendaWindow
			OverlapStart time.Time
			OverlapEnd   time.Time
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil || failure.Details.Requested.Modality != teleconsultation ||
		failure.Details.Existing.ClinicID != physical.ID ||
		failure.Details.OverlapEnd.Sub(failure.Details.OverlapStart) != 30*time.Minute {
		t.Fatal(w.Body.String(), err)
	}
	assertCode(t, put(virtual, "0", "12:00", "15:00", second), 200, "")
	assertCode(t, put(physical, "1", "09:00", "12:30", general), 409, "SCHEDULE_CONFLICT")
	w = f.request("GET", path(virtual)+"/occupied?from="+start.Format(civilLayout), owner, "")
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), `"modality":"IN_PERSON"`) {
		t.Fatal(w.Body.String())
	}
	assertCode(t, f.request("GET", path(virtual)+"/occupied?from="+start.Format(civilLayout), other, ""), 404, "CLINIC_NOT_FOUND")

	week := "/occupied?from=" + start.Format(civilLayout)
	w = f.request("GET", path(virtual)+week+"&specialtyContextId="+general, owner, "")
	assertCode(t, w, 200, "")
	var occupied struct{ Items []agendaWindow }
	if err := json.Unmarshal(w.Body.Bytes(), &occupied); err != nil {
		t.Fatal(err)
	}
	var foundOtherVirtual bool
	for _, window := range occupied.Items {
		if window.ClinicID == virtual.ID {
			if window.ContextID != second {
				t.Fatalf("unexpected teleconsultation context: %+v", window)
			}
			foundOtherVirtual = true
		}
	}
	if !foundOtherVirtual {
		t.Fatal("other specialty missing from the shared teleconsultation agenda")
	}
	w = f.request("GET", path(virtual)+week+"&specialtyContextId="+second, owner, "")
	assertCode(t, w, 200, "")
	if strings.Contains(w.Body.String(), `"modality":"TELECONSULTATION"`) {
		t.Fatal(w.Body.String())
	}
	assertCode(t, f.request("GET", path(virtual)+week+"&specialtyContextId="+contextID(t, f, other), owner, ""), 404, "SPECIALTY_CONTEXT_NOT_FOUND")

	w = bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(physical.ID, start))
	assertCode(t, w, 201, "")
	b := booked(t, w)
	assertCode(t, f.request("PATCH", "/clinics/"+physical.ID+"/status", owner, `{"expectedVersion":"1","status":"INACTIVE"}`), 200, "")
	assertCode(t, put(virtual, "1", "09:00", "15:00", second), 409, "SCHEDULE_CONFLICT")
	w = f.request("PATCH", "/clinics/"+physical.ID+"/status", owner, `{"expectedVersion":"2","status":"ARCHIVED"}`)
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), `"futureBookings":1`) {
		t.Fatal(w.Body.String())
	}
	assertCode(t, put(virtual, "1", "09:00", "15:00", second), 409, "BOOKING_SCHEDULE_CONFLICT")
	w = f.request("GET", path(virtual)+"/occupied?from="+start.Format(civilLayout), owner, "")
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), start.Format(time.RFC3339)) || strings.Contains(w.Body.String(), "Contacto") {
		t.Fatal(w.Body.String())
	}
	cancel := `{"expectedVersion":"1","status":"CANCELLED"}`
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), cancel), 200, "")
	assertCode(t, put(virtual, "1", "09:00", "15:00", second), 200, "")
	w = bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(virtual.ID, start))
	assertCode(t, w, 201, "")
	online := booked(t, w)
	if online.ContextID != second || online.ClinicSnapshot.Modality != teleconsultation || online.ClinicSnapshot.Address != "" {
		t.Fatal(online)
	}
	replacement := scheduleClinicFixture(t, f)
	assertCode(t, f.request("PATCH", "/clinics/"+replacement.ID, owner, `{"expectedVersion":"0","timezone":"UTC"}`), 200, "")
	assertCode(t, put(replacement, "0", "15:00", "16:00", second), 200, "")
	move := fmt.Sprintf(`{"expectedVersion":%q,"clinicId":%q,"start":%q}`, online.Version, replacement.ID, start.Add(6*time.Hour).Format(time.RFC3339))
	w = bookingRequestFixture(f, "POST", "/bookings/"+online.ID+"/reschedule", owner, newBookingKey(), move)
	assertCode(t, w, 200, "")
	moved := booked(t, w)
	if moved.ClinicSnapshot.Modality != inPerson || moved.ContextID != second {
		t.Fatal(moved)
	}
	movement := f.request("GET", "/bookings/"+online.ID+"/events", owner, "")
	assertCode(t, movement, 200, "")
	if !strings.Contains(movement.Body.String(), `"modality":"TELECONSULTATION"`) || !strings.Contains(movement.Body.String(), `"modality":"IN_PERSON"`) {
		t.Fatal(movement.Body.String())
	}
	history := f.request("GET", "/bookings/"+b.ID+"/events", owner, "")
	assertCode(t, history, 200, "")
	if !strings.Contains(history.Body.String(), `"modality":"IN_PERSON"`) || !strings.Contains(history.Body.String(), "CANCELLED") {
		t.Fatal(history.Body.String())
	}
}

func TestTeleconsultationOnlyPublicDirectoryAndPrivateStaffProjection(t *testing.T) {
	f, physical, start, publication := publicFixture(t)
	virtual := virtualClinic(t, f)
	start = start.Add(3 * time.Hour)
	weekday := (int(start.Weekday())+6)%7 + 1
	assertCode(t, f.request("PUT", "/clinics/"+virtual.ID+"/schedule", owner, scheduleBody("0", weekday, "12:00", "15:00")), 200, "")
	publication.ExpectedVersion = publication.Version
	publication.Clinics = []PublishedClinic{{virtual.ID, true, ""}}
	w := f.request("PUT", "/booking-publication", owner, jsonString(publication))
	assertCode(t, w, 200, "")
	if err := json.Unmarshal(w.Body.Bytes(), &publication); err != nil {
		t.Fatal(err)
	}
	directory := "/public/appointments/doctors?country=Argentina&includeSpecialties=1"
	for _, suffix := range []string{"", "&modality=TELECONSULTATION"} {
		w = pubRequest(f, "GET", directory+suffix, "", nil)
		assertCode(t, w, 200, "")
		if !strings.Contains(w.Body.String(), `"modality":"TELECONSULTATION"`) || strings.Contains(w.Body.String(), physical.Name) {
			t.Fatal(w.Body.String())
		}
	}
	w = pubRequest(f, "GET", directory+"&modality=IN_PERSON", "", nil)
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal(w.Body.String())
	}
	body := publicBody(t, f, publication, start)
	path := "/public/appointments/doctors/" + publication.PublicID + "/bookings"
	key := newOpaqueID()
	w = pubRequest(f, "POST", path, key, body)
	assertCode(t, w, 201, "")
	if !strings.Contains(w.Body.String(), `"modality":"TELECONSULTATION"`) || !strings.Contains(w.Body.String(), `"address":""`) {
		t.Fatal(w.Body.String())
	}
	assertCode(t, pubRequest(f, "POST", path, key, body), 201, "")
	toggleTeleconsultation(t, f, contextID(t, f, owner), false)
	w = pubRequest(f, "GET", directory+"&modality=TELECONSULTATION", "", nil)
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal(w.Body.String())
	}

	data, err := staffBookingDocument([]byte(`{"id":"1","patientId":"2","meetingUrl":"private","answers":{"insurance":"synthetic-sensitive-answer"},"clinicalNote":"synthetic-sensitive-note","clinicSnapshot":{"name":"Virtual","modality":"TELECONSULTATION","meetingUrl":"private"},"specialty":{"name":"General","private":"private"}}`))
	if err != nil || strings.Contains(jsonString(data), "private") || strings.Contains(jsonString(data), "synthetic-sensitive") || !strings.Contains(jsonString(data), "TELECONSULTATION") {
		t.Fatal(data, err)
	}
}

func TestTeleconsultationStaffScopeAndConcurrentBooking(t *testing.T) {
	f, physical, start := staffFixture(t)
	virtual := virtualClinic(t, f)
	start = start.Add(3 * time.Hour)
	weekday := (int(start.Weekday())+6)%7 + 1
	assertCode(t, f.request("PUT", "/clinics/"+virtual.ID+"/schedule", owner, scheduleBody("0", weekday, "12:00", "15:00")), 200, "")
	_, code := staffInvite(t, f, owner, virtual, []string{"view", "create", "reschedule", "cancel"})
	client := staffActivateTest(t, f, code)
	w := staffReq(f, client, "GET", "/assignments", nil)
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), `"modality":"TELECONSULTATION"`) {
		t.Fatal(w.Body.String())
	}
	base := "/owners/" + owner
	assertCode(t, staffReq(f, client, "GET", base+"/clinics/"+physical.ID+"/availability?from="+start.Format(civilLayout)+"&days=1", nil), 404, "CLINIC_NOT_FOUND")
	body := map[string]string{"clinicId": virtual.ID, "start": start.Format(time.RFC3339), "contactName": "Persona de ensayo"}
	ch := make(chan *httptest.ResponseRecorder, 2)
	go func() { ch <- staffReq(f, client, "POST", base+"/bookings", body) }()
	go func() { ch <- bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), jsonString(body)) }()
	first, second := <-ch, <-ch
	if second.Code == 201 {
		first, second = second, first
	}
	assertCode(t, first, 201, "")
	assertCode(t, second, 409, "BOOKING_SLOT_UNAVAILABLE")
	b := booked(t, first)
	move := map[string]string{"clinicId": virtual.ID, "start": start.Add(time.Hour).Format(time.RFC3339), "expectedVersion": b.Version}
	w = staffReq(f, client, "POST", base+"/bookings/"+b.ID+"/reschedule", move)
	assertCode(t, w, 200, "")
	moved := booked(t, w)
	if moved.ClinicSnapshot.Modality != teleconsultation || !moved.Start.Equal(start.Add(time.Hour)) || strings.Contains(w.Body.String(), "patientId") {
		t.Fatal(w.Body.String())
	}
	assertCode(t, staffReq(f, client, "PATCH", base+"/bookings/"+b.ID+"/status", map[string]string{"status": "CANCELLED", "expectedVersion": moved.Version}), 200, "")
}

func TestRecurringTeleconsultationChecksDistantExceptionsAndDST(t *testing.T) {
	today, _ := civilDate("2026-01-01")
	cfg := func(start, end string) ScheduleConfig {
		return ScheduleConfig{Weekly: []ScheduleDay{{Weekday: 7, Ranges: []ScheduleRange{{Start: start, End: end}}}}}
	}
	a := agendaRecord{id: "1", name: "Presencial", zone: "UTC", modality: inPerson, config: cfg("14:00", "15:00")}
	b := agendaRecord{id: "2", name: "Virtual", zone: "America/New_York", modality: teleconsultation, config: cfg("10:00", "11:00")}
	if err := validateRecurringAgendas(t.Context(), []agendaRecord{a, b}, b.id, today); err == nil {
		t.Fatal("summer offset overlap was accepted")
	}
	b.zone = "UTC"
	b.config.Exceptions = []ScheduleException{{From: "2050-05-01", Through: "2050-05-09", Ranges: []ScheduleRange{{Start: "14:30", End: "15:30"}}}}
	if err := validateRecurringAgendas(t.Context(), []agendaRecord{a, b}, b.id, today); err == nil {
		t.Fatal("exception outside the booking horizon was accepted")
	}
	b.config.Exceptions[0].Ranges[0].Start = "15:00"
	if err := validateRecurringAgendas(t.Context(), []agendaRecord{a, b}, b.id, today); err != nil {
		t.Fatal(err)
	}
	day, _ := civilDate("2026-11-01")
	b.zone, b.config = "America/New_York", cfg("01:00", "01:30")
	windows, err := agendaWindows(t.Context(), b, day, 1)
	if err != nil || len(windows) != 2 || windows[1].Start.Sub(windows[0].Start) != time.Hour {
		t.Fatal(windows, err)
	}
}
