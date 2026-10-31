package clinics

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBookingAttendeeAdministrativeIdentity(t *testing.T) {
	f, c, start := bookingFixture(t)
	body := strings.TrimSuffix(createBookingBody(c.ID, start), "}") + `,"attendeeName":"  Persona atendida  "}`
	key := newBookingKey()
	w := bookingRequestFixture(f, "POST", "/bookings", owner, key, body)
	assertCode(t, w, 201, "")
	b := booked(t, w)
	if b.AttendeeName != "Persona atendida" || b.ContactName == b.AttendeeName || b.PatientID != nil {
		t.Fatal(b)
	}
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, key, body), 201, "")
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), strings.TrimSuffix(createBookingBody(c.ID, start.Add(time.Hour)), "}")+`,"attendeeName":" "}`), 400, "INVALID_REQUEST")
	moved := bookingRequestFixture(f, "POST", "/bookings/"+b.ID+"/reschedule", owner, newBookingKey(), fmt.Sprintf(`{"expectedVersion":"1","clinicId":%q,"start":%q}`, c.ID, start.Add(time.Hour).Format(time.RFC3339)))
	assertCode(t, moved, 200, "")
	if booked(t, moved).AttendeeName != b.AttendeeName {
		t.Fatal("move changed attendee")
	}
	if _, err := f.pool.Exec(t.Context(), "UPDATE bookings SET attendee_name='silent change',version=version+1 WHERE id=$1", b.ID); err == nil {
		t.Fatal("SQL silently changed attendee")
	}
	legacy := bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start))
	assertCode(t, legacy, 201, "")
	old := booked(t, legacy)
	if old.AttendeeName != old.ContactName {
		t.Fatal("legacy create lost fallback", old)
	}
	var patients int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM patients").Scan(&patients); err != nil || patients != 0 {
		t.Fatal("administrative attendee created clinical patient", patients, err)
	}
}
