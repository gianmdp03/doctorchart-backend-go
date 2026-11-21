package clinics

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperatorBookingChangeReason(t *testing.T) {
	cases := []struct {
		name   string
		op     string
		body   map[string]any
		want   string
		reject bool
	}{
		{"cancellation", "status", map[string]any{"expectedVersion": "1", "status": "CANCELLED", "reason": "  Cambio de agenda.\r\nGracias.  "}, "Cambio de agenda.\nGracias.", false},
		{"reschedule", "reschedule", map[string]any{"expectedVersion": "1", "clinicId": "1", "start": "2030-01-10T14:00:00Z", "reason": "Cambio de disponibilidad"}, "Cambio de disponibilidad", false},
		{"older client", "status", map[string]any{"expectedVersion": "1", "status": "CANCELLED"}, "", false},
		{"blank reason", "status", map[string]any{"expectedVersion": "1", "status": "CANCELLED", "reason": "  "}, "", true},
		{"too long", "status", map[string]any{"expectedVersion": "1", "status": "CANCELLED", "reason": strings.Repeat("a", 501)}, "", true},
		{"check-in", "status", map[string]any{"expectedVersion": "1", "status": "CHECKED_IN", "reason": "Cambio"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/booking", strings.NewReader(string(body)))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", "operator-reason-test")
			request, _, _, err := decodeBooking(httptest.NewRecorder(), r, tc.op)
			if (err != nil) != tc.reject {
				t.Fatalf("unexpected validation result: %v", err)
			}
			if tc.want != "" && (request.Reason == nil || *request.Reason != tc.want) {
				t.Fatal("operator reason was not passed to the booking mutation")
			}
		})
	}
}
