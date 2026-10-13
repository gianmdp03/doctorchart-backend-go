package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/billing"
)

func TestLimitsCallbackAcknowledgesOnlySuccessfulReload(t *testing.T) {
	var mode atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 1:
			w.WriteHeader(503)
		case 2:
			w.Write([]byte(`{}`))
		case 3:
			w.Write([]byte(`{"BASIC":{"max_patients":"12"}}`))
		case 4:
			w.Write([]byte(`{"BASIC":{"max_patients":12,"max_records_per_patient":9}}`))
		default:
			w.Write([]byte(`{"BASIC":{"max_patients":7,"max_records_per_patient":9}}`))
		}
	}))
	defer remote.Close()
	rules := billing.New(remote.URL)
	rules.Load(context.Background())
	mux := http.NewServeMux()
	BaseRoutes(mux, rules, "fixture-billing-secret")
	for _, path := range []string{"/internal/webhooks/billing/limits-updated", "/api/doctorchart/internal/webhooks/billing/limits-updated"} {
		for _, fail := range []int32{1, 2, 3} {
			mode.Store(fail)
			r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
			r.Header.Set("X-Internal-Secret", "fixture-billing-secret")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 503 {
				t.Errorf("reload failure mode%d falsely ACKed: %d", fail, w.Code)
			}
			if rules.Int("BASIC", "max_patients", 50) != 7 {
				t.Error("failed reload replaced good snapshot")
			}
		}
		mode.Store(4)
		r := httptest.NewRequest("POST", path, nil)
		r.Header.Set("X-Internal-Secret", "wrong")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("wrong secret admitted")
		}
		r.Header.Set("X-Internal-Secret", "fixture-billing-secret")
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 || rules.Int("BASIC", "max_patients", 50) != 12 {
			t.Fatal("recovery not acknowledged")
		}
		mode.Store(0)
		if err := rules.Reload(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}
