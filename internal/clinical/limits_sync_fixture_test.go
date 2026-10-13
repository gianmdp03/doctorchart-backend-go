package clinical

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
)

// This server is a test fixture only. The inherited integrity fixture accepts
// only the explicitly opted-in disposable loopback database.
func TestLimitsSyncDoctorChartFixture(t *testing.T) {
	if os.Getenv("DOCTORCHART_LIMITS_SYNC_FIXTURE") != "1" {
		t.Skip("owned cross-service fixture only")
	}
	billingURL := os.Getenv("LIMITS_SYNC_BILLING_URL")
	u, err := url.Parse(billingURL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		t.Fatal("loopback Billing fixture required")
	}
	ready := os.Getenv("R3_FIXTURE_READY")
	if !filepath.IsAbs(ready) {
		t.Fatal("absolute readiness file required")
	}
	services, _ := integrityFixture(t, 50, 100)
	var failNext atomic.Bool
	client := &http.Client{Timeout: 3 * time.Second}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failNext.CompareAndSwap(true, false) {
			w.WriteHeader(503)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), "GET", billingURL+r.URL.RequestURI(), nil)
		if err != nil {
			w.WriteHeader(503)
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			w.WriteHeader(503)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 175*time.Second)
	defer cancel()
	rules := []*billing.Rules{billing.New(proxy.URL), billing.New(billingURL)}
	stop := make(chan struct{}, 1)
	servers := make([]*httptest.Server, 2)
	done := make(chan struct{})
	go func() { defer close(done); rules[1].RunRefresh(ctx) }()
	defer func() { cancel(); <-done }()
	for i := range services {
		rules[i].Load(ctx)
		services[i].rules = rules[i]
		mux := http.NewServeMux()
		api.BaseRoutes(mux, rules[i], "fixture-billing-secret-32-characters")
		services[i].Routes(mux)
		services[i].EncounterRoutes(mux)
		control := http.NewServeMux()
		control.HandleFunc("POST /__limits/fail-next", func(w http.ResponseWriter, r *http.Request) { failNext.Store(true); w.WriteHeader(204) })
		control.HandleFunc("POST /__limits/stop", func(w http.ResponseWriter, r *http.Request) {
			select {
			case stop <- struct{}{}:
			default:
			}
			w.WriteHeader(204)
		})
		control.Handle("/", api.Gateway("fixture-gateway", api.Dispatch(mux)))
		servers[i] = limitsFixtureServer(t, control)
		// Control paths sit outside the public identity boundary in the test fixture.
		app := servers[i]
		t.Cleanup(func() { app.CloseClientConnections(); app.Close() })
	}
	data, err := json.Marshal(map[string]string{"url": servers[0].URL, "replica": servers[1].URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, data, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stop:
	case <-ctx.Done():
		t.Fatal("fixture shutdown not requested")
	}
}

// Linux's ephemeral range starts below the fixture's required high-port range.
// Keep the callback allowlist strict by owning an explicit high-port listener.
func limitsFixtureServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	first := int(time.Now().UnixNano() % 16384)
	for offset := range 128 {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", 49152+(first+offset)%16384))
		if err != nil {
			continue
		}
		server := httptest.NewUnstartedServer(handler)
		if err := server.Listener.Close(); err != nil {
			listener.Close()
			t.Fatal(err)
		}
		server.Listener = listener
		server.Start()
		return server
	}
	t.Fatal("could not own a high-port fixture listener")
	return nil
}
