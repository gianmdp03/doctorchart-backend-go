package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGatewayIdentityBoundary(t *testing.T) {
	const secret = "fixture-gateway-secret-32-characters"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { id := User(r.Context()); WriteJSON(w, 200, id) })
	for _, test := range []struct {
		name, method, path, secret, user, role string
		want                                   int
		authenticated                          bool
	}{
		{"public identity ignored", "GET", "/api/doctorchart/health", secret, "attacker", "SUPER_ADMIN", 200, false},
		{"preflight identity ignored", "OPTIONS", "/private", secret, "attacker", "SUPER_ADMIN", 200, false},
		{"webhook own authentication", "POST", "/internal/webhooks/billing/limits-updated", "wrong", "attacker", "SUPER_ADMIN", 200, false},
		{"missing secret", "GET", "/private", "", "doctor", "DOCTOR", 401, false},
		{"wrong secret", "GET", "/private", "wrong", "doctor", "DOCTOR", 403, false},
		{"missing identity", "GET", "/private", secret, " ", "DOCTOR", 401, false},
		{"roles normalized", "GET", "/private", secret, " doctor ", "DOCTOR, ROLE_USER", 200, true},
		{"admin denied", "GET", "/api/doctorchart/admin/specialties", secret, "doctor", "DOCTOR", 403, false},
		{"admin role case sensitive", "GET", "/api/doctorchart/admin/specialties", secret, "doctor", "super_admin", 403, false},
		{"admin allowed", "GET", "/api/doctorchart/admin/specialties", secret, "doctor", "ROLE_SUPER_ADMIN", 200, true},
		{"head requires identity", "HEAD", "/api/doctorchart/health", "", "", "", 401, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("X-Gateway-Secret", test.secret)
			request.Header.Set("X-User-Id", test.user)
			request.Header.Set("X-User-Role", test.role)
			request.Header.Set("X-User-Plan", "PRO")
			request.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).Format(time.RFC3339Nano))
			recorder := httptest.NewRecorder()
			Gateway(secret, next).ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
			}
			if test.want == 200 {
				var id Identity
				if err := json.Unmarshal(recorder.Body.Bytes(), &id); err != nil {
					t.Fatal(err)
				}
				if (id.UserID != "") != test.authenticated {
					t.Fatalf("identity trust changed: %+v", id)
				}
			}
		})
	}
}

func TestDoctorChartBenefitBoundary(t *testing.T) {
	const secret = "fixture-gateway-secret-32-characters"
	future := time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	past := time.Now().Add(-time.Second).Format(time.RFC3339Nano)
	for _, test := range []struct {
		name, plan, expiry, role string
		want                     int
	}{
		{"valid", "PRO", future, "DOCTOR", 200},
		{"normalized", " basic ", future, "COMPANY", 200},
		{"trial", "FREE_TRIAL", future, "DOCTOR", 200},
		{"unlimited still expires", "UNLIMITED", past, "DOCTOR", 403},
		{"expired jwt benefit", "PRO", past, "DOCTOR", 403},
		{"missing tier", "", future, "DOCTOR", 403},
		{"invalid tier", "FAKE", future, "DOCTOR", 403},
		{"missing expiry", "PRO", "", "DOCTOR", 403},
		{"invalid expiry", "PRO", "tomorrow", "DOCTOR", 403},
		{"administrator", "", "", "SUPER_ADMIN", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/doctorchart/patients", nil)
			req.Header.Set("X-Gateway-Secret", secret)
			req.Header.Set("X-User-Id", "synthetic-doctor")
			req.Header.Set("X-User-Role", test.role)
			req.Header.Set("X-User-Plan", test.plan)
			req.Header.Set("X-User-Plan-Expires-At", test.expiry)
			called := false
			recorder := httptest.NewRecorder()
			Gateway(secret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(200) })).ServeHTTP(recorder, req)
			if recorder.Code != test.want || called != (test.want == 200) {
				t.Fatalf("status=%d reached=%v", recorder.Code, called)
			}
		})
	}
}
func TestJavaWhitespaceAtJSONBoundary(t *testing.T) {
	for _, test := range []struct {
		text    string
		blank   bool
		trimmed string
	}{
		{"\t abc\x00", false, "abc"},
		{"\u00a0", false, "\u00a0"},
		{"\u2007", false, "\u2007"},
		{"\u202f", false, "\u202f"},
		{"\u0085", false, "\u0085"},
		{"\u2003", true, "\u2003"},
		{"\x1c", true, ""},
	} {
		if JavaBlank(test.text) != test.blank || JavaTrim(test.text) != test.trimmed {
			t.Fatalf("text=%q blank=%v trim=%q", test.text, JavaBlank(test.text), JavaTrim(test.text))
		}
	}
}
func TestVersionNoNumericCoercion(t *testing.T) {
	for _, value := range []any{"1", json.Number("1.0"), json.Number("1e0"), json.Number("9223372036854775808"), true} {
		if _, err := Integer(value, "expectedVersion", 0); err == nil {
			t.Fatalf("accepted invalid integer %v", value)
		}
	}
	version, err := Integer(json.Number("9223372036854775807"), "expectedVersion", 0)
	if err != nil || version != 9223372036854775807 {
		t.Fatalf("BIGINT lost: %d %v", version, err)
	}
}
func TestUTF16LengthMatchesJava(t *testing.T) {
	if JavaLength("😀") != 2 {
		t.Fatal("Java length mismatch")
	}
}
