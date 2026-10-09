package billing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestReloadAndFailureRetention(t *testing.T) {
	var mu sync.Mutex
	payload := `{"BASIC":{"max_patients":77,"max_records_per_patient":"123"}}`
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
		t.Fatal("fallback/string conversion mismatch")
	}
	if rules.Int("FREE_TRIAL", "max_patients", 50) != 20 {
		t.Fatal("trial must override billing")
	}
	mu.Lock()
	payload = `{}`
	mu.Unlock()
	if err := rules.Reload(context.Background()); err != nil {
		t.Fatal(err)
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
func TestPlanAliases(t *testing.T) {
	for input, want := range map[string]string{"enterprise": "UNLIMITED", " pro ": "PRO", "unknown": "NONE", "": "NONE", "FREE_TRIAL": "FREE_TRIAL"} {
		if got := Plan(input); got != want {
			t.Fatalf("%q=%s, want %s", input, got, want)
		}
	}
}
