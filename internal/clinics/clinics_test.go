package clinics

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
	"github.com/syntah/doctorchart-backend-go/internal/bootstrap"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

func TestActualBillingEffectiveRights(t *testing.T) {
	address := os.Getenv("DOCTORCHART_EFFECTIVE_BILLING_TEST_URL")
	key := os.Getenv("DOCTORCHART_EFFECTIVE_BILLING_TEST_SECRET")
	if address == "" || key == "" {
		t.Skip("run through Billing TestM2CrossRepositoryEffectiveRights with owned fixtures")
	}
	u, err := url.Parse(address)
	if err != nil || u.Hostname() != "127.0.0.1" {
		t.Fatal("only local fixture provider allowed")
	}
	f := newFixture(t)
	rights := billing.NewEntitlements(f.pool, address, key)
	mux := http.NewServeMux()
	New(f.pool, rights).Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	w := f.request("GET", "/clinics/capabilities", owner, "")
	assertCode(t, w, 200, "")
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"VERIFIED"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"max_clinics":2`)) {
		t.Fatalf("actual Billing rights not effective: %s", w.Body)
	}
	for range 2 {
		assertCode(t, f.request("POST", "/clinics", owner, createBody), 201, "")
	}
	assertCode(t, f.request("POST", "/clinics", owner, createBody), 409, "CLINIC_LIMIT_REACHED")
}

const owner = "11111111-1111-4111-8111-111111111111"
const other = "22222222-2222-4222-8222-222222222222"
const secret = "fixture-billing-secret-with-64-characters-aaaaaaaaaaaaaaaaaaaaaaaa"

type fixture struct {
	pool    *pgxpool.Pool
	handler http.Handler
	rights  *billing.Entitlements
	mode    atomic.Int32
	online  atomic.Bool
	limit   atomic.Int64
	calls   atomic.Int32
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	raw := os.Getenv("DOCTORCHART_CLINICS_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("owned disposable doctorchart_clinics_test DSN required")
	}
	config, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "doctorchart_clinics_test" || config.ConnConfig.Host != "127.0.0.1" || config.ConnConfig.User != "migration_fixture" {
		t.Fatal("owned loopback fixture required")
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(t.Context(), "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	v, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.Run(t.Context(), pool, v); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{owner, other} {
		if _, err := pool.Exec(t.Context(), "INSERT INTO doctor_workspaces(created_at,current_step_key,onboarding_completed,updated_at,user_id,version,specialty_id) SELECT now(),'COMPLETE',true,now(),$1,0,min(id) FROM specialties", user); err != nil {
			t.Fatal(err)
		}
	}
	f := &fixture{pool: pool}
	f.limit.Store(1)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Header.Get("X-Internal-Secret") != secret {
			t.Error("missing internal authentication")
			w.WriteHeader(401)
			return
		}
		mode := f.mode.Load()
		if mode == 1 {
			w.WriteHeader(503)
			return
		}
		now := time.Now().UTC().Add(-time.Millisecond)
		until := now.Add(time.Minute)
		coverage := now.Add(time.Hour)
		max := f.limit.Load()
		value := billing.Snapshot{OwnerID: strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/internal/doctorchart/owners/"), "/entitlements"), Module: "DOCTORCHART", Revision: strings.Repeat("a", 64), IssuedAt: now, ValidUntil: until, CoverageExpiresAt: &coverage, ClinicManagement: true, MaxClinics: &max}
		if strings.HasSuffix(r.URL.Path, "/entitlements-v2") {
			flag := f.online.Load()
			value.OwnerID = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/internal/doctorchart/owners/"), "/entitlements-v2")
			value.OnlineBooking = &flag
		}
		if mode == 2 {
			value.ClinicManagement = false
			if value.OnlineBooking != nil {
				*value.OnlineBooking = false
			}
			value.MaxClinics = nil
		}
		if mode == 3 {
			value.ValidUntil = now.Add(-time.Second)
		}
		body, _ := json.Marshal(value)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		signature := hex.EncodeToString(mac.Sum(nil))
		if mode == 4 {
			signature = strings.Repeat("0", 64)
		}
		w.Header().Set("X-Entitlements-Signature", signature)
		w.Write(body)
	}))
	t.Cleanup(remote.Close)
	f.rights = billing.NewEntitlements(pool, remote.URL, secret)
	mux := http.NewServeMux()
	New(pool, f.rights).Routes(mux)
	f.handler = api.Gateway("gateway-fixture", api.Dispatch(mux))
	return f
}

func (f *fixture) request(method, path, user, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/doctorchart"+path, strings.NewReader(body))
	r.Header.Set("X-Gateway-Secret", "gateway-fixture")
	r.Header.Set("X-User-Id", user)
	r.Header.Set("X-User-Role", "COMPANY")
	r.Header.Set("X-User-Plan", "BASIC")
	r.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).Format(time.RFC3339))
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

const createBody = `{"name":"Centro","country":"Argentina","province":"Buenos Aires","city":"Mar del Plata","address":"Synthetic 123","timezone":"America/Argentina/Buenos_Aires"}`

func assertCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d want %d: %s", w.Code, status, w.Body)
	}
	if code != "" {
		var e api.Error
		if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || e.Code != code {
			t.Fatalf("wrong error %s", w.Body)
		}
	}
}
func clinic(t *testing.T, w *httptest.ResponseRecorder) Clinic {
	t.Helper()
	var c Clinic
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClinicOwnershipQuotaStatesAndOfflineSnapshot(t *testing.T) {
	f := newFixture(t)
	w := f.request("POST", "/clinics", owner, createBody)
	assertCode(t, w, 201, "")
	c := clinic(t, w)
	if c.Version != "0" || c.Status != "ACTIVE" || w.Header().Get("Location") != "/api/doctorchart/clinics/"+c.ID {
		t.Fatal("create contract diverged")
	}
	assertCode(t, f.request("GET", "/clinics/"+c.ID, other, ""), 404, "CLINIC_NOT_FOUND")
	assertCode(t, f.request("PATCH", "/clinics/"+c.ID, other, `{"expectedVersion":"0","name":"steal"}`), 404, "CLINIC_NOT_FOUND")
	assertCode(t, f.request("POST", "/clinics", owner, createBody), 409, "CLINIC_LIMIT_REACHED")
	f.mode.Store(1)
	calls := f.calls.Load()
	w = f.request("PATCH", "/clinics/"+c.ID, owner, `{"expectedVersion":"0","name":"New name"}`)
	assertCode(t, w, 200, "")
	if f.calls.Load() != calls {
		t.Fatal("valid snapshot unnecessarily contacted unavailable Billing")
	}
	assertCode(t, f.request("PATCH", "/clinics/"+c.ID, owner, `{"expectedVersion":"0","name":"stale"}`), 409, "VERSION_CONFLICT")
	w = f.request("PATCH", "/clinics/"+c.ID+"/status", owner, `{"expectedVersion":"1","status":"INACTIVE"}`)
	assertCode(t, w, 200, "")
	assertCode(t, f.request("POST", "/clinics", owner, createBody), 409, "CLINIC_LIMIT_REACHED")
	w = f.request("PATCH", "/clinics/"+c.ID+"/status", owner, `{"expectedVersion":"2","status":"ARCHIVED"}`)
	assertCode(t, w, 200, "")
	assertCode(t, f.request("PATCH", "/clinics/"+c.ID, owner, `{"expectedVersion":"3","name":"archived"}`), 409, "CLINIC_STATE_CONFLICT")
	w = f.request("POST", "/clinics", owner, createBody)
	assertCode(t, w, 201, "")
	c2 := clinic(t, w)
	assertCode(t, f.request("PATCH", "/clinics/"+c.ID+"/status", owner, `{"expectedVersion":"3","status":"INACTIVE"}`), 409, "CLINIC_LIMIT_REACHED")
	if err := f.rights.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCode(t, f.request("PATCH", "/clinics/"+c2.ID, owner, `{"expectedVersion":"0","name":"must not write"}`), 503, "ENTITLEMENTS_UNAVAILABLE")
	assertCode(t, f.request("GET", "/clinics", owner, ""), 200, "")
	w = f.request("GET", "/clinics/capabilities", owner, "")
	assertCode(t, w, 200, "")
	if !bytes.Contains(w.Body.Bytes(), []byte(`"create":false`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"status":"UNAVAILABLE"`)) {
		t.Fatal(w.Body.String())
	}
	f.mode.Store(0)
	f.limit.Store(0)
	w = f.request("GET", "/clinics/capabilities", owner, "")
	assertCode(t, w, 200, "")
	if !bytes.Contains(w.Body.Bytes(), []byte(`"archive":true`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"edit":false`)) {
		t.Fatal("downgrade permissions", w.Body.String())
	}
	assertCode(t, f.request("PATCH", "/clinics/"+c2.ID, owner, `{"expectedVersion":"0","name":"over quota"}`), 409, "CLINIC_LIMIT_REACHED")
	assertCode(t, f.request("PATCH", "/clinics/"+c2.ID+"/status", owner, `{"expectedVersion":"0","status":"ARCHIVED"}`), 200, "")
}

func TestClinicConcurrentLastSlotAndVersion(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	results := make(chan int, 8)
	for range 8 {
		wg.Go(func() { results <- f.request("POST", "/clinics", owner, createBody).Code })
	}
	wg.Wait()
	close(results)
	created := 0
	for code := range results {
		if code == 201 {
			created++
		} else if code != 409 {
			t.Fatalf("unexpected concurrent status %d", code)
		}
	}
	if created != 1 {
		t.Fatalf("admitted %d last-slot requests", created)
	}
	w := f.request("GET", "/clinics", owner, "")
	var list struct{ Items []Clinic }
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	results = make(chan int, 2)
	for range 2 {
		wg.Go(func() {
			results <- f.request("PATCH", "/clinics/"+list.Items[0].ID, owner, `{"expectedVersion":"0","name":"edit"}`).Code
		})
	}
	wg.Wait()
	close(results)
	success := 0
	for code := range results {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatal(code)
		}
	}
	if success != 1 {
		t.Fatal("lost update protection failed")
	}
}

func TestClinicValidationDeniedAndUnavailable(t *testing.T) {
	f := newFixture(t)
	for _, zone := range []string{"Local", "ART", "+03:00", "posix/Europe/Madrid", "right/Europe/Madrid", "America/Unknown"} {
		body := strings.Replace(createBody, "America/Argentina/Buenos_Aires", zone, 1)
		assertCode(t, f.request("POST", "/clinics", owner, body), 400, "INVALID_REQUEST")
	}
	for _, body := range []string{`{}`, `{"ownerId":"fake"}`, strings.Replace(createBody, `"Centro"`, `null`, 1), strings.Replace(createBody, `"Centro"`, `""`, 1)} {
		assertCode(t, f.request("POST", "/clinics", owner, body), 400, "INVALID_REQUEST")
	}
	for _, query := range []string{"?page=-1", "?size=101", "?status=DELETED", "?page=1&page=2", "?ownerId=other", "?page=9223372036854775807"} {
		assertCode(t, f.request("GET", "/clinics"+query, owner, ""), 400, "INVALID_REQUEST")
	}
	for _, mode := range []int32{1, 3, 4} {
		f.mode.Store(mode)
		assertCode(t, f.request("POST", "/clinics", owner, createBody), 503, "ENTITLEMENTS_UNAVAILABLE")
	}
	f.mode.Store(2)
	assertCode(t, f.request("POST", "/clinics", owner, createBody), 403, "CLINIC_MANAGEMENT_DISABLED")
	if err := f.rights.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.mode.Store(0)
	f.limit.Store(-1)
	for range 3 {
		assertCode(t, f.request("POST", "/clinics", owner, createBody), 201, "")
	}
	assertCode(t, f.request("GET", "/clinics?page=0&size=2", owner, ""), 200, "")
	r := httptest.NewRequest("POST", "/api/doctorchart/clinics", strings.NewReader(createBody))
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	assertCode(t, w, 401, "UNAUTHENTICATED")
}

func TestEntitlementFenceRejectsOldSnapshotAcrossInstances(t *testing.T) {
	f := newFixture(t)
	snapshot, err := f.rights.Resolve(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := f.rights.Fence(t.Context(), tx, snapshot); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := billing.NewEntitlements(f.pool, "", secret).Invalidate(ctx); err == nil {
		t.Fatal("invalidation passed a live mutation fence")
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.rights.Invalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	tx, err = f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := f.rights.Fence(t.Context(), tx, snapshot); err == nil {
		t.Fatal("old snapshot survived shared invalidation")
	}
	f.mode.Store(1)
	if _, err := f.rights.Resolve(t.Context(), owner); err == nil {
		t.Fatal("cache resurrected after failed reload")
	}
}

func TestDecimalContract(t *testing.T) {
	for _, raw := range []string{"01", "-1", "+1", "1.0", "9223372036854775808"} {
		if _, err := decimal(raw, true); err == nil {
			t.Fatal(fmt.Sprintf("noncanonical ID accepted %q", raw))
		}
	}
}
