package clinics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

const staffTestPassword = "A-test-password-29!"
const staffTestOrigin = "https://doctorchart.test"

type staffTestClient struct {
	cookie         *http.Cookie
	csrf, recovery string
}

func staffFixture(t *testing.T) (*fixture, Clinic, time.Time) {
	f, c, start := bookingFixture(t)
	f.staff.Store(true)
	mux := http.NewServeMux()
	NewWithBookings(f.pool, f.rights, true, true).WithStaff(true, staffTestOrigin).Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	return f, c, start
}
func staffReq(f *fixture, client staffTestClient, method, path string, body any, keys ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/doctorchart/staff"+path, strings.NewReader(jsonString(body)))
	r.Header.Set("Origin", staffTestOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Gateway-Secret", "gateway-fixture")
	r.Header.Set("X-Public-Client", strings.Repeat("b", 64))
	key := newBookingKey()
	if len(keys) > 0 {
		key = keys[0]
	}
	r.Header.Set("Idempotency-Key", key)
	if client.cookie != nil {
		r.AddCookie(client.cookie)
		r.Header.Set("X-CSRF-Token", client.csrf)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func staffInvite(t *testing.T, f *fixture, u string, c Clinic, permissions []string) (string, string) {
	t.Helper()
	w := f.request("POST", "/secretaries/invitations", u, jsonString(map[string]any{"email": "assistant@example.invalid", "name": "Secretaria ensayo", "clinicIds": []string{c.ID}, "permissions": permissions}))
	assertCode(t, w, 201, "")
	var result struct {
		ID             string
		InvitationCode string
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	return result.ID, result.InvitationCode
}
func staffActivateTest(t *testing.T, f *fixture, code string) staffTestClient {
	t.Helper()
	w := staffReq(f, staffTestClient{}, "POST", "/activate", map[string]string{"email": "assistant@example.invalid", "password": staffTestPassword, "invitationCode": code})
	assertCode(t, w, 200, "")
	var body struct {
		CSRFToken    string
		RecoveryCode string
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing cookie")
	}
	return staffTestClient{cookie: cookies[0], csrf: body.CSRFToken, recovery: body.RecoveryCode}
}
func TestStaffActivationSessionsRecoveryAndCSRF(t *testing.T) {
	f, c, _ := staffFixture(t)
	_, code := staffInvite(t, f, owner, c, []string{"view", "create"})
	client := staffActivateTest(t, f, code)
	if !client.cookie.Secure || !client.cookie.HttpOnly || client.cookie.SameSite != http.SameSiteStrictMode || client.cookie.Path != staffBase || len(client.recovery) != 43 {
		t.Fatal("unsafe session or recovery")
	}
	assertCode(t, staffReq(f, client, "GET", "/session", nil), 200, "")
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/activate", map[string]string{"email": "assistant@example.invalid", "password": staffTestPassword, "invitationCode": code}), 401, "INVALID_CREDENTIALS")
	tampered := client
	copyCookie := *client.cookie
	copyCookie.Value = staffToken()
	tampered.cookie = &copyCookie
	assertCode(t, staffReq(f, tampered, "GET", "/session", nil), 401, "STAFF_UNAUTHENTICATED")
	noCSRF := client
	noCSRF.csrf = ""
	assertCode(t, staffReq(f, noCSRF, "POST", "/logout", map[string]string{}), 403, "CSRF_REJECTED")
	r := httptest.NewRequest("POST", staffBase+"/logout", strings.NewReader("{}"))
	r.Header.Set("Origin", "https://evil.invalid")
	r.Header.Set("X-Gateway-Secret", "gateway-fixture")
	r.AddCookie(client.cookie)
	r.Header.Set("X-CSRF-Token", client.csrf)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	assertCode(t, w, 403, "CSRF_REJECTED")
	var passwordHash, tokenHash, recoveryHash string
	if err := f.pool.QueryRow(t.Context(), "SELECT a.password_hash,a.recovery_hash,t.token_hash FROM staff_accounts a JOIN staff_sessions t ON t.account_id=a.id").Scan(&passwordHash, &recoveryHash, &tokenHash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(passwordHash, staffTestPassword) || tokenHash == client.cookie.Value || recoveryHash == client.recovery || !strings.HasPrefix(passwordHash, "$2a$12$") {
		t.Fatal("secrets not protected")
	}
	reset := map[string]string{"email": "assistant@example.invalid", "recoveryCode": client.recovery, "password": "Changed-password-42!"}
	w = staffReq(f, staffTestClient{}, "POST", "/recover", reset)
	assertCode(t, w, 200, "")
	var result struct{ RecoveryCode string }
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.RecoveryCode == client.recovery || len(result.RecoveryCode) != 43 {
		t.Fatal("recovery not rotated")
	}
	assertCode(t, staffReq(f, client, "GET", "/session", nil), 401, "STAFF_UNAUTHENTICATED")
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/recover", reset), 401, "INVALID_CREDENTIALS")
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/login", map[string]string{"email": "assistant@example.invalid", "password": staffTestPassword}), 401, "INVALID_CREDENTIALS")
	w = staffReq(f, staffTestClient{}, "POST", "/login", map[string]string{"email": "assistant@example.invalid", "password": "Changed-password-42!"})
	assertCode(t, w, 200, "")
	var session staffSession
	json.Unmarshal(w.Body.Bytes(), &session)
	client = staffTestClient{cookie: w.Result().Cookies()[0], csrf: session.CSRF}
	assertCode(t, staffReq(f, client, "POST", "/logout", map[string]string{}), 204, "")
	assertCode(t, staffReq(f, client, "GET", "/session", nil), 401, "STAFF_UNAUTHENTICATED")
}
func TestStaffDelegationUsesM4AndNeverExposesPatientIDs(t *testing.T) {
	f, c, start := staffFixture(t)
	access, code := staffInvite(t, f, owner, c, []string{"view", "create", "cancel", "reschedule", "status"})
	client := staffActivateTest(t, f, code)
	base := "/owners/" + owner
	body := map[string]any{"clinicId": c.ID, "start": start.Format(time.RFC3339), "contactName": "Solicitante", "attendeeName": "Atendido"}
	w := staffReq(f, client, "POST", base+"/bookings", body)
	assertCode(t, w, 201, "")
	b := booked(t, w)
	if strings.Contains(w.Body.String(), "patientId") {
		t.Fatal("clinical ID exposed")
	}
	assertCode(t, staffReq(f, client, "POST", base+"/bookings", body), 409, "BOOKING_SLOT_UNAVAILABLE")
	w = staffReq(f, client, "POST", base+"/bookings/"+b.ID+"/reschedule", map[string]string{"expectedVersion": b.Version, "clinicId": c.ID, "start": start.Add(30 * time.Minute).Format(time.RFC3339)})
	assertCode(t, w, 200, "")
	b = booked(t, w)
	w = staffReq(f, client, "PATCH", base+"/bookings/"+b.ID+"/status", map[string]string{"expectedVersion": b.Version, "status": "CANCELLED"})
	assertCode(t, w, 200, "")
	w = staffReq(f, client, "GET", base+"/clinics/"+c.ID+"/bookings?from="+start.Add(-time.Hour).Format(time.RFC3339)+"&to="+start.Add(time.Hour*24).Format(time.RFC3339), nil)
	assertCode(t, w, 200, "")
	if strings.Contains(w.Body.String(), "patientId") {
		t.Fatal("clinical ID in list")
	}
	body["patientId"] = "1"
	assertCode(t, staffReq(f, client, "POST", base+"/bookings", body), 403, "STAFF_FORBIDDEN")
	var actor string
	if err := f.pool.QueryRow(t.Context(), "SELECT actor_id FROM booking_events WHERE booking_id=$1 LIMIT 1", b.ID).Scan(&actor); err != nil || !strings.HasPrefix(actor, "STAFF:") {
		t.Fatal("wrong actor", err)
	}
	assertCode(t, f.request("DELETE", "/secretaries/"+access, owner, ""), 204, "")
	assertCode(t, staffReq(f, client, "GET", base+"/clinics/"+c.ID+"/bookings?from="+start.Format(time.RFC3339)+"&to="+start.Add(time.Hour).Format(time.RFC3339), nil), 403, "STAFF_FORBIDDEN")
}
func TestStaffMultipleOwnersIndependentPermissionsAndClinicalBoundary(t *testing.T) {
	f, c, start := staffFixture(t)
	_, code := staffInvite(t, f, owner, c, []string{"view"})
	client := staffActivateTest(t, f, code)
	otherClinic := clinic(t, f.request("POST", "/clinics", other, createBody))
	_, otherCode := staffInvite(t, f, other, otherClinic, []string{"view", "create"})
	w := staffReq(f, staffTestClient{}, "POST", "/activate", map[string]string{"email": "assistant@example.invalid", "password": "Not-current-password-20", "invitationCode": otherCode})
	assertCode(t, w, 401, "INVALID_CREDENTIALS")
	_ = staffActivateTest(t, f, otherCode)
	w = staffReq(f, client, "GET", "/assignments", nil)
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), owner) || !strings.Contains(w.Body.String(), other) {
		t.Fatal("missing independent owners")
	}
	assertCode(t, staffReq(f, client, "POST", "/owners/"+owner+"/bookings", map[string]string{"clinicId": c.ID, "start": start.Format(time.RFC3339), "contactName": "Name"}), 403, "STAFF_FORBIDDEN")
	assertCode(t, staffReq(f, client, "GET", "/owners/"+owner+"/clinics/"+otherClinic.ID+"/availability?from="+start.Format("2006-01-02")+"&days=1", nil), 404, "CLINIC_NOT_FOUND")
	for _, path := range []string{"/patients", "/prescriptions", "/studies", "/encounters", "/workspace"} {
		r := httptest.NewRequest("GET", "/api/doctorchart"+path, nil)
		r.Header.Set("X-Gateway-Secret", "gateway-fixture")
		r.AddCookie(client.cookie)
		w = httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		assertCode(t, w, 401, "UNAUTHENTICATED")
	}
	// Browser-supplied medical headers on a staff route do not choose the owner.
	r := httptest.NewRequest("GET", staffBase+"/assignments", nil)
	r.Header.Set("X-Gateway-Secret", "gateway-fixture")
	r.Header.Set("X-User-Id", owner)
	r.Header.Set("X-User-Role", "SUPER_ADMIN")
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	assertCode(t, w, 401, "STAFF_UNAUTHENTICATED")
}
func TestStaffConcurrentLastSlotCommercialExpiryAndRevocation(t *testing.T) {
	f, c, start := staffFixture(t)
	_, code := staffInvite(t, f, owner, c, []string{"view", "create"})
	client := staffActivateTest(t, f, code)
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- staffReq(f, client, "POST", "/owners/"+owner+"/bookings", map[string]string{"clinicId": c.ID, "start": start.Format(time.RFC3339), "contactName": "Concurrent"}).Code
		}()
	}
	wg.Wait()
	close(results)
	sum := 0
	for status := range results {
		sum += status
	}
	if sum != 201+409 {
		t.Fatal("concurrency", sum)
	}
	if err := f.rights.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.staff.Store(false)
	assertCode(t, staffReq(f, client, "POST", "/owners/"+owner+"/bookings", map[string]string{"clinicId": c.ID, "start": start.Add(time.Hour).Format(time.RFC3339), "contactName": "Denied"}), 403, "SECRETARY_MANAGEMENT_DISABLED")
	if _, err := f.pool.Exec(t.Context(), "UPDATE staff_sessions SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	assertCode(t, staffReq(f, client, "GET", "/session", nil), 401, "STAFF_UNAUTHENTICATED")
}
func TestStaffLoginRateLimitAndInvalidPermissions(t *testing.T) {
	f, c, _ := staffFixture(t)
	for i := 0; i < 10; i++ {
		assertCode(t, staffReq(f, staffTestClient{}, "POST", "/login", map[string]string{"email": "missing@example.invalid", "password": "invalid-password"}), 401, "INVALID_CREDENTIALS")
	}
	w := staffReq(f, staffTestClient{}, "POST", "/login", map[string]string{"email": "missing@example.invalid", "password": "invalid-password"})
	assertCode(t, w, 429, "STAFF_RATE_LIMITED")
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing retry header")
	}
	for _, p := range [][]string{{"create"}, {"view", "clinical"}, {"view", "view"}, {"view", "schedules"}} {
		w = f.request("POST", "/secretaries/invitations", owner, fmt.Sprintf(`{"email":"s@example.invalid","name":"Name","clinicIds":[%q],"permissions":%s}`, c.ID, jsonString(p)))
		assertCode(t, w, 400, "INVALID_REQUEST")
	}
}

func TestStaffIdempotencyRevocationAndSessionPasswordChange(t *testing.T) {
	f, c, start := staffFixture(t)
	access, code := staffInvite(t, f, owner, c, []string{"view", "create"})
	client := staffActivateTest(t, f, code)
	body := map[string]string{"clinicId": c.ID, "start": start.Format(time.RFC3339), "contactName": "Retry"}
	key := newBookingKey()
	path := "/owners/" + owner + "/bookings"
	first := staffReq(f, client, "POST", path, body, key)
	assertCode(t, first, 201, "")
	second := staffReq(f, client, "POST", path, body, key)
	assertCode(t, second, 201, "")
	if first.Body.String() != second.Body.String() {
		t.Fatal("retry changed response")
	}
	body["contactName"] = "Different"
	assertCode(t, staffReq(f, client, "POST", path, body, key), 409, "IDEMPOTENCY_CONFLICT")
	login := staffReq(f, staffTestClient{}, "POST", "/login", map[string]string{"email": "assistant@example.invalid", "password": staffTestPassword})
	assertCode(t, login, 200, "")
	var session staffSession
	json.Unmarshal(login.Body.Bytes(), &session)
	otherSession := staffTestClient{cookie: login.Result().Cookies()[0], csrf: session.CSRF}
	assertCode(t, staffReq(f, client, "POST", "/password", map[string]string{"currentPassword": staffTestPassword, "password": "New-secret-password-2026"}), 204, "")
	assertCode(t, staffReq(f, otherSession, "GET", "/session", nil), 401, "STAFF_UNAUTHENTICATED")
	assertCode(t, staffReq(f, client, "GET", "/session", nil), 200, "")
	assertCode(t, f.request("DELETE", "/secretaries/"+access, owner, ""), 204, "")
	body["contactName"] = "Retry"
	assertCode(t, staffReq(f, client, "POST", path, body, key), 403, "STAFF_FORBIDDEN")
}
func TestStaffExpiredInvitationAndBillingOutage(t *testing.T) {
	f, c, start := staffFixture(t)
	_, code := staffInvite(t, f, owner, c, []string{"view", "create"})
	if _, err := f.pool.Exec(t.Context(), "UPDATE staff_invitations SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/activate", map[string]string{"email": "assistant@example.invalid", "password": staffTestPassword, "invitationCode": code}), 401, "INVALID_CREDENTIALS")
	if _, err := f.pool.Exec(t.Context(), "UPDATE staff_invitations SET expires_at=now()+interval '1 hour'"); err != nil {
		t.Fatal(err)
	}
	client := staffActivateTest(t, f, code)
	// Invitation warmed a verifiable v3 lease. An outage alone cannot revoke it.
	f.mode.Store(1)
	assertCode(t, staffReq(f, client, "GET", "/assignments", nil), 200, "")
	if err := f.rights.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCode(t, staffReq(f, client, "POST", "/owners/"+owner+"/bookings", map[string]string{"clinicId": c.ID, "start": start.Format(time.RFC3339), "contactName": "No-default-grant"}), 503, "ENTITLEMENTS_UNAVAILABLE")
}
