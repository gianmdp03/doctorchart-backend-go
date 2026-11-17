package workspace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestExternalTemplateEvidenceBindsReviewerAndDraft(t *testing.T) {
	const actor = "external-review-operator-fixture"
	draft := reviewDraft{Code: "test_specialty", Revision: 5, Fingerprint: strings.Repeat("a", 64)}
	entry := reviewEvidence{Specialty: draft.Code, Revision: draft.Revision, Fingerprint: draft.Fingerprint, Reviewer: actor, Reference: "external-evidence-fixture"}
	manifest, err := json.Marshal([]reviewEvidence{entry})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewReviewAuthority(string(manifest), "")
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{reviews: authority}
	for _, test := range []struct {
		name, user, role string
		draft            reviewDraft
		allowed          bool
	}{
		{"exact evidence", actor, "SUPER_ADMIN", draft, true},
		{"technical admin", "different-operator", "SUPER_ADMIN", draft, false},
		{"missing admin role", actor, "DOCTOR", draft, false},
		{"another specialty", actor, "SUPER_ADMIN", reviewDraft{Code: "another_specialty", Revision: draft.Revision, Fingerprint: draft.Fingerprint}, false},
		{"new revision", actor, "SUPER_ADMIN", reviewDraft{Code: draft.Code, Revision: 6, Fingerprint: draft.Fingerprint}, false},
		{"different content", actor, "SUPER_ADMIN", reviewDraft{Code: draft.Code, Revision: draft.Revision, Fingerprint: strings.Repeat("b", 64)}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := api.Gateway("review-fixture", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				kind, reference, err := store.reviewEvidence(r.Context(), nil, test.draft)
				if err != nil || (kind == "CLINICAL" && reference == entry.Reference) != test.allowed {
					t.Fatalf("evidence result %q %q %v", kind, reference, err)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest("GET", "/api/doctorchart/workspace", nil)
			request.Header.Set("X-Gateway-Secret", "review-fixture")
			request.Header.Set("X-User-Id", test.user)
			request.Header.Set("X-User-Role", test.role)
			request.Header.Set("X-User-Plan", "BASIC")
			request.Header.Set("X-User-Plan-Expires-At", "2099-01-01T00:00:00Z")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("identity fixture failed: %d %s", response.Code, response.Body)
			}
		})
	}
}
