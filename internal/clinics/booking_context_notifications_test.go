package clinics

import (
	"strings"
	"testing"
)

func bookingLinkPatientFixture(t *testing.T, f *fixture) string {
	t.Helper()
	var id string
	err := f.pool.QueryRow(t.Context(), `INSERT INTO patients(user_id,workspace_id,specialty_context_id,full_name,birth_date,form_definition_snapshot,form_values,formula_context,formula_results,updated_at,version,workspace_version)
 SELECT w.user_id,w.id,c.id,'Nombre clínico reservado','1990-01-01','{}','{}','{}','{}',now(),0,0 FROM doctor_workspaces w JOIN doctor_specialty_contexts c ON c.workspace_id=w.id WHERE w.user_id=$1 RETURNING id::text`, owner).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestBookingLegacyLinkKeepsHistoricalConfirmation(t *testing.T) {
	f, c, start, p, notices, sender := notificationFixture(t)
	receipt, b := notificationBooking(t, f, p, start)
	patient := bookingLinkPatientFixture(t, f)
	seedLegacyBookingLink(t, f, b.ID, patient)
	var count int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_outbox WHERE portal_code_id IS NOT NULL").Scan(&count); err != nil || count != 1 {
		t.Fatal("link changed the verification queue", count, err)
	}
	if _, err := f.pool.Exec(t.Context(), `WITH renamed AS (UPDATE specialties SET display_name='New catalog name' WHERE id=$1), changed_address AS (UPDATE clinic_publications SET address='New public address' WHERE clinic_id=$2) UPDATE clinics SET name='New clinic name',version=version+1 WHERE id=$2`, b.Specialty.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	access := portalVerify(t, f, receipt, portalCodeFromMail(t, notices, sender))
	w := pubRequest(f, "POST", "/public/appointments/my-bookings", "", struct{}{}, access)
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), b.Specialty.Name) || !strings.Contains(w.Body.String(), b.ClinicSnapshot.Name) || strings.Contains(w.Body.String(), "New catalog name") || strings.Contains(w.Body.String(), "New clinic name") || strings.Contains(w.Body.String(), "New public address") || strings.Contains(w.Body.String(), "Nombre clínico reservado") {
		t.Fatal("confirmation depends on mutable or clinical data", w.Body.String())
	}

}

// Existing installations may still have historical links and old idempotency documents.
func seedLegacyBookingLink(t *testing.T, f *fixture, booking, patient string) {
	t.Helper()
	if _, err := f.pool.Exec(t.Context(), `WITH updated AS (
		UPDATE bookings SET patient_id=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1 RETURNING id,version,patient_id
	) INSERT INTO booking_events(booking_id,version,kind,actor_id,before_document,after_document)
	SELECT u.id,u.version,'PATIENT_LINKED',$3,e.after_document,e.after_document || jsonb_build_object('patientId',u.patient_id::text)
	FROM updated u JOIN booking_events e ON e.booking_id=u.id AND e.version=u.version-1`, booking, patient, owner); err != nil {
		t.Fatal(err)
	}
}

func TestBookingLegacyClinicalIDIsExcludedFromReadsAndRetries(t *testing.T) {
	f, c, start := bookingFixture(t)
	key := newBookingKey()
	body := createBookingBody(c.ID, start)
	w := bookingRequestFixture(f, "POST", "/bookings", owner, key, body)
	assertCode(t, w, 201, "")
	b := booked(t, w)
	patient := bookingLinkPatientFixture(t, f)
	seedLegacyBookingLink(t, f, b.ID, patient)
	if _, err := f.pool.Exec(t.Context(), "UPDATE booking_idempotency SET response_document=response_document || jsonb_build_object('patientId',$2::text) WHERE booking_id=$1", b.ID, patient); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/bookings/" + b.ID, "/bookings/" + b.ID + "/events"} {
		w = f.request("GET", path, owner, "")
		assertCode(t, w, 200, "")
		if strings.Contains(w.Body.String(), `"patientId"`) {
			t.Fatal("clinical identifier in agenda response", w.Body.String())
		}
	}
	w = bookingRequestFixture(f, "POST", "/bookings", owner, key, body)
	assertCode(t, w, 201, "")
	if strings.Contains(w.Body.String(), `"patientId"`) {
		t.Fatal("clinical identifier in historical replay", w.Body.String())
	}
	assertCode(t, bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"2","status":"CANCELLED"}`), 200, "")
}
