package clinics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/mailclient"
	"github.com/syntah/doctorchart-backend-go/internal/notifications"
)

type testMail struct {
	mu       sync.Mutex
	messages []mailclient.Message
	fail     bool
}

func (m *testMail) Send(_ context.Context, message mailclient.Message) (mailclient.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return mailclient.Result{}, errors.New("synthetic mail service failure with secret that must not be persisted")
	}
	m.messages = append(m.messages, message)
	return mailclient.Result{ID: message.ID, Status: "accepted_by_provider"}, nil
}
func (m *testMail) count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.messages) }
func notificationFixture(t *testing.T) (*fixture, Clinic, time.Time, Publication, *notifications.Service, *testMail) {
	t.Helper()
	f, c, start, p := publicFixture(t)
	f.staff.Store(true)
	sender := &testMail{}
	n, err := notifications.NewWithSender(f.pool, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), sender)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewWithBookings(f.pool, f.rights, true, true).WithPublicBookings(true, "gateway-fixture").WithStaff(true, staffTestOrigin).WithNotifications(n).Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	return f, c, start, p, n, sender
}

type notificationReceipt struct {
	VerificationRequestID string
	Reference             string
}

func notificationBooking(t *testing.T, f *fixture, p Publication, start time.Time) (notificationReceipt, Booking) {
	t.Helper()
	w := pubRequest(f, "POST", "/public/appointments/doctors/"+p.PublicID+"/bookings", newOpaqueID(), publicBody(t, f, p, start))
	assertCode(t, w, 201, "")
	var receipt notificationReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if !opaqueID.MatchString(receipt.VerificationRequestID) {
		t.Fatal("missing immediate recovery code")
	}
	var id string
	if err := f.pool.QueryRow(t.Context(), "SELECT booking_id::text FROM booking_public_details WHERE reference=$1", receipt.Reference).Scan(&id); err != nil {
		t.Fatal(err)
	}
	b, err := ownedBooking(t.Context(), f.pool, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	return receipt, b
}
func processMail(t *testing.T, n *notifications.Service) {
	t.Helper()
	processed, err := n.ProcessOne(t.Context())
	if err != nil || !processed {
		t.Fatal("expected durable job", processed, err)
	}
}

var testPortalIdentity = portalIdentity{"AR", "DNI", "12345678", "synthetic@example.invalid"}

func portalCodeFromMail(t *testing.T, n *notifications.Service, sender *testMail) string {
	t.Helper()
	for range 5 {
		before := sender.count()
		processMail(t, n)
		if sender.count() == before {
			continue
		}
		for _, line := range strings.Split(sender.messages[len(sender.messages)-1].Text, "\n") {
			if portalCodePattern.MatchString(line) {
				return line
			}
		}
	}
	t.Fatal("verification email did not contain a six-digit code")
	return ""
}

func portalVerify(t *testing.T, f *fixture, receipt notificationReceipt, code string) *http.Cookie {
	t.Helper()
	draft := portalAccess{portalIdentity: testPortalIdentity, RequestID: receipt.VerificationRequestID}
	w := pubRequest(f, "POST", "/public/appointments/access/verify", "", map[string]string{
		"documentCountry": draft.DocumentCountry, "documentType": draft.DocumentType, "document": draft.Document,
		"email": draft.Email, "requestId": draft.RequestID, "code": code,
	})
	assertCode(t, w, 204, "")
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != portalCookie || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge != 172800 || w.Body.Len() != 0 {
		t.Fatal("expected a secure 48-hour session")
	}
	return cookies[0]
}

func TestManualBookingUsesTheSamePatientPortal(t *testing.T) {
	f, clinic, start, publication, notices, sender := notificationFixture(t)
	form := f.request("GET", "/bookings/form", owner, "")
	assertCode(t, form, 200, "")
	if !strings.Contains(form.Body.String(), "insurance") {
		t.Fatal("missing current administrative question")
	}
	details := bookingPortalIntake{
		Email: testPortalIdentity.Email, Document: testPortalIdentity.Document,
		FormVersion: publication.Version,
	}
	request := bookingRequest{
		ClinicID: clinic.ID, Start: start.Format(time.RFC3339),
		ContactName: "Solicitante manual", ContactPhone: "+54 223 4951234", Portal: &details,
	}
	send := func(key string) *httptest.ResponseRecorder {
		return bookingRequestFixture(f, "POST", "/bookings", owner, key, jsonString(map[string]any{
			"clinicId": request.ClinicID, "start": request.Start, "contactName": request.ContactName,
			"contactPhone": request.ContactPhone, "portal": details,
		}))
	}
	assertCode(t, send(newBookingKey()), 400, "INVALID_REQUEST")
	details.Answers = map[string]string{"insurance": "Cobertura sintética"}
	w := send(newBookingKey())
	assertCode(t, w, 201, "")
	booking := booked(t, w)
	var reference string
	if err := f.pool.QueryRow(t.Context(), "SELECT reference::text FROM booking_public_details WHERE booking_id=$1", booking.ID).Scan(&reference); err != nil {
		t.Fatal(err)
	}
	proof := publicBody(t, f, publication, start)
	w = pubRequest(f, "POST", "/public/appointments/access/request", "", map[string]string{
		"document": details.Document, "reference": reference, "challenge": proof.Challenge, "solution": proof.Solution,
	})
	assertCode(t, w, 202, "")
	var issued struct{ RequestID string }
	if err := json.Unmarshal(w.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	code := portalCodeFromMail(t, notices, sender)
	access := portalVerify(t, f, notificationReceipt{VerificationRequestID: issued.RequestID}, code)
	w = pubRequest(f, "POST", "/public/appointments/my-bookings", "", struct{}{}, access)
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), reference) || strings.Contains(w.Body.String(), details.Email) {
		t.Fatal("manual booking is missing from the private portal or exposed its email")
	}
}

func TestBookingPortalCodeAndListKeepPatientDataPrivate(t *testing.T) {
	f, _, start, publication, notices, sender := notificationFixture(t)
	receipt, booking := notificationBooking(t, f, publication, start)
	assertCode(t, pubRequest(f, "POST", "/public/appointments/my-bookings", "", struct{}{}), 403, "BOOKING_ACCESS_DENIED")
	code := portalCodeFromMail(t, notices, sender)
	emailBody := sender.messages[0].Text
	if strings.Contains(emailBody, "https:") || strings.Contains(emailBody, "12345678") || strings.Contains(emailBody, "Persona atendida") {
		t.Fatal("verification email contains a link or patient data")
	}
	diff := map[string]string{"documentCountry": "AR", "documentType": "DNI", "document": "12345677", "email": testPortalIdentity.Email,
		"requestId": receipt.VerificationRequestID, "code": code}
	assertCode(t, pubRequest(f, "POST", "/public/appointments/access/verify", "", diff), 403, "BOOKING_ACCESS_DENIED")
	access := portalVerify(t, f, receipt, code)
	diff["document"] = testPortalIdentity.Document
	assertCode(t, pubRequest(f, "POST", "/public/appointments/access/verify", "", diff), 403, "BOOKING_ACCESS_DENIED")
	f.mode.Store(1)
	w := pubRequest(f, "POST", "/public/appointments/my-bookings", "", struct{}{}, access)
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), receipt.Reference) || !strings.Contains(w.Body.String(), booking.Specialty.Name) {
		t.Fatal("missing booking in verified portal")
	}
	for _, secret := range []string{"patientId", "bookingId", "email", "document", "answers", "attendeeName", "contactName", owner, "Cobertura sintética"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("portal exposed private fields", secret)
		}
	}
	var hours float64
	if err := f.pool.QueryRow(t.Context(), "SELECT extract(epoch FROM session_expires_at-clock_timestamp())/3600 FROM booking_portal_codes").Scan(&hours); err != nil || hours < 47.9 || hours > 48 {
		t.Fatal("wrong server session lifetime", hours, err)
	}
	assertCode(t, pubRequest(f, "POST", "/public/appointments/my-bookings/logout", "", struct{}{}, access), 204, "")
	assertCode(t, pubRequest(f, "POST", "/public/appointments/my-bookings", "", struct{}{}, access), 403, "BOOKING_ACCESS_DENIED")
}

func TestBookingConfirmationAndReminderExplainHowToManageTheBooking(t *testing.T) {
	f, _, start, publication, notices, sender := notificationFixture(t)
	_, booking := notificationBooking(t, f, publication, start)
	portalCodeFromMail(t, notices, sender)
	processMail(t, notices)
	if _, err := f.pool.Exec(t.Context(), "UPDATE notification_outbox SET due_at=now()-interval '1 second' WHERE booking_id=$1 AND kind='BOOKING_REMINDER'", booking.ID); err != nil {
		t.Fatal(err)
	}
	processMail(t, notices)
	if sender.count() != 3 {
		t.Fatal("expected verification code, confirmation and reminder")
	}
	for _, message := range sender.messages {
		body := message.Text
		if !strings.Contains(body, "Mis turnos") || !strings.Contains(body, "DNI") || !strings.Contains(body, "48 horas") ||
			strings.Contains(body, "12345678") || strings.Contains(body, "Persona atendida") {
			t.Fatal("incorrect booking instructions or private information in email")
		}
	}
}
func TestNotificationMailServiceFailureRetryRestartAndLeaseRecovery(t *testing.T) {
	f, c, _, _, n, sender := notificationFixture(t)
	staffInvite(t, f, owner, c, []string{"view"})
	sender.fail = true
	processMail(t, n)
	var err error
	var state, last string
	var attempts int
	if err = f.pool.QueryRow(t.Context(), "SELECT state,last_code,attempts FROM notification_outbox WHERE kind='STAFF_INVITATION'").Scan(&state, &last, &attempts); err != nil || state != "PENDING" || last != "MAIL_SERVICE_UNAVAILABLE" || attempts != 1 {
		t.Fatal(state, last, attempts, err)
	}
	if _, err = f.pool.Exec(t.Context(), "UPDATE notification_outbox SET state='PROCESSING',lease_id=$1,lease_until=now()-interval '1 second' WHERE kind='STAFF_INVITATION'", newOpaqueID()); err != nil {
		t.Fatal(err)
	}
	sender.fail = false
	restarted, err := notifications.NewWithSender(f.pool, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), sender)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.ValidateKey(t.Context()); err != nil {
		t.Fatal(err)
	}
	processMail(t, restarted)
	if sender.count() != 1 {
		t.Fatal("restart did not deliver exactly one logical job")
	}
	wrong, err := notifications.NewWithSender(f.pool, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))), sender)
	if err != nil {
		t.Fatal(err)
	}
	if wrong.ValidateKey(t.Context()) == nil {
		t.Fatal("changed encryption key silently accepted")
	}
}
func TestVerifiedPortalReschedulesAndCancelsThroughBookingEngine(t *testing.T) {
	f, _, start, publication, notices, sender := notificationFixture(t)
	receipt, booking := notificationBooking(t, f, publication, start)
	access := portalVerify(t, f, receipt, portalCodeFromMail(t, notices, sender))
	const path = "/public/appointments/my-bookings/change"
	req := struct {
		Reference       string `json:"reference"`
		ExpectedVersion string `json:"expectedVersion"`
		Action          string `json:"action"`
		Start           string `json:"start,omitempty"`
	}{receipt.Reference, booking.Version, "reschedule", start.Add(time.Hour).Format(time.RFC3339)}
	forged := req
	forged.Reference = newOpaqueID()
	assertCode(t, pubRequest(f, "POST", path, newOpaqueID(), forged, access), 404, "BOOKING_NOT_FOUND")
	key := newOpaqueID()
	assertCode(t, pubRequest(f, "POST", path, key, req, access), 200, "")
	assertCode(t, pubRequest(f, "POST", path, key, req, access), 200, "")
	current, err := ownedBooking(t.Context(), f.pool, owner, booking.ID)
	if err != nil || !current.Start.Equal(start.Add(time.Hour)) || current.Version == booking.Version {
		t.Fatal("reschedule did not persist", current, err)
	}
	req.Action, req.Start, req.ExpectedVersion = "cancel", "", current.Version
	assertCode(t, pubRequest(f, "POST", path, newOpaqueID(), req, access), 200, "")
	var jobs, events int
	if err = f.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM notification_outbox WHERE kind IN ('BOOKING_CONFIRMED','BOOKING_RESCHEDULED','BOOKING_CANCELLED')),(SELECT count(*) FROM booking_events WHERE booking_id=$1 AND actor_id='PUBLIC_PATIENT')", booking.ID).Scan(&jobs, &events); err != nil || jobs != 3 || events != 2 {
		t.Fatal("expected three event emails and two audited changes", jobs, events, err)
	}
	w := pubRequest(f, "POST", "/public/appointments/my-bookings", "", struct{}{}, access)
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), "CANCELLED") {
		t.Fatal("portal shows an obsolete status", w.Body.String())
	}
}
func TestPublicConfirmationReplayKeepsSameCode(t *testing.T) {
	f, _, start, p, _, _ := notificationFixture(t)
	body := publicBody(t, f, p, start)
	key := newOpaqueID()
	path := "/public/appointments/doctors/" + p.PublicID + "/bookings"
	first := pubRequest(f, "POST", path, key, body)
	assertCode(t, first, 201, "")
	second := pubRequest(f, "POST", path, key, body)
	assertCode(t, second, 201, "")
	if first.Body.String() != second.Body.String() {
		t.Fatal("retry lost immediate access code")
	}
	var codes int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM booking_portal_codes").Scan(&codes); err != nil || codes != 1 {
		t.Fatal("replay duplicated confirmation codes", codes, err)
	}
}
func TestPublicConfirmationPersistenceFailureRollsBackBooking(t *testing.T) {
	f, _, start, p, _, _ := notificationFixture(t)
	if _, err := f.pool.Exec(t.Context(), "ALTER TABLE booking_portal_codes ADD CONSTRAINT synthetic_failure CHECK(false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	w := pubRequest(f, "POST", "/public/appointments/doctors/"+p.PublicID+"/bookings", newOpaqueID(), publicBody(t, f, p, start))
	assertCode(t, w, 500, "")
	var rows int
	f.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM bookings)+(SELECT count(*) FROM booking_events)+(SELECT count(*) FROM booking_portal_codes)").Scan(&rows)
	if rows != 0 {
		t.Fatal("partial confirmation committed despite database event failure")
	}
}
func TestNotificationStaffInvitationAndEmailRecovery(t *testing.T) {
	f, c, _, _, n, sender := notificationFixture(t)
	_, firstCode := staffInvite(t, f, owner, c, []string{"view"})
	_, code := staffInvite(t, f, owner, c, []string{"view"})
	processMail(t, n)
	processMail(t, n)
	if sender.count() != 1 {
		t.Fatal("only the most recent invitation should be delivered")
	}
	invitation := sender.messages[0].Text
	if !strings.Contains(invitation, staffTestOrigin+"/staff/activate#invite="+code) ||
		strings.Contains(invitation, firstCode) {
		t.Fatal("email did not contain the current private activation link")
	}
	client := staffActivateTest(t, f, code)
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/recovery-email", map[string]string{"email": "assistant@example.invalid"}), 202, "")
	processMail(t, n)
	// Only decrypt the captured test mailbox's MIME body, never query a plaintext token.
	var emailCode string
	for _, line := range strings.Split(sender.messages[1].Text, "\n") {
		if len(line) == 43 {
			emailCode = line
			break
		}
	}
	if emailCode == "" {
		t.Fatal("mailbox did not receive recovery code")
	}
	w := staffReq(f, staffTestClient{}, "POST", "/recover-email", map[string]string{"email": "assistant@example.invalid", "emailCode": emailCode, "password": "New-local-password-2026!"})
	assertCode(t, w, 200, "")
	assertCode(t, staffReq(f, client, "GET", "/session", nil), 401, "STAFF_UNAUTHENTICATED")
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/recover-email", map[string]string{"email": "assistant@example.invalid", "emailCode": emailCode, "password": "Another-password-2026!"}), 401, "INVALID_CREDENTIALS")
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/login", map[string]string{"email": "assistant@example.invalid", "password": "New-local-password-2026!"}), 200, "")
	for range 3 {
		assertCode(t, staffReq(f, staffTestClient{}, "POST", "/recovery-email", map[string]string{"email": "unknown@example.invalid"}), 202, "")
	}
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/recovery-email", map[string]string{"email": "unknown@example.invalid"}), 429, "STAFF_RATE_LIMITED")
}

func TestPortalCodeExpiresAndLocksAfterFiveGuesses(t *testing.T) {
	f, _, start, publication, notices, sender := notificationFixture(t)
	receipt, _ := notificationBooking(t, f, publication, start)
	code := portalCodeFromMail(t, notices, sender)
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	body := map[string]string{"documentCountry": "AR", "documentType": "DNI", "document": testPortalIdentity.Document,
		"email": testPortalIdentity.Email, "requestId": receipt.VerificationRequestID, "code": wrong}
	for range 5 {
		assertCode(t, pubRequest(f, "POST", "/public/appointments/access/verify", "", body), 403, "BOOKING_ACCESS_DENIED")
	}
	body["code"] = code
	assertCode(t, pubRequest(f, "POST", "/public/appointments/access/verify", "", body), 403, "BOOKING_ACCESS_DENIED")
	if _, err := f.pool.Exec(t.Context(), "UPDATE booking_portal_codes SET attempts=0, expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	assertCode(t, pubRequest(f, "POST", "/public/appointments/access/verify", "", body), 403, "BOOKING_ACCESS_DENIED")
}

func TestPortalRequestByDocumentAndEmailUsesGenericResponseAndInboxCooldown(t *testing.T) {
	f, _, start, publication, notices, sender := notificationFixture(t)
	notificationBooking(t, f, publication, start)
	portalCodeFromMail(t, notices, sender)
	if _, err := f.pool.Exec(t.Context(), "UPDATE booking_portal_codes SET created_at=now()-interval '2 minutes'"); err != nil {
		t.Fatal(err)
	}
	makeRequest := func(email string) notificationReceipt {
		t.Helper()
		proof := publicBody(t, f, publication, start)
		w := pubRequest(f, "POST", "/public/appointments/access/request", "", map[string]string{
			"documentCountry": "AR", "documentType": "DNI", "document": testPortalIdentity.Document,
			"email": email, "challenge": proof.Challenge, "solution": proof.Solution,
		})
		assertCode(t, w, 202, "")
		var response struct{ RequestID string }
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || !opaqueID.MatchString(response.RequestID) {
			t.Fatal("missing generic request ID", err)
		}
		return notificationReceipt{VerificationRequestID: response.RequestID}
	}
	makeRequest("different@example.invalid")
	draft := makeRequest(testPortalIdentity.Email)
	makeRequest(testPortalIdentity.Email)
	var jobs int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_outbox WHERE kind='BOOKING_PORTAL_CODE'").Scan(&jobs); err != nil || jobs != 2 {
		t.Fatal("unknown email or repeated request enqueued mail", jobs, err)
	}
	portalVerify(t, f, draft, portalCodeFromMail(t, notices, sender))
}

func TestNotificationLegacyBookingEmailsAndInvitationsNeverSend(t *testing.T) {
	f, c, start, p, n, sender := notificationFixture(t)
	_, b := notificationBooking(t, f, p, start)
	if _, err := f.pool.Exec(t.Context(), "UPDATE booking_portal_codes SET expires_at=now()-interval '1 second'; UPDATE notification_outbox SET state='OBSOLETE' WHERE booking_id IS NOT NULL"); err != nil {
		t.Fatal(err)
	}
	processMail(t, n)
	// Simulate an old reminder whose original delivery time is still in the future.
	if _, err := f.pool.Exec(t.Context(), "INSERT INTO notification_outbox(dedup_key,kind,booking_id,booking_version,payload,due_at) VALUES('legacy-booking','BOOKING_REMINDER',$1,$2,$3,now()+interval '1 day')", b.ID, b.Version, []byte("legacy payload")); err != nil {
		t.Fatal(err)
	}
	processMail(t, n)
	var state, code string
	if err := f.pool.QueryRow(t.Context(), "SELECT state,last_code FROM notification_outbox WHERE dedup_key='legacy-booking'").Scan(&state, &code); err != nil || state != "OBSOLETE" || code != "BOOKING_EMAIL_DISABLED" || sender.count() != 0 {
		t.Fatal("legacy booking email was not retired", state, code, err)
	}
	access, _ := staffInvite(t, f, owner, c, []string{"view"})
	assertCode(t, f.request("DELETE", "/secretaries/"+access, owner, ""), 204, "")
	if _, err := f.pool.Exec(t.Context(), "INSERT INTO notification_outbox(dedup_key,kind,access_id,payload,due_at) VALUES('legacy-invitation','STAFF_INVITATION',$1,$2,now())", access, []byte("legacy payload")); err != nil {
		t.Fatal(err)
	}
	processMail(t, n)
	processMail(t, n)
	if err := f.pool.QueryRow(t.Context(), "SELECT state FROM notification_outbox WHERE dedup_key='legacy-invitation'").Scan(&state); err != nil || state != "OBSOLETE" || sender.count() != 0 {
		t.Fatal("legacy invitation was not retired", state, err)
	}
}

func TestNotificationConcurrentEmailRecoveryTokensAreSingleUse(t *testing.T) {
	f, c, _, _, n, sender := notificationFixture(t)
	_, code := staffInvite(t, f, owner, c, []string{"view"})
	_ = staffActivateTest(t, f, code)
	processMail(t, n)
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/recovery-email", map[string]string{"email": "assistant@example.invalid"}), 202, "")
	processMail(t, n)
	emailCode := ""
	for _, line := range strings.Split(sender.messages[0].Text, "\n") {
		if len(line) == 43 {
			emailCode = line
			break
		}
	}
	if emailCode == "" {
		t.Fatal("no recovery code in mailbox")
	}
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			w := staffReq(f, staffTestClient{}, "POST", "/recover-email", map[string]string{"email": "assistant@example.invalid", "emailCode": emailCode, "password": "Concurrent-password-2026!"})
			results <- w.Code
		})
	}
	wg.Wait()
	close(results)
	sum := 0
	for status := range results {
		sum += status
	}
	if sum != 200+401 {
		t.Fatal("two successful uses of one recovery code", sum)
	}
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/recovery-email", map[string]string{"email": "assistant@example.invalid"}), 202, "")
	if _, err := f.pool.Exec(t.Context(), "UPDATE staff_email_recovery SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	processMail(t, n)
	if sender.count() != 1 {
		t.Fatal("expired recovery email delivered")
	}
}
