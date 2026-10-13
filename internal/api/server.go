package api

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/billing"
)

// BaseRoutes keeps process liveness independent from dependency readiness.
// Readiness requires exactly one context-aware check supplied by normal startup.
func BaseRoutes(mux *http.ServeMux, rules *billing.Rules, billingSecret string, readiness ...func(context.Context) error) {
	mux.HandleFunc("GET /api/doctorchart/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /api/doctorchart/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if len(readiness) != 1 || readiness[0] == nil || ctx.Err() != nil {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "DOWN", "service": "doctorchart"})
			return
		}
		if err := readiness[0](ctx); err != nil || ctx.Err() != nil {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "DOWN", "service": "doctorchart"})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "UP", "service": "doctorchart"})
	})
	mux.HandleFunc("GET /api/doctorchart/limits/all", func(w http.ResponseWriter, r *http.Request) {
		if err := rules.EnsureFresh(r.Context()); err != nil {
			w.WriteHeader(503)
			return
		}
		WriteJSON(w, 200, rules.All())
	})
	mux.HandleFunc("GET /api/doctorchart/limits/my-limits", func(w http.ResponseWriter, r *http.Request) {
		id := User(r.Context())
		if !id.HasRole("COMPANY") && !id.HasRole("SUPER_ADMIN") {
			w.Header().Set("Content-Type", "application/problem+json")
			WriteJSON(w, 403, map[string]any{"title": "Access Denied", "status": 403, "detail": "Access Denied", "instance": r.URL.Path, "timestamp": FormatInstant(time.Now().UTC())})
			return
		}
		plan := billing.Plan(id.Plan)
		if err := rules.EnsureFresh(r.Context()); err != nil {
			w.WriteHeader(503)
			return
		}
		WriteJSON(w, 200, map[string]any{"plan": plan, "limits": map[string]int{"max_patients": rules.Int(plan, "max_patients", 50), "max_records_per_patient": rules.Int(plan, "max_records_per_patient", 100)}})
	})
	webhook := func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(billingSecret) == "" {
			w.WriteHeader(503)
			return
		}
		incoming := r.Header.Get("X-Internal-Secret")
		if strings.TrimSpace(incoming) == "" || subtle.ConstantTimeCompare([]byte(incoming), []byte(billingSecret)) != 1 {
			w.WriteHeader(401)
			return
		}
		if err := rules.Reload(r.Context()); err != nil {
			slog.Warn("billing reload unavailable; current rules retained")
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}
	mux.HandleFunc("POST /internal/webhooks/billing/limits-updated", webhook)
	mux.HandleFunc("POST /api/doctorchart/internal/webhooks/billing/limits-updated", webhook)
}
