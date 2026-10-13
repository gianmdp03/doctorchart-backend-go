package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func healthFixture(checks ...func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	BaseRoutes(mux, nil, "fixture-billing", checks...)
	return Gateway("fixture-gateway", Dispatch(mux))
}

func healthRequest(handler http.Handler, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func TestReadinessDependenciesAndLiveness(t *testing.T) {
	const sensitive = "postgres://fixture:DO_NOT_LOG_SECRET@internal-host/clinical"
	for _, tc := range []struct {
		name   string
		checks []func(context.Context) error
		status int
	}{
		{"healthy", []func(context.Context) error{func(context.Context) error { return nil }}, http.StatusOK},
		{"failed dependency", []func(context.Context) error{func(context.Context) error { return errors.New(sensitive) }}, http.StatusServiceUnavailable},
		{"missing check", nil, http.StatusServiceUnavailable},
		{"nil check", []func(context.Context) error{nil}, http.StatusServiceUnavailable},
		{"ambiguous checks", []func(context.Context) error{func(context.Context) error { return nil }, func(context.Context) error { return nil }}, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := healthFixture(tc.checks...)
			response := healthRequest(app, "/api/doctorchart/health")
			state := "DOWN"
			if tc.status == http.StatusOK {
				state = "UP"
			}
			if response.Code != tc.status || response.Body.String() != `{"service":"doctorchart","status":"`+state+`"}` {
				t.Fatalf("health status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), sensitive) || response.Header().Get("Content-Type") != "application/json" || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("readiness exposed dependency details or allowed caching")
			}
			ping := healthRequest(app, "/api/doctorchart/ping")
			if ping.Code != http.StatusOK || ping.Body.Len() != 0 {
				t.Fatal("dependency failure changed process liveness", ping.Code)
			}
		})
	}
}

func TestReadinessIsBoundedAndRecovers(t *testing.T) {
	var unavailable atomic.Bool
	unavailable.Store(true)
	var calls atomic.Int32
	app := healthFixture(func(ctx context.Context) error {
		calls.Add(1)
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Error("dependency check must have a one-second maximum deadline")
		}
		if unavailable.Load() {
			return errors.New("synthetic unavailable")
		}
		return nil
	})
	if response := healthRequest(app, "/api/doctorchart/health"); response.Code != http.StatusServiceUnavailable {
		t.Fatal("failed dependency reported ready", response.Code)
	}
	unavailable.Store(false)
	if response := healthRequest(app, "/api/doctorchart/health"); response.Code != http.StatusOK {
		t.Fatal("recovered dependency stayed unavailable", response.Code)
	}
	if calls.Load() != 2 {
		t.Fatal("readiness was not checked for every request", calls.Load())
	}
}

func TestReadinessPropagatesCancellation(t *testing.T) {
	app := healthFixture(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/doctorchart/health", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	started := time.Now()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || time.Since(started) > 500*time.Millisecond {
		t.Fatal("readiness ignored request cancellation", response.Code)
	}
}

func TestReadinessTimeoutIsOneSecond(t *testing.T) {
	app := healthFixture(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	started := time.Now()
	response := healthRequest(app, "/api/doctorchart/health")
	if response.Code != http.StatusServiceUnavailable || time.Since(started) > 2*time.Second {
		t.Fatal("readiness was not bounded", response.Code)
	}
}

func TestReadinessPreservesMethodAndAuthenticationBoundary(t *testing.T) {
	app := healthFixture(func(context.Context) error { return nil })
	for _, method := range []string{http.MethodHead, http.MethodPost} {
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest(method, "/api/doctorchart/health", nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatal("health added a public method", method, response.Code)
		}
	}
}

func TestReadinessDoesNotStartChecksAfterCancellation(t *testing.T) {
	called := false
	app := healthFixture(func(context.Context) error { called = true; return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/doctorchart/health", nil).WithContext(ctx)
	app.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || called {
		t.Fatal("canceled request started a dependency check or reported ready")
	}
}
