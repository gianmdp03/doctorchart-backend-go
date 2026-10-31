package clinics

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestPublicTwoVisitorsAndSameReceiptConcurrent(t *testing.T) {
	for _, same := range []bool{false, true} {
		t.Run(fmt.Sprint("same receipt=", same), func(t *testing.T) {
			f, _, start, p := publicFixture(t)
			one := publicBody(t, f, p, start)
			two := one
			if !same {
				two = publicBody(t, f, p, start)
			}
			key1, key2 := newOpaqueID(), newOpaqueID()
			if same {
				key2 = key1
			}
			path := "/public/appointments/doctors/" + p.PublicID + "/bookings"
			done := make(chan int, 2)
			go func() { done <- pubRequest(f, "POST", path, key1, one).Code }()
			go func() { done <- pubRequest(f, "POST", path, key2, two).Code }()
			a, b := <-done, <-done
			if same {
				if a != 201 || b != 201 {
					t.Fatal(a, b)
				}
			} else if !((a == 201 && b == 409) || (b == 201 && a == 409)) {
				t.Fatal(a, b)
			}
			var count int
			if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM bookings").Scan(&count); err != nil || count != 1 {
				t.Fatal(count, err)
			}
		})
	}
}
func TestPublicPublicationAndScheduleRaces(t *testing.T) {
	for _, kind := range []string{"publication", "schedule"} {
		t.Run(kind, func(t *testing.T) {
			f, c, start, p := publicFixture(t)
			body := publicBody(t, f, p, start)
			done := make(chan int, 1)
			go func() {
				done <- pubRequest(f, "POST", "/public/appointments/doctors/"+p.PublicID+"/bookings", newOpaqueID(), body).Code
			}()
			status := 0
			if kind == "publication" {
				p.ExpectedVersion = p.Version
				p.Published = false
				status = f.request("PUT", "/booking-publication", owner, jsonString(p)).Code
			} else {
				status = f.request("PUT", "/clinics/"+c.ID+"/schedule", owner, `{"expectedVersion":"1","slotMinutes":30,"minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":[],"exceptions":[]}`).Code
			}
			book := <-done
			if kind == "publication" {
				if status != 200 || (book != 201 && book != 404) {
					t.Fatal(book, status)
				}
			} else if !((book == 201 && status == 409) || (book == 409 && status == 200)) {
				t.Fatal(book, status)
			}
			_ = start
		})
	}
}
func TestPublicReplayAndFailureRollBackAdministrativeDetails(t *testing.T) {
	f, _, start, p := publicFixture(t)
	body := publicBody(t, f, p, start)
	path := "/public/appointments/doctors/" + p.PublicID + "/bookings"
	key := newOpaqueID()
	if _, err := f.pool.Exec(t.Context(), "CREATE FUNCTION fail_public_event_fixture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic failure'; END $$; CREATE TRIGGER fail_public_event_fixture BEFORE INSERT ON booking_events FOR EACH ROW EXECUTE FUNCTION fail_public_event_fixture()"); err != nil {
		t.Fatal(err)
	}
	assertCode(t, pubRequest(f, "POST", path, key, body), 500, "INTERNAL_ERROR")
	var count int
	if err := f.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM bookings)+(SELECT count(*) FROM booking_public_details)+(SELECT count(*) FROM booking_idempotency)").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial public write", count, err)
	}
	if _, err := f.pool.Exec(t.Context(), "DROP TRIGGER fail_public_event_fixture ON booking_events;DROP FUNCTION fail_public_event_fixture()"); err != nil {
		t.Fatal(err)
	}
	w := pubRequest(f, "POST", path, key, body)
	assertCode(t, w, 201, "")
	var confirmed map[string]any
	json.Unmarshal(w.Body.Bytes(), &confirmed)
	f.mode.Store(1)
	if err := f.rights.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	again := pubRequest(f, "POST", path, key, body)
	assertCode(t, again, 201, "")
	if again.Body.String() != w.Body.String() {
		t.Fatal("retry changed")
	}
	assertCode(t, pubRequest(f, "GET", "/public/appointments/doctors/"+p.PublicID, "", nil), 503, "ENTITLEMENTS_UNAVAILABLE")
}
