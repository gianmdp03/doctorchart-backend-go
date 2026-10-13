package billing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReloadAndFailureRetention(t *testing.T) {
	var mu sync.Mutex
	payload := `{"BASIC":{"max_patients":77,"max_records_per_patient":123}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/billing/limits/DOCTORCHART" {
			t.Error("wrong billing path")
		}
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprint(w, payload)
	}))
	defer server.Close()
	rules := New(server.URL)
	if err := rules.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rules.Int("NONE", "max_patients", 50) != 77 || rules.Int("BASIC", "max_records_per_patient", 100) != 123 {
		t.Fatal("fallback/numeric conversion mismatch")
	}
	if rules.Int("FREE_TRIAL", "max_patients", 50) != 20 {
		t.Fatal("trial must override billing")
	}
	mu.Lock()
	payload = `{}`
	mu.Unlock()
	if err := rules.Reload(context.Background()); err == nil {
		t.Fatal("empty catalog acknowledged as refreshed")
	}
	if rules.Int("BASIC", "max_patients", 50) != 77 {
		t.Fatal("empty reload overwrote rules")
	}
	copy := rules.All()
	copy["BASIC"]["max_patients"] = 1
	if rules.Int("BASIC", "max_patients", 50) != 77 {
		t.Fatal("caller mutated live rules")
	}
}

func TestReloadRejectsInvalidSnapshotsAtomically(t *testing.T) {
	cases := map[string]string{
		"string":            `{"BASIC":{"max_patients":"123"}}`,
		"null limit":        `{"BASIC":{"max_patients":null}}`,
		"boolean":           `{"BASIC":{"max_patients":true}}`,
		"fraction":          `{"BASIC":{"max_patients":1.5}}`,
		"tiny fraction":     `{"BASIC":{"max_patients":1.0000000000000000000001}}`,
		"negative":          `{"BASIC":{"max_patients":-2}}`,
		"overflow":          `{"BASIC":{"max_patients":2147483648}}`,
		"unknown key":       `{"BASIC":{"max_clients":10}}`,
		"unknown tier":      `{"ENTERPRISE":{"max_patients":10}}`,
		"null tier":         `{"BASIC":null}`,
		"empty":             `{}`,
		"null catalog":      `null`,
		"trailing":          `{"BASIC":{"max_patients":10}} {}`,
		"partial malformed": `{"BASIC":{"max_patients":10},"PRO":{"max_patients":`,
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			var payload atomic.Value
			payload.Store(`{"BASIC":{"max_patients":77,"max_records_per_patient":123}}`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, payload.Load().(string))
			}))
			defer server.Close()
			rules := New(server.URL)
			if err := rules.Reload(t.Context()); err != nil {
				t.Fatal(err)
			}
			payload.Store(invalid)
			if err := rules.Reload(t.Context()); err == nil {
				t.Errorf("invalid snapshot acknowledged: %s", name)
			}
			if got := rules.Int("BASIC", "max_patients", 50); got != 77 {
				t.Errorf("previous snapshot changed: %d", got)
			}
			payload.Store(`{"PRO":{"max_patients":9}}`)
			if err := rules.Reload(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := rules.Int("PRO", "max_patients", 50); got != 9 {
				t.Fatalf("recovery = %d", got)
			}
		})
	}
}

func TestReloadIntegerBoundariesAndSparseCatalog(t *testing.T) {
	for _, literal := range []string{"-1", "0", "2147483647", "12.0", "1e2"} {
		t.Run(literal, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"BASIC":{"max_patients":%s},"PRO":{}}`, literal)
			}))
			defer server.Close()
			rules := New(server.URL)
			if err := rules.Reload(t.Context()); err != nil {
				t.Fatal(err)
			}
			want := map[string]int{"-1": -1, "0": 0, "2147483647": 2147483647, "12.0": 12, "1e2": 100}[literal]
			if got := rules.Int("BASIC", "max_patients", 50); got != want {
				t.Fatalf("got %d, want %d", got, want)
			}
			if rules.Int("PRO", "max_records_per_patient", 100) != 100 {
				t.Fatal("missing key lost fallback")
			}
			if rules.Int("FREE_TRIAL", "max_patients", 50) != 20 || rules.Int("FREE_TRIAL", "max_records_per_patient", 100) != 50 {
				t.Fatal("trial bounds changed")
			}
		})
	}
}

func TestReloadRejectsUnsuccessfulHTTPAndCancellation(t *testing.T) {
	for _, code := range []int{204, 206, 403, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				fmt.Fprint(w, `{"BASIC":{"max_patients":1}}`)
			}))
			defer server.Close()
			if err := New(server.URL).Reload(t.Context()); err == nil {
				t.Fatal("unsuccessful HTTP accepted")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := New(server.URL).Reload(ctx); err == nil {
		t.Fatal("canceled fetch succeeded")
	}
}

func TestRulesFreshnessFailureAndRecovery(t *testing.T) {
	var status atomic.Int32
	status.Store(503)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		fmt.Fprint(w, `{"BASIC":{"max_patients":77}}`)
	}))
	defer server.Close()
	rules := New(server.URL)
	if rules.Available() {
		t.Fatal("startup defaults authorized quota writes")
	}
	if err := rules.EnsureFresh(t.Context()); err == nil || rules.Available() {
		t.Fatal("failed initial fetch authorized quota writes")
	}
	status.Store(200)
	if err := rules.EnsureFresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !rules.Available() {
		t.Fatal("loaded catalog unavailable")
	}
	count := calls.Load()
	if err := rules.EnsureFresh(t.Context()); err != nil || calls.Load() != count {
		t.Fatal("fresh cache performed unnecessary I/O")
	}
	status.Store(503)
	rules.mu.Lock()
	rules.loadedAt = time.Now().Add(-9 * time.Minute)
	previous := rules.loadedAt
	rules.mu.Unlock()
	if err := rules.Reload(t.Context()); err == nil {
		t.Fatal("failed forced reload acknowledged")
	}
	rules.mu.RLock()
	unchanged := rules.loadedAt.Equal(previous)
	rules.mu.RUnlock()
	if !unchanged || !rules.Available() {
		t.Fatal("failure changed lifetime or lost bounded previous snapshot")
	}
	rules.mu.Lock()
	rules.loadedAt = time.Now().Add(-rulesMaxAge)
	rules.mu.Unlock()
	if rules.Available() {
		t.Fatal("expired snapshot available")
	}
	if err := rules.EnsureFresh(t.Context()); err == nil || rules.Available() {
		t.Fatal("failed recovery authorized expired snapshot")
	}
	if got := rules.Int("BASIC", "max_patients", 50); got != 77 {
		t.Fatal("failure damaged last good snapshot")
	}
	status.Store(200)
	if err := rules.EnsureFresh(t.Context()); err != nil || !rules.Available() {
		t.Fatal("recovery did not refresh lifetime")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := rules.EnsureFresh(ctx); err == nil {
		t.Fatal("canceled quota request authorized")
	}
}

func TestRulesConcurrentRecoveryAndReloadSerialization(t *testing.T) {
	var calls, active, maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
		fmt.Fprint(w, `{"BASIC":{"max_patients":77}}`)
	}))
	defer server.Close()
	rules := New(server.URL)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 16 {
		wg.Go(func() {
			<-start
			if err := rules.EnsureFresh(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent recovery fetched %d times", calls.Load())
	}
	for range 16 {
		wg.Go(func() {
			if err := rules.Reload(t.Context()); err != nil {
				t.Error(err)
			}
			if rules.Int("BASIC", "max_patients", 50) != 77 || !rules.Available() {
				t.Error("concurrent reader lost snapshot")
			}
			copy := rules.All()
			copy["BASIC"]["max_patients"] = 3
		})
	}
	wg.Wait()
	if maximum.Load() != 1 {
		t.Fatalf("reloads were not serialized: %d", maximum.Load())
	}
	if rules.Int("BASIC", "max_patients", 50) != 77 {
		t.Fatal("copy modified snapshot")
	}
}

func TestRulesRefreshLifecycleAndRecovery(t *testing.T) {
	var payload atomic.Value
	payload.Store(`{"BASIC":{"max_patients":77}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, payload.Load().(string))
	}))
	defer server.Close()
	rules := New(server.URL)
	rules.Load(t.Context())
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); rules.runRefresh(ctx, time.Millisecond*2) }()
	defer func() { cancel(); <-done }()
	payload.Store(`{}`)
	if err := rules.Reload(t.Context()); err == nil {
		t.Fatal("empty refresh acknowledged")
	}
	if rules.Int("BASIC", "max_patients", 50) != 77 {
		t.Fatal("failed refresh damaged catalog")
	}
	payload.Store(`{"BASIC":{"max_patients":99}}`)
	deadline := time.Now().Add(time.Second)
	for rules.Int("BASIC", "max_patients", 50) != 99 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rules.Int("BASIC", "max_patients", 50) != 99 {
		t.Fatal("periodic refresh did not converge without restart")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh owner cancellation did not stop worker")
	}
}

func TestRulesRefreshCancelsInflightFetch(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	rules := New(server.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); rules.runRefresh(ctx, time.Millisecond) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start fetch")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh did not cancel inflight HTTP")
	}
	if rules.Available() {
		t.Fatal("canceled refresh marked snapshot loaded")
	}
}
func TestPlanAliases(t *testing.T) {
	for input, want := range map[string]string{"enterprise": "UNLIMITED", " pro ": "PRO", "unknown": "NONE", "": "NONE", "FREE_TRIAL": "FREE_TRIAL"} {
		if got := Plan(input); got != want {
			t.Fatalf("%q=%s, want %s", input, got, want)
		}
	}
}
