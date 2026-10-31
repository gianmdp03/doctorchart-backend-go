package clinics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

var bookingKeyCounter atomic.Int64

func bookingFixture(t *testing.T) (*fixture, Clinic, time.Time) {
	t.Helper()
	f := newFixture(t)
	f.limit.Store(-1)
	mux := http.NewServeMux()
	NewWithBookings(f.pool, f.rights, true, true).Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	c := scheduleClinicFixture(t, f)
	// UTC removes the machine timezone from the HTTP fixture.
	assertCode(t, f.request("PATCH", "/clinics/"+c.ID, owner, `{"expectedVersion":"0","timezone":"UTC"}`), 200, "")
	c.Timezone = "UTC"
	c.Version = "1"
	day := time.Now().UTC().AddDate(0, 0, 2)
	start := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, time.UTC)
	weekday := (int(start.Weekday())+6)%7 + 1
	assertCode(t, f.request("PUT", "/clinics/"+c.ID+"/schedule", owner, scheduleBody("0", weekday, "09:00", "12:00")), 200, "")
	return f, c, start
}
func bookingRequestFixture(f *fixture, method, path, user, key, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/doctorchart"+path, strings.NewReader(body))
	r.Header.Set("X-Gateway-Secret", "gateway-fixture")
	r.Header.Set("X-User-Id", user)
	r.Header.Set("X-User-Role", "COMPANY")
	r.Header.Set("X-User-Plan", "BASIC")
	r.Header.Set("X-User-Plan-Expires-At", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func newBookingKey() string { return fmt.Sprintf("booking-key-%020d", bookingKeyCounter.Add(1)) }
func createBookingBody(clinic string, start time.Time) string {
	return fmt.Sprintf(`{"clinicId":%q,"start":%q,"contactName":"Contacto sintético","contactPhone":"123"}`, clinic, start.Format(time.RFC3339Nano))
}
func booked(t *testing.T, w *httptest.ResponseRecorder) Booking {
	t.Helper()
	var b Booking
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(w.Body.String(), err)
	}
	return b
}
func bookingCounts(t *testing.T, f *fixture) (int, int, int) {
	t.Helper()
	var bookings, events, keys int
	if err := f.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM bookings),(SELECT count(*) FROM booking_events),(SELECT count(*) FROM booking_idempotency)").Scan(&bookings, &events, &keys); err != nil {
		t.Fatal(err)
	}
	return bookings, events, keys
}
func TestBookingsCreateCancelReleaseReplayAndEvents(t *testing.T) {
	f, c, start := bookingFixture(t)
	key := newBookingKey()
	body := createBookingBody(c.ID, start)
	reply := bookingRequestFixture(f, "POST", "/bookings", owner, key, body)
	assertCode(t, reply, 201, "")
	b := booked(t, reply)
	if b.Status != "CONFIRMED" || b.Version != "1" || !b.End.Equal(start.Add(30*time.Minute)) || b.PatientID != nil {
		t.Fatal(b)
	}
	again := bookingRequestFixture(f, "POST", "/bookings", owner, key, body)
	assertCode(t, again, 201, "")
	if booked(t, again) != b {
		t.Fatal("retry changed reply", again.Body.String())
	}
	a, e, k := bookingCounts(t, f)
	if a != 1 || e != 1 || k != 1 {
		t.Fatal(a, e, k)
	}
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, key, createBookingBody(c.ID, start.Add(time.Hour))), 409, "IDEMPOTENCY_CONFLICT")
	avail := f.request("GET", "/clinics/"+c.ID+"/availability?from="+start.Format("2006-01-02")+"&days=1", owner, "")
	assertCode(t, avail, 200, "")
	if strings.Contains(avail.Body.String(), `"start":"`+start.Format(time.RFC3339)+`"`) {
		t.Fatal("occupied slot leaked", avail.Body.String())
	}
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), body), 409, "BOOKING_SLOT_UNAVAILABLE")
	cancelKey := newBookingKey()
	cancelBody := `{"expectedVersion":"1","status":"CANCELLED"}`
	reply = bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, cancelKey, cancelBody)
	assertCode(t, reply, 200, "")
	cancelled := booked(t, reply)
	if cancelled.Status != "CANCELLED" || cancelled.Version != "2" {
		t.Fatal(cancelled)
	}
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, cancelKey, cancelBody), 200, "")
	avail = f.request("GET", "/clinics/"+c.ID+"/availability?from="+start.Format("2006-01-02")+"&days=1", owner, "")
	if !strings.Contains(avail.Body.String(), `"start":"`+start.Format(time.RFC3339)+`"`) {
		t.Fatal("cancel did not release slot", avail.Body.String())
	}
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), body), 201, "")
	events := f.request("GET", "/bookings/"+b.ID+"/events", owner, "")
	assertCode(t, events, 200, "")
	var history struct {
		Items []struct {
			Version, Kind string
			Before        *Booking
			After         Booking
		}
	}
	if err := json.Unmarshal(events.Body.Bytes(), &history); err != nil || len(history.Items) != 2 || history.Items[0].Before != nil || history.Items[1].After.Status != "CANCELLED" {
		t.Fatal(events.Body.String(), err)
	}
}
func TestBookingsConcurrentSameSlotAndSameIdempotency(t *testing.T) {
	for _, sameKey := range []bool{false, true} {
		t.Run(fmt.Sprint(sameKey), func(t *testing.T) {
			f, c, start := bookingFixture(t)
			key := newBookingKey()
			body := createBookingBody(c.ID, start)
			done := make(chan *httptest.ResponseRecorder, 2)
			for i := 0; i < 2; i++ {
				k := key
				if !sameKey {
					k = newBookingKey()
				}
				go func(k string) { done <- bookingRequestFixture(f, "POST", "/bookings", owner, k, body) }(k)
			}
			a, b := <-done, <-done
			if sameKey {
				assertCode(t, a, 201, "")
				assertCode(t, b, 201, "")
				if booked(t, a).ID != booked(t, b).ID {
					t.Fatal("duplicate confirmations")
				}
			} else if !((a.Code == 201 && b.Code == 409) || (b.Code == 201 && a.Code == 409)) {
				t.Fatal(a.Code, b.Code, a.Body.String(), b.Body.String())
			}
			count, events, keys := bookingCounts(t, f)
			if count != 1 || events != 1 || keys != 1 {
				t.Fatal("race side effects", count, events, keys)
			}
		})
	}
}
func TestBookingsRescheduleRollbackAndConcurrentVersions(t *testing.T) {
	f, c, start := bookingFixture(t)
	first := booked(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start)))
	second := booked(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start.Add(time.Hour))))
	reschedule := func(b Booking, target time.Time, key string) *httptest.ResponseRecorder {
		return bookingRequestFixture(f, "POST", "/bookings/"+b.ID+"/reschedule", owner, key, fmt.Sprintf(`{"expectedVersion":%q,"clinicId":%q,"start":%q}`, b.Version, c.ID, target.Format(time.RFC3339)))
	}
	assertCode(t, reschedule(first, second.Start, newBookingKey()), 409, "BOOKING_SLOT_UNAVAILABLE")
	original := booked(t, f.request("GET", "/bookings/"+first.ID, owner, ""))
	if original != first {
		t.Fatal("failed move changed original")
	}
	done := make(chan *httptest.ResponseRecorder, 2)
	for _, target := range []time.Time{start.Add(30 * time.Minute), start.Add(90 * time.Minute)} {
		go func(target time.Time) { done <- reschedule(first, target, newBookingKey()) }(target)
	}
	a, b := <-done, <-done
	if !((a.Code == 200 && b.Code == 409) || (a.Code == 409 && b.Code == 200)) {
		t.Fatal(a.Code, b.Code)
	}
	updated := booked(t, f.request("GET", "/bookings/"+first.ID, owner, ""))
	if updated.Version != "2" || updated.Start.Equal(first.Start) {
		t.Fatal(updated)
	}
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+first.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"1","status":"CANCELLED"}`), 409, "VERSION_CONFLICT")
}
func TestBookingsScheduleChangesAndVacations(t *testing.T) {
	f, c, start := bookingFixture(t)
	body := createBookingBody(c.ID, start)
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start.Add(-time.Hour))), 409, "BOOKING_SLOT_UNAVAILABLE")
	b := booked(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), body))
	weekday := (int(start.Weekday())+6)%7 + 1
	reply := f.request("PUT", "/clinics/"+c.ID+"/schedule", owner, scheduleBody("1", weekday, "10:00", "12:00"))
	assertCode(t, reply, 409, "BOOKING_SCHEDULE_CONFLICT")
	schedule := f.request("GET", "/clinics/"+c.ID+"/schedule", owner, "")
	if !strings.Contains(schedule.Body.String(), `"version":"1"`) {
		t.Fatal("schedule conflict persisted", schedule.Body.String())
	}
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"1","status":"CANCELLED"}`), 200, "")
	vacation := fmt.Sprintf(`{"expectedVersion":"1","slotMinutes":30,"minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":[{"weekday":%d,"ranges":[{"start":"09:00","end":"12:00"}]}],"exceptions":[{"from":%q,"through":%q,"label":"Vacaciones","ranges":[]}]}`, weekday, start.Format("2006-01-02"), start.Format("2006-01-02"))
	assertCode(t, f.request("PUT", "/clinics/"+c.ID+"/schedule", owner, vacation), 200, "")
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), body), 409, "BOOKING_SLOT_UNAVAILABLE")
}
func TestBookingsConcurrentScheduleAndCreate(t *testing.T) {
	f, c, start := bookingFixture(t)
	done := make(chan int, 2)
	go func() {
		done <- bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start)).Code
	}()
	go func() {
		done <- f.request("PUT", "/clinics/"+c.ID+"/schedule", owner, fmt.Sprintf(`{"expectedVersion":"1","slotMinutes":30,"minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":[],"exceptions":[]}`)).Code
	}()
	a, b := <-done, <-done
	if !((a == 201 && b == 409) || (a == 409 && b == 200) || (a == 200 && b == 409) || (a == 409 && b == 201)) {
		t.Fatal("booking and incompatible schedule both committed", a, b)
	}
}
