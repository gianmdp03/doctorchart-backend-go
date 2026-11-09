package clinics

import (
	"context"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"strings"
	"testing"
	"time"
)

func TestPublicProofClaimIsAtomicAcrossConcurrentTransactions(t *testing.T) {
	f, c, start, p := publicFixture(t)
	one := booked(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start)))
	two := booked(t, bookingRequestFixture(f, "POST", "/bookings", owner, newBookingKey(), createBookingBody(c.ID, start.Add(time.Hour))))
	doctor, err := NewWithBookings(f.pool, f.rights, true, true).readPublicDoctor(t.Context(), f.pool, p.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	a := publicAdmission{doctor: doctor, clinic: doctor.Clinics[0], challengeHash: strings.Repeat("b", 64), request: PublicBookingRequest{FormVersion: p.Version, DocumentCountry: "AR", DocumentType: "DNI", Document: "12345678", Email: "synthetic@example.invalid", Answers: map[string]string{}}}
	first, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback(context.Background())
	if err = a.record(t.Context(), first, one); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		tx, e := f.pool.Begin(t.Context())
		if e != nil {
			done <- e
			return
		}
		defer tx.Rollback(context.Background())
		if _, e = tx.Exec(t.Context(), "SET LOCAL application_name='m6-proof-claim-fixture'"); e != nil {
			done <- e
			return
		}
		done <- a.record(t.Context(), tx, two)
	}()
	locked := false
	for i := 0; i < 100; i++ {
		if err = f.pool.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name='m6-proof-claim-fixture' AND wait_event_type='Lock')").Scan(&locked); err != nil {
			t.Fatal(err)
		}
		if locked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !locked {
		t.Fatal("second proof writer did not compete for the unique claim")
	}
	if err = first.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	e := <-done
	failure, ok := e.(*api.Error)
	if !ok || failure.Status != 403 || failure.Code != "CHALLENGE_ALREADY_USED" {
		t.Fatal("proof reuse is not a safe conflict", e)
	}
}
func TestPublicDocumentFormatAndCountry(t *testing.T) {
	base := PublicBookingRequest{ClinicID: newOpaqueID(), Start: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), FormVersion: "1", ContactName: "Solicitante", AttendeeName: "Persona", ContactPhone: "+54 223 1234567", DocumentCountry: "AR", DocumentType: "DNI", Email: "synthetic@example.invalid"}
	for _, doc := range []string{"12345", "123456", "1234567", "12345678"} {
		p := base
		p.Document = doc
		if err := validatePublicRequest(&p); err != nil {
			t.Fatal(doc, err)
		}
	}
	for _, country := range []string{"ZZ", "EU", "BR"} {
		p := base
		p.DocumentCountry = country
		p.Document = "12345678"
		if err := validatePublicRequest(&p); err == nil {
			t.Fatal("unsupported national-document jurisdiction accepted", country)
		}
	}
	p := base
	p.DocumentCountry = "BR"
	p.DocumentType = "PASSPORT"
	p.Document = "ab123456"
	if err := validatePublicRequest(&p); err != nil || p.Document != "AB123456" {
		t.Fatal(err)
	}
}
