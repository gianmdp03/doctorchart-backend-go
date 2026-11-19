package clinics

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
)

func TestActualBillingDoctorChartPlanWebhook(t *testing.T) {
	address := os.Getenv("DOCTORCHART_EFFECTIVE_BILLING_TEST_URL")
	key := os.Getenv("DOCTORCHART_EFFECTIVE_BILLING_TEST_SECRET")
	admin := os.Getenv("DOCTORCHART_PLAN_TEST_ADMIN_TOKEN")
	if address == "" || key == "" || admin == "" {
		t.Skip("run through Billing with owned loopback databases")
	}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		t.Fatal("owned loopback Billing fixture required")
	}
	f := newFixture(t)
	rights := billing.NewEntitlements(f.runtime, address, key).WithPlanPolicy()
	rules := billing.New(address)
	rules.SetInvalidator(rights.Invalidate)
	if err := rules.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rules.Int("BASIC", "max_patients", 0) != 300 || rules.Int("PRO", "max_patients", 0) != 1000 ||
		rules.Int("UNLIMITED", "max_patients", 0) != -1 {
		t.Fatal("wrong default patient quotas")
	}
	mux := http.NewServeMux()
	api.BaseRoutes(mux, rules, key)
	NewWithBookings(f.runtime, rights, true, true).WithStaff(true, "https://doctorchart.example.invalid").Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	server := httptest.NewServer(f.handler)
	defer server.Close()
	const callbackPath = "/internal/webhooks/billing/limits-updated"
	client := &http.Client{Timeout: 4 * time.Second}
	do := func(method, endpoint, body, token string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, endpoint, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			raw, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
			t.Fatalf("%s status %d want %d: %s", method, res.StatusCode, want, raw)
		}
	}
	do("POST", server.URL+callbackPath, "", "", 401)
	do("POST", address+"/__fixture/callback", jsonString(server.URL+callbackPath), "", 204)
	assertCode(t, f.request("POST", "/clinics", other, createBody), 403, "APPOINTMENTS_DISABLED")
	physical := scheduleClinicFixture(t, f)
	assertCode(t, f.request("PATCH", "/clinics/"+physical.ID, owner, `{"expectedVersion":"0","timezone":"UTC"}`), 200, "")
	virtual := virtualClinic(t, f)
	day := time.Now().UTC().AddDate(0, 0, 2)
	start := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, time.UTC)
	weekday := (int(start.Weekday())+6)%7 + 1
	assertCode(t, f.request("PUT", "/clinics/"+physical.ID+"/schedule", owner, scheduleBody("0", weekday, "09:00", "12:00")), 200, "")
	contextID := contextID(t, f, owner)
	virtualHours := fmt.Sprintf(`{"expectedVersion":"0","minNoticeMinutes":0,"maxAdvanceDays":30,"weekly":[{"weekday":%d,"ranges":[{"start":"13:00","end":"15:00","specialtyContextId":%q,"slotMinutes":30}]}],"exceptions":[]}`, weekday, contextID)
	assertCode(t, f.request("PUT", "/clinics/"+virtual.ID+"/schedule", owner, virtualHours), 200, "")
	booking := func(clinicID string, at time.Time) *httptest.ResponseRecorder {
		return bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(clinicID, at))
	}
	first := booking(physical.ID, start)
	assertCode(t, first, 201, "")
	firstBooking := booked(t, first)
	invite := func(number int) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"name":"Secretaría","email":"assistant%d@example.invalid","clinicIds":[%q],"permissions":["view"]}`, number, physical.ID)
		return f.request("POST", "/secretaries/invitations", owner, body)
	}
	for number := 1; number <= 3; number++ {
		assertCode(t, invite(number), 201, "")
	}
	assertCode(t, invite(4), 409, "SECRETARY_LIMIT_REACHED")

	generation := func() int64 {
		t.Helper()
		var value int64
		if err := f.pool.QueryRow(t.Context(), "SELECT generation FROM clinic_entitlement_epoch WHERE id=1").Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	edit := func(body string, patients int) {
		t.Helper()
		old := generation()
		do("PUT", address+"/api/billing/limits/DOCTORCHART/PRO", body, admin, 200)
		deadline := time.Now().Add(10 * time.Second)
		for generation() <= old || rules.Int("PRO", "max_patients", 0) != patients {
			if time.Now().After(deadline) {
				t.Fatal("Billing webhook did not refresh the live rules and invalidate the cached rights")
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	edit(`{"max_appointments":1,"teleconsultation_enabled":false,"max_patients":901}`, 901)
	assertCode(t, booking(physical.ID, start.Add(time.Hour)), 409, "APPOINTMENT_LIMIT_REACHED")
	assertCode(t, booking(virtual.ID, start.Add(4*time.Hour)), 403, "TELECONSULTATION_DISABLED")
	assertCode(t, f.request("PUT", "/teleconsultations/"+contextID, owner, `{"enabled":true}`), 403, "TELECONSULTATION_DISABLED")
	cancel := bookingRequestFixture(f, "PATCH", "/bookings/"+firstBooking.ID+"/status", owner, newBookingKey(), `{"expectedVersion":"1","status":"CANCELLED"}`)
	assertCode(t, cancel, 200, "")
	outcomes := make(chan *httptest.ResponseRecorder, 2)
	go func() { outcomes <- booking(physical.ID, start.Add(time.Hour)) }()
	go func() { outcomes <- booking(physical.ID, start.Add(90*time.Minute)) }()
	accepted, rejected := 0, 0
	for range 2 {
		outcome := <-outcomes
		if outcome.Code == 201 {
			accepted++
		} else {
			assertCode(t, outcome, 409, "APPOINTMENT_LIMIT_REACHED")
			rejected++
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("concurrent admissions across the last available spot: %d accepted, %d rejected", accepted, rejected)
	}
	edit(`{"max_appointments":-1,"teleconsultation_enabled":true,"max_patients":1000}`, 1000)
	assertCode(t, booking(virtual.ID, start.Add(4*time.Hour)), 201, "")
	assertCode(t, booking(physical.ID, start.Add(2*time.Hour)), 201, "")
	b, _, _ := bookingCounts(t, f)
	if b != 4 {
		t.Fatalf("changing limits lost appointments: %d", b)
	}
	t.Log("Básico agenda denegada, tres secretarías Pro, webhook actualiza pacientes y revoca teleconsulta, cupo concurrente, reapertura y conservación de turnos")
}
