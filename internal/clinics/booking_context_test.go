package clinics

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Direct SQL writers deliberately bypass the Go cross-clinic schedule guard to
// exercise the unchanged global PostgreSQL booking exclusion.
func copyScheduleDirect(t *testing.T, f *fixture, from, to string, offset int) {
	t.Helper()
	if _, err := f.pool.Exec(t.Context(), "UPDATE clinics SET timezone=(SELECT timezone FROM clinics WHERE id=$1),version=version+1 WHERE id=$2", from, to); err != nil {
		t.Fatal(err)
	}
	_, err := f.pool.Exec(t.Context(), `WITH root AS (INSERT INTO clinic_schedules(clinic_id,workspace_id,min_notice_minutes,max_advance_days) SELECT $2,workspace_id,0,30 FROM clinics WHERE id=$1 RETURNING clinic_id,workspace_id)
 INSERT INTO schedule_blocks(clinic_id,workspace_id,specialty_context_id,weekday,start_minute,end_minute,slot_minutes)
 SELECT root.clinic_id,root.workspace_id,b.specialty_context_id,b.weekday,b.start_minute+$3,b.end_minute+$3,b.slot_minutes FROM root CROSS JOIN schedule_blocks b WHERE b.clinic_id=$1 AND b.weekday IS NOT NULL`, from, to, offset)
	if err != nil {
		t.Fatal(err)
	}
}
func insertBookingDirect(ctx context.Context, f *fixture, clinic, ownerClinic string, patient any, start, end time.Time) error {
	result, err := f.pool.Exec(ctx, `INSERT INTO bookings(workspace_id,clinic_id,patient_id,start_at,end_at,contact_name,specialty_context_id,specialty_snapshot,clinic_snapshot,provenance)
 SELECT owner.workspace_id,$1,$3,$4,$5,'Direct SQL fixture',c.id,jsonb_build_object('id',s.id::text,'code',s.code,'name',s.display_name),public.booking_clinic_snapshot($1),public.booking_block_snapshot(b.id)
 FROM clinics owner JOIN doctor_specialty_contexts c ON c.workspace_id=owner.workspace_id JOIN specialties s ON s.id=c.specialty_id
 CROSS JOIN schedule_blocks b WHERE owner.id=$2 AND b.clinic_id=$1 AND b.weekday=extract(isodow FROM $4::timestamptz AT TIME ZONE 'UTC') LIMIT 1`, clinic, ownerClinic, patient, start, end)
	if err == nil && result.RowsAffected() != 1 {
		return fmt.Errorf("direct booking fixture has no matching source block")
	}
	return err
}

func TestBookingContextScopedAvailabilityHidesUnassignedClinic(t *testing.T) {
	f, c, start := staffFixture(t)
	hidden := scheduleClinicFixture(t, f)
	copyScheduleDirect(t, f, c.ID, hidden.ID, 0)
	_, code := staffInvite(t, f, owner, c, []string{"view"})
	client := staffActivateTest(t, f, code)
	w := staffReq(f, client, "GET", "/owners/"+owner+"/clinics/"+c.ID+"/availability?from="+start.Format("2006-01-02")+"&days=1", nil)
	assertCode(t, w, 409, "BOOKING_SLOT_UNAVAILABLE")
	if strings.Contains(w.Body.String(), hidden.Name) {
		t.Fatal("scoped availability leaked unassigned clinic", w.Body.String())
	}
}

func TestBookingContextSnapshots(t *testing.T) {
	f, c, start := bookingFixture(t)
	key := newBookingKey()
	body := createBookingBody(c.ID, start)
	reply := bookingRequestFixture(f, "POST", "/bookings", owner, key, body)
	assertCode(t, reply, 201, "")
	b := booked(t, reply)
	if b.ContextID != contextID(t, f, owner) || b.Specialty.Name == "" || b.ClinicSnapshot.Name != c.Name || b.Provenance.Kind != "SCHEDULE_BLOCK" || b.Provenance.BlockID == "" || b.Provenance.SlotMinutes != 30 {
		t.Fatal("missing context/provenance", b)
	}
	// Explicit scope is checked against the authorized slot, not merely its format.
	wrong := body[:len(body)-1] + fmt.Sprintf(`,"specialtyContextId":%q}`, contextID(t, f, other))
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), wrong), 409, "BOOKING_CONTEXT_MISMATCH")
	if _, err := f.pool.Exec(t.Context(), `WITH renamed AS (UPDATE specialties SET display_name='Renamed after booking' WHERE id=$1) UPDATE clinics SET name='Renamed clinic',version=version+1 WHERE id=$2`, b.Specialty.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	current := booked(t, f.request("GET", "/bookings/"+b.ID, owner, ""))
	if current.Specialty != b.Specialty || current.ClinicSnapshot != b.ClinicSnapshot {
		t.Fatal("mutable data rewrote booking")
	}
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, key, body), 201, "")
	// Destination snapshots update explicitly while the specialty snapshot stays historical.
	reply = bookingRequestFixture(f, "POST", "/bookings/"+b.ID+"/reschedule", owner, newBookingKey(), fmt.Sprintf(`{"expectedVersion":"1","clinicId":%q,"start":%q}`, c.ID, start.Add(time.Hour).Format(time.RFC3339)))
	assertCode(t, reply, 200, "")
	moved := booked(t, reply)
	if moved.Specialty != b.Specialty || moved.ContextID != b.ContextID || moved.ClinicSnapshot.Name != "Renamed clinic" || moved.Provenance.BlockID != b.Provenance.BlockID {
		t.Fatal("reschedule lost history", moved)
	}
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"2","status":"CANCELLED"}`), 200, "")
	fresh := booked(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), body))
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+fresh.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"1","status":"CANCELLED"}`), 200, "")
	agenda := agendaDTO(t, f, c)
	agenda.Weekly = []ScheduleDay{}
	agenda.Exceptions = []ScheduleException{}
	putBlocks(t, f, c, agenda, 200, "")
	retained := booked(t, f.request("GET", "/bookings/"+b.ID, owner, ""))
	if retained.Provenance != moved.Provenance || retained.Specialty != b.Specialty {
		t.Fatal("deleting retired block erased provenance", retained)
	}
}
