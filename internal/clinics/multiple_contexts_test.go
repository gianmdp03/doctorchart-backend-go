package clinics

import (
	"encoding/json"
	"fmt"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"net/http"
	"testing"
	"time"
)

func TestMultipleSpecialtySchedulesBookingsPublicOffersAndLinks(t *testing.T) {
	f, c, start := bookingFixture(t)
	first := contextID(t, f, owner)
	var second, pending string
	err := f.pool.QueryRow(t.Context(), `INSERT INTO doctor_specialty_contexts(workspace_id,specialty_id,version,current_step_key,onboarding_completed,definition,created_at,updated_at)
 SELECT w.id,s.id,0,'completed',true,d.published_definition,now(),now() FROM doctor_workspaces w CROSS JOIN specialties s JOIN wizard_defaults d ON d.specialty_id=s.id WHERE w.user_id=$1 AND s.id<>(SELECT specialty_id FROM doctor_specialty_contexts WHERE id=$2) ORDER BY s.id LIMIT 1 RETURNING id::text`, owner, first).Scan(&second)
	if err != nil {
		t.Fatal(err)
	}
	err = f.pool.QueryRow(t.Context(), `INSERT INTO doctor_specialty_contexts(workspace_id,specialty_id,version,current_step_key,onboarding_completed,draft_definition,created_at,updated_at)
 SELECT w.id,s.id,0,'specialty',false,d.published_definition,now(),now() FROM doctor_workspaces w CROSS JOIN specialties s JOIN wizard_defaults d ON d.specialty_id=s.id WHERE w.user_id=$1 AND NOT EXISTS(SELECT 1 FROM doctor_specialty_contexts x WHERE x.workspace_id=w.id AND x.specialty_id=s.id) RETURNING id::text`, owner).Scan(&pending)
	if err != nil {
		t.Fatal(err)
	}
	agenda := agendaDTO(t, f, c)
	original := agenda.Weekly[0].Ranges[0]
	agenda.Weekly[0].Ranges = []ScheduleRange{{ID: original.ID, ContextID: first, Start: "09:00", End: "10:00", SlotMinutes: 20}, {ContextID: second, Start: "10:00", End: "12:00", SlotMinutes: 30}}
	putBlocks(t, f, c, agenda, 200, "")
	saved := agendaDTO(t, f, c)
	invalid := saved
	invalid.Weekly = append([]ScheduleDay{}, saved.Weekly...)
	invalid.Weekly[0].Ranges = append([]ScheduleRange{}, saved.Weekly[0].Ranges...)
	invalid.Weekly[0].Ranges[1].ContextID = pending
	putBlocks(t, f, c, invalid, 404, "SPECIALTY_CONTEXT_NOT_FOUND")
	firstReply := bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start))
	assertCode(t, firstReply, 201, "")
	one := booked(t, firstReply)
	secondReply := bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start.Add(time.Hour)))
	assertCode(t, secondReply, 201, "")
	two := booked(t, secondReply)
	if one.ContextID != first || two.ContextID != second || one.Provenance.SlotMinutes != 20 || two.Provenance.SlotMinutes != 30 {
		t.Fatal("slot contexts/durations mixed", one, two)
	}
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings/"+one.ID+"/reschedule", owner, newBookingKey(), fmt.Sprintf(`{"expectedVersion":"1","clinicId":%q,"start":%q}`, c.ID, start.Add(90*time.Minute).Format(time.RFC3339))), 409, "BOOKING_CONTEXT_MISMATCH")
	assertCode(t, bookingRequestFixture(f, "POST", "/bookings/"+two.ID+"/reschedule", owner, newBookingKey(), fmt.Sprintf(`{"expectedVersion":"1","clinicId":%q,"start":%q}`, c.ID, start.Add(90*time.Minute).Format(time.RFC3339))), 200, "")
	var patient string
	if err = f.pool.QueryRow(t.Context(), `INSERT INTO patients(user_id,workspace_id,specialty_context_id,full_name,birth_date,form_definition_snapshot,form_values,formula_context,formula_results,updated_at,version,workspace_version) SELECT $1,w.id,$2,'Synthetic','1990-01-01','{}','{}','{}','{}',now(),0,0 FROM doctor_workspaces w WHERE w.user_id=$1::varchar(100) RETURNING id::text`, owner, second).Scan(&patient); err != nil {
		t.Fatal(err)
	}
	assertCode(t, bookingRequestFixture(f, "PUT", "/bookings/"+one.ID+"/patient", owner, newBookingKey(), fmt.Sprintf(`{"expectedVersion":"1","patientId":%q}`, patient)), 404, "PATIENT_NOT_FOUND")
	assertCode(t, bookingRequestFixture(f, "PUT", "/bookings/"+two.ID+"/patient", owner, newBookingKey(), fmt.Sprintf(`{"expectedVersion":"2","patientId":%q}`, patient)), 200, "")
	list := f.request("GET", "/bookings?from="+start.Add(-time.Hour).Format(time.RFC3339)+"&to="+start.Add(4*time.Hour).Format(time.RFC3339), owner, "")
	assertCode(t, list, 200, "")
	var bookings struct{ Items []Booking }
	json.Unmarshal(list.Body.Bytes(), &bookings)
	if len(bookings.Items) != 2 {
		t.Fatal("global calendar lost a specialty", list.Body)
	}
	f.online.Store(true)
	mux := http.NewServeMux()
	NewWithBookings(f.pool, f.rights, true, true).WithPublicBookings(true, "gateway-fixture").Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	publication := Publication{ExpectedVersion: "0", Name: "Multiple specialties", Published: true, ContextIDs: []string{first, second}, Clinics: []PublishedClinic{{c.ID, true, "Public address"}}, Questions: []PublicQuestion{}}
	reply := f.request("PUT", "/booking-publication", owner, jsonString(publication))
	assertCode(t, reply, 200, "")
	json.Unmarshal(reply.Body.Bytes(), &publication)
	profile := pubRequest(f, "GET", "/public/appointments/doctors/"+publication.PublicID, "", nil)
	assertCode(t, profile, 200, "")
	var doctor PublicDoctor
	json.Unmarshal(profile.Body.Bytes(), &doctor)
	if len(doctor.Specialties) != 2 || len(doctor.Clinics) != 1 || doctor.Specialty != "" {
		t.Fatal("public profile ambiguous", profile.Body)
	}
	path := "/public/appointments/doctors/" + publication.PublicID + "/clinics/" + doctor.Clinics[0].ID + "/availability?from=" + start.Format("2006-01-02") + "&days=1"
	assertCode(t, pubRequest(f, "GET", path, "", nil), 400, "SPECIALTY_CONTEXT_REQUIRED")
	offer := pubRequest(f, "GET", path+"&specialtyContextId="+second, "", nil)
	assertCode(t, offer, 200, "")
	var availability struct{ Days []AvailabilityDay }
	json.Unmarshal(offer.Body.Bytes(), &availability)
	for _, day := range availability.Days {
		for _, slot := range day.Slots {
			if slot.SpecialtyContextID != second {
				t.Fatal("public slots mixed")
			}
		}
	}
	assertCode(t, pubRequest(f, "GET", path+"&specialtyContextId="+pending, "", nil), 404, "")
	body := publicBody(t, f, publication, start.Add(2*time.Hour))
	body.ContextID = second
	body.Answers = map[string]string{}
	key := newOpaqueID()
	assertCode(t, pubRequest(f, "POST", "/public/appointments/doctors/"+publication.PublicID+"/bookings", key, body), 201, "")
	if err = f.pool.QueryRow(t.Context(), "SELECT specialty_context_id::text FROM bookings WHERE start_at=$1", start.Add(2*time.Hour)).Scan(&pending); err != nil || pending != second {
		t.Fatal("public reservation context", pending, err)
	}
	publication.ExpectedVersion = publication.Version
	publication.ContextIDs = []string{first}
	assertCode(t, f.request("PUT", "/booking-publication", owner, jsonString(publication)), 200, "")
	assertCode(t, pubRequest(f, "POST", "/public/appointments/doctors/"+publication.PublicID+"/bookings", key, body), 201, "")
}
