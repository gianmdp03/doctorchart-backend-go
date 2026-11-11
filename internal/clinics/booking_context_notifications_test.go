package clinics

import (
	"encoding/base64"
	"fmt"
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
func TestBookingContextLinkKeepsPendingMessagesAndHistoricalConfirmation(t *testing.T) {
	f, c, start, p, n, sender := notificationFixture(t)
	receipt, b := notificationBooking(t, f, p, start)
	patient := bookingLinkPatientFixture(t, f)
	assertCode(t, bookingRequestFixture(f, "PUT", "/bookings/"+b.ID+"/patient", owner, newBookingKey(), fmt.Sprintf(`{"expectedVersion":"1","patientId":%q}`, patient)), 200, "")
	var count int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_outbox WHERE booking_id=$1", b.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("link duplicated/replaced notices", count, err)
	}
	if _, err := f.pool.Exec(t.Context(), `WITH renamed AS (UPDATE specialties SET display_name='New catalog name' WHERE id=$1), changed_address AS (UPDATE clinic_publications SET address='New public address' WHERE clinic_id=$2) UPDATE clinics SET name='New clinic name',version=version+1 WHERE id=$2`, b.Specialty.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	w := pubRequest(f, "POST", "/public/appointments/confirmation", "", map[string]string{"accessCode": receipt.AccessCode})
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), b.Specialty.Name) || !strings.Contains(w.Body.String(), b.ClinicSnapshot.Name) || strings.Contains(w.Body.String(), "New catalog name") || strings.Contains(w.Body.String(), "New clinic name") || strings.Contains(w.Body.String(), "New public address") || strings.Contains(w.Body.String(), "Nombre clínico reservado") {
		t.Fatal("confirmation depends on mutable or clinical data", w.Body.String())
	}
	processMail(t, n)
	if sender.count() != 1 {
		t.Fatal("patient link invalidated pending confirmation")
	}
	if _, err := f.pool.Exec(t.Context(), "UPDATE notification_outbox SET due_at=clock_timestamp()-interval '1 second' WHERE booking_id=$1 AND kind='BOOKING_REMINDER'", b.ID); err != nil {
		t.Fatal(err)
	}
	processMail(t, n)
	if sender.count() != 2 {
		t.Fatal("patient link invalidated pending reminder")
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	for _, message := range sender.messages {
		parts := strings.SplitN(string(message), "\r\n\r\n", 2)
		if len(parts) != 2 {
			t.Fatal("invalid MIME fixture")
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[1]))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(decoded), "Especialidad: "+b.Specialty.Name) || strings.Contains(string(decoded), "New catalog name") || strings.Contains(string(decoded), "Nombre clínico reservado") {
			t.Fatal("notice lost snapshot/privacy")
		}
	}
}

func TestBookingContextPatientLinkRollsBackAndRetries(t *testing.T) {
	f, c, start := bookingFixture(t)
	w := bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start))
	assertCode(t, w, 201, "")
	b := booked(t, w)
	patient := bookingLinkPatientFixture(t, f)
	key := newBookingKey()
	body := fmt.Sprintf(`{"expectedVersion":"1","patientId":%q}`, patient)
	if _, err := f.pool.Exec(t.Context(), `CREATE FUNCTION fixture_fail_link() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.kind='PATIENT_LINKED' THEN RAISE EXCEPTION 'fixture link failure'; END IF;RETURN NEW;END$$;CREATE TRIGGER fixture_fail_link BEFORE INSERT ON booking_events FOR EACH ROW EXECUTE FUNCTION fixture_fail_link()`); err != nil {
		t.Fatal(err)
	}
	assertCode(t, bookingRequestFixture(f, "PUT", "/bookings/"+b.ID+"/patient", owner, key, body), 500, "")
	current := booked(t, f.request("GET", "/bookings/"+b.ID, owner, ""))
	if current.PatientID != nil || current.Version != "1" {
		t.Fatal("failed link committed partial state")
	}
	if _, err := f.pool.Exec(t.Context(), "DROP TRIGGER fixture_fail_link ON booking_events;DROP FUNCTION fixture_fail_link()"); err != nil {
		t.Fatal(err)
	}
	assertCode(t, bookingRequestFixture(f, "PUT", "/bookings/"+b.ID+"/patient", owner, key, body), 200, "")
}
