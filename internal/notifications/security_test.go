package notifications

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/syntah/doctorchart-backend-go/internal/mailclient"
	"testing"
	"time"
)

type discardMail struct{}

func (discardMail) Send(context.Context, mailclient.Message) (mailclient.Result, error) {
	return mailclient.Result{}, nil
}
func TestNotificationEncryptionBindsCiphertextToItsPurpose(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{23}, 32))
	s, err := NewWithSender(nil, key, discardMail{})
	if err != nil {
		t.Fatal(err)
	}
	secret := Token()
	if len(secret) != 43 || Hash(secret) == secret {
		t.Fatal("weak token")
	}
	raw := s.seal([]byte(secret), "booking:42")
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("plaintext secret stored")
	}
	decoded, err := s.open(raw, "booking:42")
	if err != nil || string(decoded) != secret {
		t.Fatal("cannot recover encrypted code", err)
	}
	if _, err = s.open(raw, "booking:43"); err == nil {
		t.Fatal("ciphertext moved between bookings")
	}
	raw[len(raw)-1] ^= 1
	if _, err = s.open(raw, "booking:42"); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}
func TestNotificationUsesMailService(t *testing.T) {
	key := strings.Repeat("k", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/emails" || r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("wrong mail service request")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var message mailclient.Message
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil || message.To != "recipient@example.invalid" {
			t.Error("invalid mail service message", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(mailclient.Result{ID: message.ID, Status: "accepted_by_provider"})
	}))
	defer server.Close()
	c := Config{Enabled: true, Key: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)),
		PublicOrigin: "https://doctorchart.example", MailServiceURL: server.URL, MailServiceAPIKey: key}
	s, err := New(nil, c)
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.render("BOOKING_PORTAL_CODE", "stable-job", Message{Date: time.Now(), To: "recipient@example.invalid", Code: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.sender.Send(t.Context(), message); err != nil {
		t.Fatal(err)
	}
	c.MailServiceURL = ""
	if _, err := New(nil, c); err == nil {
		t.Fatal("missing mail service accepted")
	}
}
func TestNotificationSpanishAdministrativeTemplatesAndStableMessageID(t *testing.T) {
	s, err := NewWithSender(nil, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), discardMail{})
	if err != nil {
		t.Fatal(err)
	}
	s.origin = "https://doctorchart.example"
	m := Message{Date: time.Date(2030, 3, 10, 7, 0, 0, 0, time.UTC), To: "recipient@example.invalid", Code: "012345"}
	for _, kind := range []string{"STAFF_RECOVERY"} {
		t.Run(kind, func(t *testing.T) {
			a, err := s.render(kind, "stable-job", m)
			if err != nil {
				t.Fatal(err)
			}
			b, err := s.render(kind, "stable-job", m)
			if err != nil || a != b {
				t.Fatal("retry changed identity")
			}
			if !strings.Contains(a.Text, "\n"+m.Code+"\n") || !strings.Contains(a.HTML, ">"+m.Code+"<") {
				t.Fatal("missing recovery code")
			}
			if strings.Contains(a.Subject+a.ID+a.To, m.Code) {
				t.Fatal("secret in mail metadata")
			}
		})
	}
}

func TestStaffRecoveryDigestIsBoundToIssueAndSecret(t *testing.T) {
	newService := func(key byte) *Service {
		t.Helper()
		s, err := NewWithSender(nil, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{key}, 32)), discardMail{})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := newService(1)
	const code = "012345"
	digest := s.recoveryCodeHash("issue-1", code)
	if digest == Hash(code) || !s.MatchesRecoveryCode("issue-1", code, digest) ||
		s.MatchesRecoveryCode("issue-2", code, digest) || s.MatchesRecoveryCode("issue-1", "012346", digest) ||
		newService(2).MatchesRecoveryCode("issue-1", code, digest) {
		t.Fatal("recovery digest is not bound to its issue, code and private key")
	}
}
func TestBookingChangeIncludesReasonAndCorrectPatientLink(t *testing.T) {
	s := &Service{origin: "https://doctorchart.example"}
	m := Message{
		Date: time.Date(2030, 3, 9, 19, 0, 0, 0, time.UTC), To: "recipient@example.invalid",
		Start: time.Date(2030, 3, 10, 17, 30, 0, 0, time.UTC), Timezone: "America/Argentina/Buenos_Aires",
		Change: &bookingChange{Reason: "Cambio de agenda <script>alert(1)</script>\nGracias por tu comprensión.", PublicID: "profile-id"},
	}
	cases := []struct{ kind, link, dateLabel string }{
		{"BOOKING_CANCELLED", "/appointments/profile-id", "Fecha y hora del turno cancelado"},
		{"BOOKING_RESCHEDULED", "/appointments/confirmation", "Nueva fecha y hora"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			message, err := s.render(tc.kind, "booking-change", m)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(message.Text, m.Change.Reason) || !strings.Contains(message.Text, "10/03/2030 a las 14:30") ||
				!strings.Contains(message.HTML, tc.dateLabel) || !strings.Contains(message.HTML, s.origin+tc.link) ||
				!strings.Contains(message.HTML, "&lt;script&gt;") || strings.Contains(message.HTML, "<script>") {
				t.Fatal("unexpected content in booking change notification")
			}
		})
	}
	m.Change = &bookingChange{}
	cancel, err := s.render("BOOKING_CANCELLED", "booking-change", m)
	if err != nil || !strings.Contains(cancel.HTML, `href="https://doctorchart.example/appointments"`) ||
		strings.Contains(cancel.Text, "Motivo de la cancelación") {
		t.Fatal("cancellation without a published profile should link to the directory", err)
	}
}

func TestNotificationRetryBoundaries(t *testing.T) {
	if retry(1) != time.Second || retry(2) != 2*time.Second || retry(20) != 5*time.Minute {
		t.Fatal("retry bounds changed")
	}
}

func TestRescheduleProposalEmailOffersBothActions(t *testing.T) {
	s := &Service{origin: "https://doctorchart.example"}
	m := Message{
		Date: time.Now(), To: "recipient@example.invalid", Start: time.Now().Add(time.Hour), Timezone: "UTC",
		Change: &bookingChange{Reference: "opaque-reference", ProposalVersion: "3"},
	}
	message, err := s.render("BOOKING_RESCHEDULED", "proposal", m)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct{ name, value string }{
		{"Confirmar turno", "confirm"}, {"Reprogramar para otra fecha", "reschedule"},
	} {
		link := s.origin + "/appointments/confirmation#action=" + action.value
		if !strings.Contains(message.HTML, link) || !strings.Contains(message.HTML, action.name) ||
			!strings.Contains(message.Text, action.name+": "+link) {
			t.Fatalf("missing %s link in the HTML or text email", action.value)
		}
	}
}
