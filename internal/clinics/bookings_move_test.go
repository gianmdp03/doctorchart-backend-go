package clinics

import (
	"fmt"
	"testing"
	"time"
)

func TestBookingsMoveBetweenClinicsAndConcurrentCancelledMove(t *testing.T) {
	f, c, start := bookingFixture(t)
	destination := scheduleClinicFixture(t, f)
	assertCode(t, f.request("PATCH", "/clinics/"+destination.ID, owner, `{"expectedVersion":"0","timezone":"UTC"}`), 200, "")
	weekday := (int(start.Weekday())+6)%7 + 1
	assertCode(t, f.request("PUT", "/clinics/"+destination.ID+"/schedule", owner, scheduleBody("0", weekday, "13:00", "15:00")), 200, "")
	reply := bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start))
	assertCode(t, reply, 201, "")
	b := booked(t, reply)
	target := start.Add(4 * time.Hour)
	moveBody := fmt.Sprintf(`{"expectedVersion":"1","clinicId":%q,"start":%q}`, destination.ID, target.Format(time.RFC3339))
	key := newBookingKey()
	reply = bookingRequestFixture(f, "POST", "/bookings/"+b.ID+"/reschedule", owner, key, moveBody)
	assertCode(t, reply, 200, "")
	moved := booked(t, reply)
	if moved.ID != b.ID || moved.ClinicID != destination.ID || !moved.Start.Equal(target) || moved.ContactName != b.ContactName || moved.Version != "2" {
		t.Fatal(moved)
	}
	if moved.ContextID != b.ContextID || moved.Specialty != b.Specialty || moved.Provenance.BlockID == b.Provenance.BlockID || moved.ClinicSnapshot.Name != destination.Name {
		t.Fatal("move lost context or destination provenance", moved)
	}
	var beforeBlock, afterBlock string
	if err := f.pool.QueryRow(t.Context(), "SELECT before_document->'provenance'->>'blockId',after_document->'provenance'->>'blockId' FROM booking_events WHERE booking_id=$1 AND kind='RESCHEDULED'", b.ID).Scan(&beforeBlock, &afterBlock); err != nil || beforeBlock != b.Provenance.BlockID || afterBlock != moved.Provenance.BlockID {
		t.Fatal("reschedule history lost original block", err)
	}
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings/"+b.ID+"/reschedule", owner, key, moveBody), 200, "")
	// The original clinic slot is really free after the atomic move.
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start)), 201, "")
	done := make(chan int, 2)
	go func() {
		done <- bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"2","status":"CANCELLED"}`).Code
	}()
	go func() {
		done <- bookingRequestFixture(f, "POST", "/bookings/"+b.ID+"/reschedule", owner, newBookingKey(), fmt.Sprintf(`{"expectedVersion":"2","clinicId":%q,"start":%q}`, destination.ID, target.Add(time.Hour).Format(time.RFC3339))).Code
	}()
	a, z := <-done, <-done
	if !((a == 200 && z == 409) || (a == 409 && z == 200)) {
		t.Fatal("cancel and move both admitted same version", a, z)
	}
}
