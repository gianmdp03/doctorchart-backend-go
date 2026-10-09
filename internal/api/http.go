package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Identity struct {
	UserID string
	Roles  []string
	Plan   string
}
type identityKey struct{}

func User(ctx context.Context) Identity { id, _ := ctx.Value(identityKey{}).(Identity); return id }
func (id Identity) HasRole(role string) bool {
	for _, r := range id.Roles {
		if r == "ROLE_"+role {
			return true
		}
	}
	return false
}

type FieldError struct {
	Path string `json:"path"`
	Code string `json:"code"`
}
type Error struct {
	Status  int          `json:"-"`
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Errors  []FieldError `json:"errors"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func Failure(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message, Errors: []FieldError{}}
}
func Invalid(message string) *Error { return Failure(400, "INVALID_REQUEST", message) }

func WriteJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		slog.Error("encode response", "error", err)
		WriteError(w, Failure(500, "INTERNAL_ERROR", "No se pudo completar la solicitud."))
		return
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		slog.Debug("response write failed", "error", err)
	}
}
func WriteError(w http.ResponseWriter, err error) {
	var failure *Error
	if !errors.As(err, &failure) {
		slog.Error("request failed", "errorType", fmt.Sprintf("%T", err))
		failure = Failure(500, "INTERNAL_ERROR", "No se pudo completar la solicitud.")
	}
	WriteJSON(w, failure.Status, failure)
}

func Public(method, path string) bool {
	return method == "OPTIONS" || method == "GET" && (path == "/api/doctorchart/ping" || path == "/api/doctorchart/health") || method == "POST" && (path == "/internal/webhooks/billing/limits-updated" || path == "/api/doctorchart/internal/webhooks/billing/limits-updated")
}

func Gateway(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match Spring Security's default headers, including errors.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		if Public(r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		securityError := func(err error) { w.Header().Set("Content-Type", "application/json;charset=UTF-8"); WriteError(w, err) }
		if JavaBlank(secret) {
			securityError(Failure(503, "AUTHENTICATION_UNAVAILABLE", "La autenticación no está disponible."))
			return
		}
		incoming := r.Header.Get("X-Gateway-Secret")
		if JavaBlank(incoming) {
			securityError(Failure(401, "UNAUTHENTICATED", "Se requiere autenticación."))
			return
		}
		if subtle.ConstantTimeCompare([]byte(incoming), []byte(secret)) != 1 {
			securityError(Failure(403, "FORBIDDEN", "Acceso denegado."))
			return
		}
		user := r.Header.Get("X-User-Id")
		if JavaBlank(user) {
			securityError(Failure(401, "UNAUTHENTICATED", "Se requiere autenticación."))
			return
		}
		id := Identity{UserID: JavaTrim(user), Plan: JavaTrim(r.Header.Get("X-User-Plan"))}
		for _, role := range strings.Split(r.Header.Get("X-User-Role"), ",") {
			role = JavaTrim(role)
			if !JavaBlank(role) {
				if !strings.HasPrefix(role, "ROLE_") {
					role = "ROLE_" + role
				}
				id.Roles = append(id.Roles, role)
			}
		}
		if (r.URL.Path == "/api/doctorchart/admin" || strings.HasPrefix(r.URL.Path, "/api/doctorchart/admin/")) && !id.HasRole("SUPER_ADMIN") {
			securityError(Failure(403, "FORBIDDEN", "Acceso denegado."))
			return
		}
		// The gateway resolves DoctorChart's own active, time-bounded benefit.
		// An empty/invalid benefit must never fall back to paid BASIC quotas.
		if !id.HasRole("SUPER_ADMIN") {
			id.Plan = strings.ToUpper(strings.TrimSpace(id.Plan))
			expires, err := time.Parse(time.RFC3339Nano, r.Header.Get("X-User-Plan-Expires-At"))
			validTier := false
			switch id.Plan {
			case "FREE_TRIAL", "BASIC", "PRO", "UNLIMITED", "ENTERPRISE":
				validTier = true
			}
			if !validTier || err != nil || !expires.After(time.Now()) {
				securityError(Failure(403, "DOCTORCHART_PLAN_REQUIRED", "Tu acceso a DoctorChart no está vigente. Revisá tu suscripción."))
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}

// Dispatch preserves Spring's route errors and implicit OPTIONS responses while
// keeping net/http responsible for route parameters and method-specific handlers.
func Dispatch(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		methods := []string{}
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH"} {
			probe := new(http.Request)
			*probe = *r
			probe.Method = method
			if _, pattern := mux.Handler(probe); pattern != "" {
				methods = append(methods, method)
			}
		}
		if len(methods) == 0 {
			w.Header().Set("Content-Type", "application/problem+json")
			WriteJSON(w, 404, map[string]any{"detail": "No se encontró el recurso.", "instance": r.URL.Path, "status": 404, "title": "Not Found"})
			return
		}
		if r.Method == "OPTIONS" {
			allow := strings.Join(append(methods, "OPTIONS"), ",")
			if r.Header.Get("Origin") != "" && r.Header.Get("Access-Control-Request-Method") != "" {
				allow = "GET, HEAD, POST, PUT, DELETE, OPTIONS, TRACE, PATCH"
			}
			w.Header().Set("Allow", allow)
			w.WriteHeader(200)
			return
		}
		WriteError(w, Failure(405, "METHOD_NOT_ALLOWED", "La operación no está disponible en esta ruta."))
	})
}

func Decode(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 26<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, Invalid("La solicitud contiene datos inválidos.")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, Invalid("La solicitud contiene datos inválidos.")
	}
	return value, nil
}
