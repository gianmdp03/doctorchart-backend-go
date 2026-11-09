package clinics

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestBookingsPatientOwnershipAndClinicalPreservation(t *testing.T) {
	f, c, start := bookingFixture(t)
	for i, user := range []string{owner, other} {
		_, err := f.pool.Exec(t.Context(), "INSERT INTO patients(id,user_id,full_name,birth_date,form_definition_snapshot,form_values,formula_context,formula_results,updated_at,version,workspace_version,personal_history) VALUES($1,$2,'Historia sintética','1990-01-01','{}','{}','{}','{}',clock_timestamp(),7,0,'{\"preservar\":true}')", i+1, user)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.pool.Exec(t.Context(), "INSERT INTO clinical_encounters(consultation_type,encounter_date,form_definition_snapshot,form_values,formula_context,formula_results,updated_at,version,workspace_version,patient_id,specialty_id) SELECT 'FIRST_VISIT',now(),'{}','{}','{}','{}',now(),3,0,1,specialty_id FROM doctor_workspaces WHERE user_id=$1", owner); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var value string
		if err := f.pool.QueryRow(t.Context(), "SELECT json_build_object('patients',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM patients p),'encounters',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM clinical_encounters e))::text").Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	base := createBookingBody(c.ID, start)
	cross := strings.TrimSuffix(base, "}") + `,"patientId":"2"}`
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), cross), 404, "PATIENT_NOT_FOUND")
	linked := strings.TrimSuffix(base, "}") + `,"patientId":"1"}`
	reply := bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), linked)
	assertCode(t, reply, 201, "")
	b := booked(t, reply)
	if b.PatientID == nil || *b.PatientID != "1" {
		t.Fatal(b)
	}
	for _, path := range []string{"/bookings/" + b.ID, "/bookings/" + b.ID + "/events"} {
		assertCode(t, f.request("GET", path, other, ""), 404, "BOOKING_NOT_FOUND")
	}
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", other, newBookingKey(), `{"expectedVersion":"1","status":"CANCELLED"}`), 404, "BOOKING_NOT_FOUND")
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings/"+b.ID+"/reschedule", other, newBookingKey(), fmt.Sprintf(`{"expectedVersion":"1","clinicId":%q,"start":%q}`, c.ID, start.Add(time.Hour).Format(time.RFC3339))), 404, "BOOKING_NOT_FOUND")
	listPath := "/bookings?from=" + start.Add(-time.Hour).Format(time.RFC3339) + "&to=" + start.Add(24*time.Hour).Format(time.RFC3339)
	reply = f.request("GET", listPath, other, "")
	assertCode(t, reply, 200, "")
	if !strings.Contains(reply.Body.String(), `"totalElements":0`) {
		t.Fatal(reply.Body.String())
	}
	reply = f.request("GET", listPath, owner, "")
	assertCode(t, reply, 200, "")
	if !strings.Contains(reply.Body.String(), `"totalElements":1`) {
		t.Fatal(reply.Body.String())
	}
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"1","status":"CANCELLED"}`), 200, "")
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), base), 201, "")
	if snapshot() != before {
		t.Fatal("booking operations changed patients or clinical history")
	}
}
func TestBookingsDatabaseExclusionAndOwnerGuards(t *testing.T) {
	f, c, start := bookingFixture(t)
	second := scheduleClinicFixture(t, f)
	b := booked(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start)))
	_, err := f.pool.Exec(t.Context(), "INSERT INTO bookings(workspace_id,clinic_id,start_at,end_at,contact_name) SELECT workspace_id,$1,$2,$3,'SQL bypass attempt' FROM clinics WHERE id=$4", second.ID, start.Add(15*time.Minute), start.Add(45*time.Minute), c.ID)
	var failure *pgconn.PgError
	if !errors.As(err, &failure) || failure.Code != "23P01" {
		t.Fatal("SQL exclusion did not protect cross-clinic owner interval", err)
	}
	// Another owner may use the exact same interval.
	otherClinicReply := f.request("POST", "/clinics", other, createBody)
	assertCode(t, otherClinicReply, 201, "")
	otherClinic := clinic(t, otherClinicReply)
	if _, err = f.pool.Exec(t.Context(), "INSERT INTO bookings(workspace_id,clinic_id,start_at,end_at,contact_name) SELECT workspace_id,id,$2,$3,'Other physician' FROM clinics WHERE id=$1", otherClinic.ID, start, b.End); err != nil {
		t.Fatal("different doctor incorrectly blocked", err)
	}
	// Composite FK protects clinic/workspace mismatches even outside Go.
	_, err = f.pool.Exec(t.Context(), "INSERT INTO bookings(workspace_id,clinic_id,start_at,end_at,contact_name) SELECT workspace_id,$1,$2,$3,'Wrong clinic' FROM clinics WHERE id=$4", otherClinic.ID, start.Add(2*time.Hour), b.End.Add(2*time.Hour), c.ID)
	if !errors.As(err, &failure) || failure.Code != "23503" {
		t.Fatal("SQL clinic ownership guard", err)
	}
	if _, err = f.pool.Exec(t.Context(), "INSERT INTO patients(id,user_id,full_name,birth_date,form_definition_snapshot,form_values,formula_context,formula_results,updated_at,version,workspace_version) VALUES(100,$1,'Other patient','1990-01-01','{}','{}','{}','{}',now(),0,0)", other); err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(t.Context(), "INSERT INTO bookings(workspace_id,clinic_id,patient_id,start_at,end_at,contact_name) SELECT workspace_id,id,100,$2,$3,'Wrong patient' FROM clinics WHERE id=$1", c.ID, start.Add(2*time.Hour), b.End.Add(2*time.Hour))
	if !errors.As(err, &failure) || failure.Code != "23514" || failure.ConstraintName != "bookings_patient_owner_check" {
		t.Fatal("SQL patient ownership guard", err)
	}
	// Terminal or skipped status changes are constrained in PostgreSQL too.
	_, err = f.pool.Exec(t.Context(), "UPDATE bookings SET status='COMPLETED',version=version+1 WHERE id=$1", b.ID)
	if !errors.As(err, &failure) || failure.Code != "23514" {
		t.Fatal("SQL transition guard", err)
	}
}
func TestBookingsAtomicEventAndIdempotencyFailures(t *testing.T) {
	for _, table := range []string{"booking_events", "booking_idempotency"} {
		t.Run(table, func(t *testing.T) {
			f, c, start := bookingFixture(t)
			key := newBookingKey()
			body := createBookingBody(c.ID, start)
			if _, err := f.pool.Exec(t.Context(), "CREATE FUNCTION fixture_reject_booking_write() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic rollback required'; END$$; CREATE TRIGGER fixture_reject_booking_write BEFORE INSERT ON "+table+" FOR EACH ROW EXECUTE FUNCTION fixture_reject_booking_write()"); err != nil {
				t.Fatal(err)
			}
			reply := bookingRequestFixture(f, "POST", "/bookings", owner, key, body)
			assertCode(t, reply, 500, "")
			if strings.Contains(reply.Body.String(), "synthetic rollback") || strings.Contains(reply.Body.String(), "INSERT") {
				t.Fatal("database details exposed", reply.Body.String())
			}
			bookings, events, receipts := bookingCounts(t, f)
			if bookings != 0 || events != 0 || receipts != 0 {
				t.Fatal("partial booking committed", bookings, events, receipts)
			}
			if _, err := f.pool.Exec(t.Context(), "DROP TRIGGER fixture_reject_booking_write ON "+table+"; DROP FUNCTION fixture_reject_booking_write()"); err != nil {
				t.Fatal(err)
			}
			assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, key, body), 201, "")
		})
	}
}
func TestBookingsConcurrentCancellationAndAdministrativeStates(t *testing.T) {
	f, c, start := bookingFixture(t)
	b := booked(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start)))
	done := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			done <- bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"1","status":"CANCELLED"}`)
		}()
	}
	a, z := <-done, <-done
	if !((a.Code == 200 && z.Code == 409) || (a.Code == 409 && z.Code == 200)) {
		t.Fatal(a.Code, z.Code)
	}
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"2","status":"CONFIRMED"}`), 409, "BOOKING_STATE_CONFLICT")
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"2","status":"CANCELLED"}`), 200, "")
	b = booked(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start)))
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"1","status":"COMPLETED"}`), 409, "BOOKING_STATE_CONFLICT")
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"1","status":"CHECKED_IN"}`), 200, "")
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"2","status":"COMPLETED"}`), 200, "")
}
func TestBookingsFlagsAndMalformedRequests(t *testing.T) {
	f, c, start := bookingFixture(t)
	body := createBookingBody(c.ID, start)
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, "", body), 400, "INVALID_REQUEST")
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, "short", body), 400, "INVALID_REQUEST")
	for _, bad := range []string{strings.TrimSuffix(body, "}") + `,"extra":true}`, strings.Replace(body, c.ID, "01", 1), `{"clinicId":"1","start":"2026-11-02T09:00:00","contactName":"Test"}`, `{"clinicId":"1","start":"2026-11-02T09:00:00Z","contactName":null}`} {
		assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), bad), 400, "INVALID_REQUEST")
	}
	mux := http.NewServeMux()
	NewWithSchedules(f.pool, f.rights, true).Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), body), 404, "")
}
func TestBookingsReplaySurvivesBillingOutageWithoutNewWrites(t *testing.T) {
	f, c, start := bookingFixture(t)
	key := newBookingKey()
	body := createBookingBody(c.ID, start)
	reply := bookingRequestFixture(f, "POST", "/bookings", owner, key, body)
	assertCode(t, reply, 201, "")
	original := booked(t, reply)
	f.mode.Store(1)
	if err := f.rights.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	reply = bookingRequestFixture(f, "POST", "/bookings", owner, key, body)
	assertCode(t, reply, 201, "")
	var duplicate Booking
	if err := json.Unmarshal(reply.Body.Bytes(), &duplicate); err != nil || duplicate.ID != original.ID {
		t.Fatal(err, reply.Body.String())
	}
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start.Add(time.Hour))), 503, "ENTITLEMENTS_UNAVAILABLE")
	a, e, k := bookingCounts(t, f)
	if a != 1 || e != 1 || k != 1 {
		t.Fatal(a, e, k)
	}
}
