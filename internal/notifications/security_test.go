package notifications

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

type discardMail struct{}

func (discardMail) Send(context.Context, string, []byte) error { return nil }
func TestNotificationEncryptionBindsCiphertextToItsPurpose(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{23}, 32))
	s, err := NewWithSender(nil, key, "notices@example.invalid", 1440, discardMail{})
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
func TestNotificationSMTPRequiresVerifiedTLSAndValidHeaders(t *testing.T) {
	for _, c := range []Config{{Host: "localhost", Port: 1025, From: "notices@example.invalid", TLSMode: "none"}, {Host: "localhost", Port: 1025, From: "victim@example.invalid\r\nBcc: another@example.invalid", TLSMode: "starttls"}, {Host: "http://localhost", Port: 1025, From: "notices@example.invalid", TLSMode: "starttls"}} {
		if _, err := NewSMTP(c); err == nil {
			t.Fatal("unsafe SMTP configuration accepted")
		}
	}
	if _, err := NewSMTP(Config{Host: "localhost", Port: 1025, From: "notices@example.invalid", TLSMode: "starttls"}); err != nil {
		t.Fatal(err)
	}
}
func TestNotificationSpanishAdministrativeTemplatesAndStableMessageID(t *testing.T) {
	s, err := NewWithSender(nil, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), "notices@example.invalid", 1440, discardMail{})
	if err != nil {
		t.Fatal(err)
	}
	s.origin = "https://doctorchart.example"
	// DST uses the clinic's zone at the reservation instant, not the process zone.
	m := Message{Date: time.Date(2030, 3, 10, 7, 0, 0, 0, time.UTC), To: "recipient@example.invalid", Doctor: "Profesional", Clinic: "Consultorio", Timezone: "America/New_York", Start: time.Date(2030, 3, 10, 7, 30, 0, 0, time.UTC), Reference: "opaque-reference", Code: Token()}
	for _, kind := range []string{"BOOKING_CONFIRMED", "BOOKING_CANCELLED", "BOOKING_RESCHEDULED", "BOOKING_REMINDER", "STAFF_INVITATION", "STAFF_RECOVERY"} {
		t.Run(kind, func(t *testing.T) {
			a, err := s.render(kind, "stable-job", m)
			if err != nil {
				t.Fatal(err)
			}
			b, err := s.render(kind, "stable-job", m)
			if err != nil || !bytes.Equal(a, b) {
				t.Fatal("retry changed identity")
			}
			parts := strings.SplitN(string(a), "\r\n\r\n", 2)
			plain, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(parts[1], "\r\n", ""))
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(kind, "BOOKING_") {
				if !strings.Contains(string(plain), "03:30 (hora local del consultorio)") ||
					strings.Contains(string(plain), m.Timezone) ||
					!strings.Contains(string(plain), "https://doctorchart.example/appointments/confirmation") {
					t.Fatal("incorrect DST or secret-free link")
				}
			}
			if strings.Contains(parts[0], m.Code) {
				t.Fatal("secret in mail headers")
			}
			for _, line := range strings.Split(parts[1], "\r\n") {
				if len(line) > 76 {
					t.Fatal("invalid SMTP MIME line")
				}
			}
		})
	}
}
func TestNotificationRetryBoundaries(t *testing.T) {
	if retry(1) != time.Second || retry(2) != 2*time.Second || retry(20) != 5*time.Minute {
		t.Fatal("retry bounds changed")
	}
}
