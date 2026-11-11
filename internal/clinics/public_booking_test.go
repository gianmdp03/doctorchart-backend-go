package clinics

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func publicFixture(t *testing.T) (*fixture, Clinic, time.Time, Publication) {
	f, c, start := bookingFixture(t)
	f.online.Store(true)
	mux := http.NewServeMux()
	NewWithBookings(f.pool, f.rights, true, true).WithPublicBookings(true, "gateway-fixture").Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	p := Publication{ExpectedVersion: "0", Name: "Profesional público", Description: "Atención profesional", Published: true, Clinics: []PublishedClinic{{c.ID, true, "Dirección autorizada"}}, Questions: []PublicQuestion{{"insurance", "Obra social (sin información de salud)", true}}}
	w := f.request("PUT", "/booking-publication", owner, jsonString(p))
	assertCode(t, w, 200, "")
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return f, c, start, p
}
func jsonString(value any) string { b, _ := json.Marshal(value); return string(b) }
func pubRequest(f *fixture, method, path, key string, body any) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/doctorchart"+path, strings.NewReader(jsonString(body)))
	r.Header.Set("X-Gateway-Secret", "gateway-fixture")
	r.Header.Set("X-Public-Client", strings.Repeat("a", 64))
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func publicBody(t *testing.T, f *fixture, p Publication, start time.Time) PublicBookingRequest {
	t.Helper()
	w := pubRequest(f, "GET", "/public/appointments/doctors/"+p.PublicID, "", nil)
	assertCode(t, w, 200, "")
	var doctor PublicDoctor
	json.Unmarshal(w.Body.Bytes(), &doctor)
	challenge := pubRequest(f, "POST", "/public/appointments/challenge", "", map[string]any{})
	assertCode(t, challenge, 200, "")
	var proof struct{ Token string }
	json.Unmarshal(challenge.Body.Bytes(), &proof)
	solution := ""
	for n := 0; ; n++ {
		v := strconv.Itoa(n)
		hash := sha256.Sum256([]byte(proof.Token + "." + v))
		if hash[0] == 0 && hash[1] == 0 && hash[2]&192 == 0 {
			solution = v
			break
		}
	}
	return PublicBookingRequest{ClinicID: doctor.Clinics[0].ID, Start: start.Format(time.RFC3339), FormVersion: p.Version, ContactName: "Solicitante", ContactPhone: "+54 223 1234567", AttendeeName: "Persona atendida", DocumentCountry: "AR", DocumentType: "DNI", Document: "12345678", Email: "synthetic@example.invalid", Answers: map[string]string{"insurance": "Cobertura sintética"}, Challenge: proof.Token, Solution: solution}
}
func TestPublicBookingNoLoginSharedEngineAndSnapshots(t *testing.T) {
	f, c, start, p := publicFixture(t)
	path := "/public/appointments/doctors/" + p.PublicID + "/bookings"
	body := publicBody(t, f, p, start)
	key := newOpaqueID()
	w := pubRequest(f, "POST", path, key, body)
	assertCode(t, w, 201, "")
	text := w.Body.String()
	for _, forbidden := range []string{"bookingId", "patientId", "workspace", "clinicId", "document", "email", "answers", "12345678", owner} {
		if strings.Contains(text, forbidden) {
			t.Fatal("public confirmation leaks private data", text)
		}
	}
	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if !opaqueID.MatchString(response["reference"].(string)) {
		t.Fatal(response)
	}
	body.Challenge = "expired-retry"
	body.Solution = "0"
	again := pubRequest(f, "POST", path, key, body)
	assertCode(t, again, 201, "")
	if again.Body.String() != text {
		t.Fatal("replay changed confirmation")
	}
	body.ContactName = "Changed"
	assertCode(t, pubRequest(f, "POST", path, key, body), 409, "IDEMPOTENCY_CONFLICT")
	list := f.request("GET", "/bookings?from="+start.Add(-time.Hour).Format(time.RFC3339)+"&to="+start.Add(time.Hour).Format(time.RFC3339), owner, "")
	assertCode(t, list, 200, "")
	var page struct{ Items []Booking }
	json.Unmarshal(list.Body.Bytes(), &page)
	if len(page.Items) != 1 || page.Items[0].PatientID != nil {
		t.Fatal(list.Body.String())
	}
	b := page.Items[0]
	if b.ContextID == "" || b.Specialty.Name == "" || b.Provenance.Kind != "SCHEDULE_BLOCK" || response["specialtyName"] != b.Specialty.Name {
		t.Fatal("public booking lost persisted specialty", b, response)
	}
	details := f.request("GET", "/bookings/"+b.ID+"/public-details", owner, "")
	assertCode(t, details, 200, "")
	assertCode(t, f.request("GET", "/bookings/"+b.ID+"/public-details", other, ""), 404, "BOOKING_NOT_FOUND")
	p.ExpectedVersion = p.Version
	p.Questions[0].Label = "Etiqueta nueva"
	w = f.request("PUT", "/booking-publication", owner, jsonString(p))
	assertCode(t, w, 200, "")
	freshDetails := f.request("GET", "/bookings/"+b.ID+"/public-details", owner, "")
	assertCode(t, freshDetails, 200, "")
	if freshDetails.Body.String() != details.Body.String() {
		t.Fatal("stored snapshot changed with publication")
	}
	if strings.Contains(details.Body.String(), "Etiqueta nueva") {
		t.Fatal("snapshot mutated")
	}
	av := f.request("GET", "/clinics/"+c.ID+"/availability?from="+start.Format("2006-01-02")+"&days=1", owner, "")
	if strings.Contains(av.Body.String(), start.Format(time.RFC3339)) {
		t.Fatal("public booking not reflected in M3")
	}
	var patients int
	f.pool.QueryRow(t.Context(), "SELECT count(*) FROM patients").Scan(&patients)
	if patients != 0 {
		t.Fatal("created clinical patient")
	}
	var actor string
	f.pool.QueryRow(t.Context(), "SELECT actor_id FROM booking_events WHERE booking_id=$1", b.ID).Scan(&actor)
	if actor != "PUBLIC_BOOKING" {
		t.Fatal("simulated doctor identity", actor)
	}
}
func TestPublicConcurrentVisitorsAndManualReservation(t *testing.T) {
	f, c, start, p := publicFixture(t)
	body := publicBody(t, f, p, start)
	path := "/public/appointments/doctors/" + p.PublicID + "/bookings"
	done := make(chan int, 2)
	go func() { done <- pubRequest(f, "POST", path, newOpaqueID(), body).Code }()
	go func() {
		done <- bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start)).Code
	}()
	a, b := <-done, <-done
	if !((a == 201 && b == 409) || (a == 409 && b == 201)) {
		t.Fatal("engines disagree", a, b)
	}
}
func TestPublicValidationCommercialDenialAndRateLimits(t *testing.T) {
	f, _, start, p := publicFixture(t)
	body := publicBody(t, f, p, start)
	path := "/public/appointments/doctors/" + p.PublicID + "/bookings"
	bad := body
	bad.Document = "1"
	assertCode(t, pubRequest(f, "POST", path, newOpaqueID(), bad), 400, "INVALID_REQUEST")
	bad = body
	bad.FormVersion = "999"
	assertCode(t, pubRequest(f, "POST", path, newOpaqueID(), bad), 409, "FORM_VERSION_CONFLICT")
	bad = body
	bad.Solution = "-1"
	assertCode(t, pubRequest(f, "POST", path, newOpaqueID(), bad), 403, "")
	f.online.Store(false)
	if err := f.rights.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCode(t, pubRequest(f, "POST", path, newOpaqueID(), body), 403, "ONLINE_BOOKING_DISABLED")
	p.ExpectedVersion = p.Version
	p.Published = false
	assertCode(t, f.request("PUT", "/booking-publication", owner, jsonString(p)), 200, "")
	assertCode(t, pubRequest(f, "GET", "/public/appointments/doctors/"+p.PublicID, "", nil), 404, "PUBLIC_PROFILE_NOT_FOUND")
	for i := 0; i < 121; i++ {
		w := pubRequest(f, "GET", "/public/appointments/doctors", "", nil)
		if i == 120 {
			assertCode(t, w, 429, "PUBLIC_RATE_LIMITED")
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("missing retry")
			}
		}
	}
}
func TestPublicDirectoryFiltersAndNoAdministrativeIdentity(t *testing.T) {
	f, _, _, p := publicFixture(t)
	for _, query := range []string{"?country=Argentina&province=Buenos&city=Mar&name=Profesional", "?name=NoMatch"} {
		w := pubRequest(f, "GET", "/public/appointments/doctors"+query, "", nil)
		assertCode(t, w, 200, "")
		if strings.Contains(query, "NoMatch") == strings.Contains(w.Body.String(), p.PublicID) {
			t.Fatal(query, w.Body.String())
		}
	}
	for _, path := range []string{"/patients", "/bookings", "/booking-publication"} {
		r := httptest.NewRequest("GET", "/api/doctorchart"+path, nil)
		r.Header.Set("X-Gateway-Secret", "gateway-fixture")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		assertCode(t, w, 401, "UNAUTHENTICATED")
	}
	w := pubRequest(f, "GET", "/public/appointments/doctors?ownerId="+owner, "", nil)
	assertCode(t, w, 400, "INVALID_REQUEST")
	w = f.request("PUT", "/booking-publication", other, fmt.Sprintf(`{"expectedVersion":"0","name":"Other","description":"","published":true,"clinics":[{"clinicId":%q,"enabled":true,"address":"public"}],"questions":[]}`, p.Clinics[0].ClinicID))
	assertCode(t, w, 404, "CLINIC_NOT_FOUND")
}
