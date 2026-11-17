package workspace_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/testfixture"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

const reviewRoot = "/api/doctorchart/admin/specialties"
const fixtureReviewer = "isolated-reviewer"

func reviewRequest(s *workspace.Store) func(string, string, string, string, any) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	s.Routes(mux)
	handler := api.Gateway("template-fixture", api.Dispatch(mux))
	return func(method, path, actor, role string, payload any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("X-Gateway-Secret", "template-fixture")
		r.Header.Set("X-User-Id", actor)
		r.Header.Set("X-User-Role", role)
		r.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
}

func checkReviewResponse(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || !strings.Contains(response.Body.String(), code) {
		t.Fatalf("expected %d %s, got %d %s", status, code, response.Code, response.Body)
	}
}

func TestTemplateReviewHTTPApprovalLifecycle(t *testing.T) {
	s, pool := contextFixture(t)
	authority, err := workspace.NewReviewAuthority("", fixtureReviewer)
	if err != nil {
		t.Fatal(err)
	}
	request := reviewRequest(s.WithReviewAuthority(authority))
	admin := func(method, path string, payload any) *httptest.ResponseRecorder {
		return request(method, path, "technical-admin", "SUPER_ADMIN", payload)
	}
	reviewer := func(method, path string, payload any) *httptest.ResponseRecorder {
		return request(method, path, fixtureReviewer, "SUPER_ADMIN", payload)
	}
	ctx := t.Context()
	var raw []byte
	if err := pool.QueryRow(ctx, "SELECT published_definition FROM wizard_defaults w JOIN specialties s ON s.id=w.specialty_id WHERE s.code='general_medicine'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	create := func(code string) (int64, string) {
		t.Helper()
		response := admin("POST", reviewRoot, map[string]any{"code": code, "name": code, "definition": json.RawMessage(raw)})
		checkReviewResponse(t, response, 201, "")
		var specialty struct{ ID int64 }
		if err := json.Unmarshal(response.Body.Bytes(), &specialty); err != nil || specialty.ID <= 0 {
			t.Fatal("missing specialty identity", err)
		}
		return specialty.ID, fmt.Sprintf("%s/%d", reviewRoot, specialty.ID)
	}
	id, root := create("review_fixture")
	_, other := create("review_other")
	reviews, publish := root+"/wizard/reviews", root+"/wizard/publish"
	version := func(x int) map[string]int { return map[string]int{"expectedVersion": x} }
	checkReviewResponse(t, admin("POST", publish, version(0)), 409, "TEMPLATE_APPROVAL_REQUIRED")
	checkReviewResponse(t, admin("POST", reviews, version(0)), 403, "TEMPLATE_REVIEW_NOT_AUTHORIZED")
	checkReviewResponse(t, request("POST", reviews, fixtureReviewer, "DOCTOR", version(0)), 403, "")
	checkReviewResponse(t, reviewer("POST", reviews, map[string]any{"expectedVersion": 0, "fingerprint": "forged"}), 400, "")
	checkReviewResponse(t, reviewer("POST", reviews, version(0)), 204, "")
	var approval int64
	if err := pool.QueryRow(ctx, "SELECT id FROM wizard_approvals WHERE specialty_id=$1", id).Scan(&approval); err != nil {
		t.Fatal(err)
	}
	checkReviewResponse(t, admin("POST", other+"/wizard/publish", map[string]any{"expectedVersion": 0, "approvalId": approval}), 400, "")
	checkReviewResponse(t, admin("POST", other+"/wizard/publish", version(0)), 409, "TEMPLATE_APPROVAL_REQUIRED")
	checkReviewResponse(t, admin("POST", fmt.Sprintf("%s/wizard/reviews/%d/revoke", other, approval), map[string]string{"reason": "Wrong specialty"}), 409, "TEMPLATE_REVOCATION_UNAVAILABLE")
	checkReviewResponse(t, admin("POST", publish, version(0)), 200, "")
	selection := map[string]any{"expectedVersion": nil, "specialtyId": number(id)}
	personal, err := s.Select(ctx, "doctor-review-owner", selection)
	if err != nil {
		t.Fatal(err)
	}
	personal, err = s.Edit(ctx, "doctor-review-owner", "finish", map[string]any{"specialtyContextId": number(personal.ContextID), "expectedVersion": number(personal.Version)})
	if err != nil {
		t.Fatal(err)
	}

	checkReviewResponse(t, admin("PUT", root+"/experience/patient-record-actions", map[string]any{"expectedVersion": 1, "patientRecordActions": []string{}}), 200, "")
	checkReviewResponse(t, admin("POST", publish, version(2)), 409, "TEMPLATE_APPROVAL_REQUIRED")
	pending, err := s.Select(ctx, "doctor-prior-publication", selection)
	if err != nil {
		t.Fatal("new draft disabled prior publication", err)
	}
	saved, err := s.Setup(ctx, "doctor-review-owner")
	if err != nil || len(saved.Experience.Actions) == 0 {
		t.Fatal("draft changed published experience", err)
	}
	checkReviewResponse(t, reviewer("POST", reviews, version(2)), 204, "")

	// Whichever side wins, the revision edited concurrently cannot be published
	// by using the preceding revision's approval.
	form, err := wizard.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	form["modulesConfig"].(map[string]any)["AI_ASSISTANT"] = !form["modulesConfig"].(map[string]any)["AI_ASSISTANT"].(bool)
	var replies [3]int
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); replies[0] = admin("POST", publish, version(2)).Code }()
	go func() {
		defer wg.Done()
		replies[1] = admin("PUT", root+"/wizard/draft", map[string]any{"expectedVersion": 2, "definition": form}).Code
	}()
	go func() { defer wg.Done(); replies[2] = reviewer("POST", reviews, version(2)).Code }()
	wg.Wait()
	if !((replies[0] == 200 && replies[1] == 409) || (replies[0] == 409 && replies[1] == 200)) || replies[2] != 409 {
		t.Fatalf("unexpected concurrent results %v", replies)
	}
	var versionNow, publishedApproval int64
	if err := pool.QueryRow(ctx, "SELECT version,published_approval_id FROM wizard_defaults WHERE specialty_id=$1", id).Scan(&versionNow, &publishedApproval); err != nil {
		t.Fatal(err)
	}
	checkReviewResponse(t, admin("POST", fmt.Sprintf("%s/%d/revoke", reviews, publishedApproval), map[string]string{"reason": "Controlled laboratory revocation"}), 204, "")
	checkReviewResponse(t, admin("GET", reviews, nil), 200, "Controlled laboratory revocation")
	checkReviewResponse(t, admin("POST", publish, version(int(versionNow))), 409, "TEMPLATE_APPROVAL_REQUIRED")
	_, err = s.Select(ctx, "doctor-after-revocation", selection)
	assertFailure(t, err, 409, "SPECIALTY_NOT_AVAILABLE")
	_, err = s.Edit(ctx, "doctor-prior-publication", "finish", map[string]any{
		"specialtyContextId": number(pending.ContextID), "expectedVersion": number(pending.Version),
	})
	assertFailure(t, err, 409, "TEMPLATE_APPROVAL_REQUIRED")
	var metadataVersion int64
	if err := pool.QueryRow(ctx, "SELECT version FROM specialties WHERE id=$1", id).Scan(&metadataVersion); err != nil {
		t.Fatal(err)
	}
	checkReviewResponse(t, admin("PATCH", root, map[string]any{"expectedVersion": metadataVersion, "active": false}), 200, "")
	checkReviewResponse(t, admin("PATCH", root, map[string]any{"expectedVersion": metadataVersion + 1, "active": true}), 409, "TEMPLATE_APPROVAL_REQUIRED")
	historical, err := s.Setup(ctx, "doctor-review-owner")
	if err != nil || historical.ContextID != personal.ContextID || !bytes.Equal(historical.Definition, personal.Definition) {
		t.Fatal("revocation or deactivation changed personal history", err)
	}
	_, err = s.Setup(api.WithSpecialtyContext(ctx, personal.ContextID), "technical-admin")
	assertFailure(t, err, 404, "SPECIALTY_CONTEXT_NOT_FOUND")
}

func TestLegacyAndProvisionalTemplatesCannotUseSyntheticApprovalsOutsideLab(t *testing.T) {
	s, pool := templateContextFixture(t, false)
	ctx := t.Context()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wizard_defaults WHERE published_version=1 AND published_approval_id IS NULL").Scan(&count); err != nil || count != 3 {
		t.Fatal("initial publications not preserved", count, err)
	}
	var restricted bool
	if err := pool.QueryRow(ctx, `SELECT bool_and(
		has_table_privilege('doctorchart_app', relation, 'SELECT')
		AND has_table_privilege('doctorchart_app', relation, 'INSERT')
		AND NOT has_table_privilege('doctorchart_app', relation, 'UPDATE,DELETE,TRUNCATE')
		AND NOT has_table_privilege('doctorchart_media', relation, 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE'))
		FROM unnest(ARRAY['public.wizard_approvals','public.wizard_approval_revocations']) relation`).Scan(&restricted); err != nil || !restricted {
		t.Fatal("review records are not append-only for the application", err)
	}
	if err := testfixture.ApprovePublishedTemplates(ctx, pool); err != nil {
		t.Fatal(err)
	}
	available, err := s.Specialties(ctx, false)
	assertFailure(t, err, 409, "SPECIALTY_NOT_AVAILABLE")
	if len(available) != 0 {
		t.Fatal("synthetic approvals were used outside lab", available)
	}
	authority, err := workspace.NewReviewAuthority("", fixtureReviewer)
	if err != nil {
		t.Fatal(err)
	}
	request := reviewRequest(s.WithReviewAuthority(authority))
	files, err := filepath.Glob("../../templates/*.provisional.json")
	if err != nil || len(files) != 16 {
		t.Fatalf("expected 16 provisional artifacts, got %v: %v", files, err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var template struct {
			Status          string
			CreateSpecialty json.RawMessage
		}
		if err := json.Unmarshal(raw, &template); err != nil || template.Status != "PROVISIONAL_TEST_ONLY" {
			t.Fatalf("provisional artifact %s: %v", file, err)
		}
		response := request("POST", reviewRoot, fixtureReviewer, "SUPER_ADMIN", template.CreateSpecialty)
		checkReviewResponse(t, response, 201, "")
		var specialty struct{ ID int64 }
		if err := json.Unmarshal(response.Body.Bytes(), &specialty); err != nil {
			t.Fatal(err)
		}
		root := fmt.Sprintf("%s/%d/wizard", reviewRoot, specialty.ID)
		checkReviewResponse(t, request("POST", root+"/reviews", fixtureReviewer, "SUPER_ADMIN", map[string]int{"expectedVersion": 0}), 403, "TEMPLATE_REVIEW_NOT_AUTHORIZED")
		checkReviewResponse(t, request("POST", root+"/publish", fixtureReviewer, "SUPER_ADMIN", map[string]int{"expectedVersion": 0}), 409, "TEMPLATE_APPROVAL_REQUIRED")
	}
}
