package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/db"
)

func entitlementPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("DOCTORCHART_ENTITLEMENTS_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("owned disposable entitlement database required")
	}
	c, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.ConnConfig.Database != "doctorchart_entitlements_test" || c.ConnConfig.Host != "127.0.0.1" || c.ConnConfig.User != "migration_fixture" {
		t.Fatal("only owned loopback fixture allowed")
	}
	p, err := pgxpool.NewWithConfig(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	if _, err := p.Exec(t.Context(), "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	if err := db.Ensure(t.Context(), p, nil); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSnapshotsExpireCannotExtendCoverageAndRejectInvalidData(t *testing.T) {
	p := entitlementPool(t)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	max := int64(2)
	coverage := now.Add(20 * time.Second)
	value := Snapshot{OwnerID: "owner", Module: "DOCTORCHART", Revision: strings.Repeat("a", 64), IssuedAt: now, ValidUntil: coverage, CoverageExpiresAt: &coverage, ClinicManagement: true, MaxClinics: &max}
	var mode atomic.Int32
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if mode.Load() == 1 {
			w.WriteHeader(503)
			return
		}
		body, _ := json.Marshal(value)
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write(body)
		w.Header().Set("X-Entitlements-Signature", hex.EncodeToString(mac.Sum(nil)))
		w.Write(body)
	}))
	defer server.Close()
	e := NewEntitlements(p, server.URL, "secret")
	e.now = func() time.Time { return now }
	if _, err := e.Resolve(t.Context(), "owner"); err != nil {
		t.Fatal(err)
	}
	mode.Store(1)
	now = now.Add(10 * time.Second)
	if _, err := e.Resolve(t.Context(), "owner"); err != nil {
		t.Fatal("valid snapshot denied during Billing outage", err)
	}
	if calls.Load() != 1 {
		t.Fatal("unexpected refresh of valid lease")
	}
	now = now.Add(10 * time.Second)
	if _, err := e.Resolve(t.Context(), "owner"); err == nil {
		t.Fatal("expired coverage authorized during outage")
	}
	mode.Store(0)
	for _, mutation := range []func(*Snapshot){
		func(s *Snapshot) { s.OwnerID = "other" }, func(s *Snapshot) { s.Module = "POINTLY" },
		func(s *Snapshot) { s.ValidUntil = s.IssuedAt.Add(61 * time.Second) }, func(s *Snapshot) { s.ValidUntil = coverage.Add(time.Second) },
		func(s *Snapshot) { s.MaxClinics = nil }, func(s *Snapshot) { n := int64(-2); s.MaxClinics = &n },
	} {
		value.IssuedAt = now
		coverage = now.Add(20 * time.Second)
		value.ValidUntil = coverage
		value.OwnerID = "owner"
		value.Module = "DOCTORCHART"
		value.MaxClinics = &max
		mutation(&value)
		if _, err := e.Resolve(t.Context(), "owner"); err == nil {
			t.Fatal("invalid snapshot accepted", value)
		}
	}
}

func TestInvalidationDuringFetchDoesNotRepopulateOldCache(t *testing.T) {
	p := entitlementPool(t)
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		coverage := now.Add(time.Hour)
		max := int64(1)
		value := Snapshot{OwnerID: "owner", Module: "DOCTORCHART", Revision: strings.Repeat("b", 64), IssuedAt: now, ValidUntil: now.Add(time.Minute), CoverageExpiresAt: &coverage, ClinicManagement: true, MaxClinics: &max}
		close(started)
		<-release
		body, _ := json.Marshal(value)
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write(body)
		w.Header().Set("X-Entitlements-Signature", hex.EncodeToString(mac.Sum(nil)))
		w.Write(body)
	}))
	defer server.Close()
	e := NewEntitlements(p, server.URL, "secret")
	done := make(chan error, 1)
	go func() { _, err := e.Resolve(context.Background(), "owner"); done <- err }()
	<-started
	if err := e.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("fetch crossing invalidation accepted")
	}
	e.mu.Lock()
	n := len(e.cache)
	e.mu.Unlock()
	if n != 0 {
		t.Fatal("invalidated fetch cached")
	}
}
