package clinics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/notifications"
)

type testMail struct {
	mu       sync.Mutex
	messages [][]byte
	fail     bool
}

func (m *testMail) Send(_ context.Context, _ string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("synthetic SMTP failure with secret that must not be persisted")
	}
	m.messages = append(m.messages, append([]byte(nil), body...))
	return nil
}
func (m *testMail) count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.messages) }
func notificationFixture(t *testing.T) (*fixture, Clinic, time.Time, Publication, *notifications.Service, *testMail) {
	t.Helper()
	f, c, start, p := publicFixture(t)
	f.staff.Store(true)
	sender := &testMail{}
	n, err := notifications.NewWithSender(f.pool, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "notices@example.invalid", 1440, sender)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewWithBookings(f.pool, f.rights, true, true).WithPublicBookings(true, "gateway-fixture").WithStaff(true, staffTestOrigin).WithNotifications(n).Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	return f, c, start, p, n, sender
}

type notificationReceipt struct {
	AccessCode      string
	AccessExpiresAt time.Time
	Reference       string
}

func notificationBooking(t *testing.T, f *fixture, p Publication, start time.Time) (notificationReceipt, Booking) {
	t.Helper()
	w := pubRequest(f, "POST", "/public/appointments/doctors/"+p.PublicID+"/bookings", newOpaqueID(), publicBody(t, f, p, start))
	assertCode(t, w, 201, "")
	var receipt notificationReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.AccessCode) != 43 {
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
func TestNotificationAtomicOutboxImmediateRecoveryAndPrivacy(t *testing.T) {
	f, _, start, p, n, sender := notificationFixture(t)
	receipt, b := notificationBooking(t, f, p, start)
	var count int
	if err := f.pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_outbox").Scan(&count); err != nil || count != 2 {
		t.Fatal("confirmation and scheduled reminder missing", count, err)
	}
	var payload []byte
	var hash string
	if err := f.pool.QueryRow(t.Context(), "SELECT o.payload,a.token_hash FROM notification_outbox o JOIN booking_confirmation_access a ON a.booking_id=o.booking_id LIMIT 1").Scan(&payload, &hash); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), receipt.AccessCode) || strings.Contains(string(payload), "synthetic@example.invalid") || hash == receipt.AccessCode {
		t.Fatal("sensitive delivery state is plaintext")
	}
	processMail(t, n)
	if sender.count() != 1 {
		t.Fatal("confirmation not delivered")
	}
	// Query existing administrative information without depending on live Billing.
	f.mode.Store(1)
	w := pubRequest(f, "POST", "/public/appointments/confirmation", "", map[string]string{"accessCode": receipt.AccessCode})
	assertCode(t, w, 200, "")
	for _, secret := range []string{"patientId", "bookingId", "clinicId", "email", "document", "answers", "attendeeName", "contactName", owner} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("recovery leaked private field", secret)
		}
	}
	assertCode(t, f.request("DELETE", "/bookings/"+b.ID+"/confirmation-access", other, ""), 404, "BOOKING_NOT_FOUND")
	assertCode(t, f.request("DELETE", "/bookings/"+b.ID+"/confirmation-access", owner, ""), 204, "")
	assertCode(t, pubRequest(f, "POST", "/public/appointments/confirmation", "", map[string]string{"accessCode": receipt.AccessCode}), 404, "CONFIRMATION_NOT_FOUND")
}
func TestNotificationSMTPFailureRetryRestartAndLeaseRecovery(t *testing.T) {
	f, _, start, p, n, sender := notificationFixture(t)
	_, b := notificationBooking(t, f, p, start)
	sender.fail = true
	processMail(t, n)
	current, err := ownedBooking(t.Context(), f.pool, owner, b.ID)
	if err != nil || current.Status != "CONFIRMED" {
		t.Fatal("SMTP changed booking", err)
	}
	var state, last string
	var attempts int
	if err = f.pool.QueryRow(t.Context(), "SELECT state,last_code,attempts FROM notification_outbox WHERE kind='BOOKING_CONFIRMED'").Scan(&state, &last, &attempts); err != nil || state != "PENDING" || last != "SMTP_UNAVAILABLE" || attempts != 1 {
		t.Fatal(state, last, attempts, err)
	}
	if _, err = f.pool.Exec(t.Context(), "UPDATE notification_outbox SET state='PROCESSING',lease_id=$1,lease_until=now()-interval '1 second' WHERE kind='BOOKING_CONFIRMED'", newOpaqueID()); err != nil {
		t.Fatal(err)
	}
	sender.fail = false
	restarted, err := notifications.NewWithSender(f.pool, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "notices@example.invalid", 1440, sender)
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
	wrong, err := notifications.NewWithSender(f.pool, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))), "notices@example.invalid", 1440, sender)
	if err != nil {
		t.Fatal(err)
	}
	if wrong.ValidateKey(t.Context()) == nil {
		t.Fatal("changed encryption key silently accepted")
	}
}
func TestNotificationVersionedReminderRescheduleCancellationAndExpiry(t *testing.T) {
	f, c, start, p, n, sender := notificationFixture(t)
	receipt, b := notificationBooking(t, f, p, start)
	w := bookingRequestFixture(f, "POST", "/bookings/"+b.ID+"/reschedule", owner, newBookingKey(), jsonString(map[string]string{"expectedVersion": b.Version, "clinicId": c.ID, "start": start.Add(time.Hour).Format(time.RFC3339)}))
	assertCode(t, w, 200, "")
	b = booked(t, w)
	// Old confirmation and reminder cannot deliver obsolete details.
	if _, err := f.pool.Exec(t.Context(), "UPDATE notification_outbox SET due_at=now()"); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		processMail(t, n)
	}
	if sender.count() != 2 {
		t.Fatal("expected only current reschedule and reminder", sender.count())
	}
	w = bookingRequestFixture(f, "PATCH", "/bookings/"+b.ID+"/status", owner, newBookingKey(), jsonString(map[string]string{"expectedVersion": b.Version, "status": "CANCELLED"}))
	assertCode(t, w, 200, "")
	processMail(t, n)
	if sender.count() != 3 {
		t.Fatal("cancellation not sent")
	}
	w = pubRequest(f, "POST", "/public/appointments/confirmation", "", map[string]string{"accessCode": receipt.AccessCode})
	assertCode(t, w, 200, "")
	if !strings.Contains(w.Body.String(), "CANCELLED") {
		t.Fatal("recovery did not reflect live administrative state")
	}
	if _, err := f.pool.Exec(t.Context(), "UPDATE booking_confirmation_access SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	assertCode(t, pubRequest(f, "POST", "/public/appointments/confirmation", "", map[string]string{"accessCode": receipt.AccessCode}), 404, "CONFIRMATION_NOT_FOUND")
}
func TestNotificationTwoWorkersAndPublicIdempotency(t *testing.T) {
	f, _, start, p, n, sender := notificationFixture(t)
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
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := n.ProcessOne(t.Context()); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if sender.count() != 1 {
		t.Fatal("workers duplicated confirmation", sender.count())
	}
	var jobs int
	f.pool.QueryRow(t.Context(), "SELECT count(*) FROM notification_outbox").Scan(&jobs)
	if jobs != 2 {
		t.Fatal("logical duplicates", jobs)
	}
}
func TestNotificationOutboxFailureRollsBackWithM4(t *testing.T) {
	f, _, start, p, _, _ := notificationFixture(t)
	if _, err := f.pool.Exec(t.Context(), "ALTER TABLE notification_outbox ADD CONSTRAINT synthetic_failure CHECK(false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	w := pubRequest(f, "POST", "/public/appointments/doctors/"+p.PublicID+"/bookings", newOpaqueID(), publicBody(t, f, p, start))
	assertCode(t, w, 500, "")
	var rows int
	f.pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM bookings)+(SELECT count(*) FROM booking_events)+(SELECT count(*) FROM booking_confirmation_access)").Scan(&rows)
	if rows != 0 {
		t.Fatal("partial confirmation committed despite database event failure")
	}
}
func TestNotificationStaffInvitationAndEmailRecoveryPreserveLocalAuth(t *testing.T) {
	f, c, _, _, n, sender := notificationFixture(t)
	_, code := staffInvite(t, f, owner, c, []string{"view"})
	processMail(t, n)
	if sender.count() != 1 {
		t.Fatal("invitation delivery missing")
	}
	client := staffActivateTest(t, f, code)
	assertCode(t, staffReq(f, staffTestClient{}, "POST", "/recovery-email", map[string]string{"email": "assistant@example.invalid"}), 202, "")
	processMail(t, n)
	// Only decrypt the captured test mailbox's MIME body, never query a plaintext token.
	message := string(sender.messages[1])
	parts := strings.SplitN(message, "\r\n\r\n", 2)
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(parts[1], "\r\n", ""))
	if err != nil {
		t.Fatal(err)
	}
	var emailCode string
	for _, line := range strings.Split(string(raw), "\r\n") {
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

func TestNotificationPublicRecoveryRateAndNoReferenceOverride(t *testing.T) {
	f, _, start, p, _, _ := notificationFixture(t)
	receipt, _ := notificationBooking(t, f, p, start)
	assertCode(t, pubRequest(f, "POST", "/public/appointments/confirmation", "", map[string]string{"accessCode": receipt.AccessCode, "reference": newOpaqueID()}), 400, "INVALID_REQUEST")
	for range 29 {
		assertCode(t, pubRequest(f, "POST", "/public/appointments/confirmation", "", map[string]string{"accessCode": strings.Repeat("x", 43)}), 404, "CONFIRMATION_NOT_FOUND")
	}
	w := pubRequest(f, "POST", "/public/appointments/confirmation", "", map[string]string{"accessCode": receipt.AccessCode})
	assertCode(t, w, 429, "PUBLIC_RATE_LIMITED")
	if w.Header().Get("Retry-After") != "60" {
		t.Fatal("missing recovery rate guidance")
	}
}
