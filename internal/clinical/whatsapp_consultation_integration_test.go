package clinical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestWhatsAppConsultationsLifecycleIsolationAndIntegrity(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	s := services[0]
	patientA := integrityPatient(t, s)
	reqB := integrityPatientRequest()
	reqB["values"].(map[string]any)["sys_dni"] = "87654321"
	reqB["values"].(map[string]any)["sys_firstName"] = "Second"
	patientB, err := s.CreatePatient(t.Context(), api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, reqB)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	var otherContext int64
	if err := pool.QueryRow(ctx, `SELECT c.id FROM doctor_specialty_contexts c
		JOIN doctor_workspaces w ON w.id=c.workspace_id WHERE w.user_id='integrity-other'`).Scan(&otherContext); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	s.Routes(mux)
	s.EncounterRoutes(mux)
	s.PrescriptionRoutes(mux)
	s.WhatsAppConsultationRoutes(mux)
	handler := api.Gateway("wa-test", api.Dispatch(mux))

	do := func(method, path, actor, role string, scope int64, body any) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, fmt.Sprintf("/api/doctorchart%s?specialtyContextId=%d", path, scope), reader)
		req.Header.Set("X-Gateway-Secret", "wa-test")
		req.Header.Set("X-User-Id", actor)
		req.Header.Set("X-User-Role", role)
		req.Header.Set("X-User-Plan", "BASIC")
		req.Header.Set("X-User-Plan-Expires-At", "2099-01-01T00:00:00Z")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	baseA := fmt.Sprintf("/patients/%d/whatsapp-consultations", patientA.ID)
	baseB := fmt.Sprintf("/patients/%d/whatsapp-consultations", patientB.ID)

	// 1. Empty initial state
	res := do(http.MethodGet, baseA, "integrity-owner", "COMPANY", patientA.ContextID, nil)
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != "[]" {
		t.Fatalf("expected empty list, got %d: %s", res.Code, res.Body.String())
	}

	// 2. Reject administrative / secretary roles
	for _, role := range []string{"SECRETARY", "ADMIN_STAFF"} {
		if got := do(http.MethodGet, baseA, "integrity-owner", role, patientA.ContextID, nil); got.Code != http.StatusForbidden {
			t.Fatalf("role %s should be forbidden on GET, got %d", role, got.Code)
		}
		if got := do(http.MethodPost, baseA, "integrity-owner", role, patientA.ContextID, map[string]any{
			"consultedAt": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
			"content":     "Intento no autorizado",
		}); got.Code != http.StatusForbidden {
			t.Fatalf("role %s should be forbidden on POST, got %d", role, got.Code)
		}
	}

	// 3. Reject missing/invalid payload fields
	for _, invalid := range []map[string]any{
		{"consultedAt": "", "content": "Texto"},
		{"consultedAt": "not-a-date", "content": "Texto"},
		{"consultedAt": time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339), "content": "Futura"},
		{"consultedAt": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), "content": "   \n\t  "},
		{"consultedAt": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), "content": strings.Repeat("a", maxWhatsAppContentLength+1)},
		{"consultedAt": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), "content": "Ok", "extra": "nope"},
	} {
		if got := do(http.MethodPost, baseA, "integrity-owner", "COMPANY", patientA.ContextID, invalid); got.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid payload %v, got %d (%s)", invalid, got.Code, got.Body.String())
		}
	}

	// 4. Create newer first, then older retroactive, and a tie on consultedAt
	tNew := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	tOld := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	tMid := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)

	multiline := "Paciente consulta por tos hace 3 días.\r\n\r\nIndicaciones:\r\n- Hidratación\r\n- Control si persiste fiebre"
	resNew := do(http.MethodPost, baseA, "integrity-owner", "COMPANY", patientA.ContextID, map[string]any{
		"consultedAt": tNew.Format(time.RFC3339),
		"content":     multiline,
	})
	if resNew.Code != http.StatusCreated {
		t.Fatalf("create newest failed: %d %s", resNew.Code, resNew.Body.String())
	}
	var itemNew WhatsAppConsultation
	if err := json.Unmarshal(resNew.Body.Bytes(), &itemNew); err != nil {
		t.Fatal(err)
	}
	if itemNew.Content != strings.ReplaceAll(multiline, "\r\n", "\n") {
		t.Fatalf("unexpected content normalization: %q", itemNew.Content)
	}

	// Retroactive older consultation
	resOld := do(http.MethodPost, baseA, "integrity-owner", "COMPANY", patientA.ContextID, map[string]any{
		"consultedAt": tOld.Format(time.RFC3339),
		"content":     "Consulta anterior registrada después",
	})
	if resOld.Code != http.StatusCreated {
		t.Fatalf("create oldest failed: %d %s", resOld.Code, resOld.Body.String())
	}
	var itemOld WhatsAppConsultation
	_ = json.Unmarshal(resOld.Body.Bytes(), &itemOld)

	// Two consultations with the exact same consultedAt
	resMid1 := do(http.MethodPost, baseA, "integrity-owner", "COMPANY", patientA.ContextID, map[string]any{
		"consultedAt": tMid.Format(time.RFC3339),
		"content":     "Empate primero",
	})
	resMid2 := do(http.MethodPost, baseA, "integrity-owner", "COMPANY", patientA.ContextID, map[string]any{
		"consultedAt": tMid.Format(time.RFC3339),
		"content":     "Empate segundo",
	})
	if resMid1.Code != http.StatusCreated || resMid2.Code != http.StatusCreated {
		t.Fatalf("tie creates failed: %d / %d", resMid1.Code, resMid2.Code)
	}
	var itemMid1, itemMid2 WhatsAppConsultation
	_ = json.Unmarshal(resMid1.Body.Bytes(), &itemMid1)
	_ = json.Unmarshal(resMid2.Body.Bytes(), &itemMid2)

	// 5. Verify descending chronological order by consultedAt DESC, id DESC
	listRes := do(http.MethodGet, baseA, "integrity-owner", "COMPANY", patientA.ContextID, nil)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list failed: %d", listRes.Code)
	}
	var listed []WhatsAppConsultation
	if err := json.Unmarshal(listRes.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 4 {
		t.Fatalf("expected 4 items, got %d", len(listed))
	}
	if listed[0].ID != itemNew.ID || listed[1].ID != itemMid2.ID || listed[2].ID != itemMid1.ID || listed[3].ID != itemOld.ID {
		t.Fatalf("unexpected chronological order: %+v", listed)
	}

	// 6. Patient and context isolation
	if got := do(http.MethodGet, baseB, "integrity-owner", "COMPANY", patientB.ContextID, nil); got.Code != http.StatusOK || strings.TrimSpace(got.Body.String()) != "[]" {
		t.Fatalf("patient B leaked patient A records: %d %s", got.Code, got.Body.String())
	}
	if got := do(http.MethodGet, fmt.Sprintf("%s/%d", baseB, itemNew.ID), "integrity-owner", "COMPANY", patientB.ContextID, nil); got.Code != http.StatusNotFound {
		t.Fatalf("cross-patient read should be 404, got %d", got.Code)
	}
	if got := do(http.MethodGet, baseA, "integrity-other", "COMPANY", patientA.ContextID, nil); got.Code != http.StatusNotFound {
		t.Fatalf("foreign owner same context should be 404, got %d", got.Code)
	}
	if got := do(http.MethodGet, baseA, "integrity-other", "COMPANY", otherContext, nil); got.Code != http.StatusNotFound {
		t.Fatalf("foreign owner other context should be 404, got %d", got.Code)
	}
	if got := do(http.MethodGet, baseA, "integrity-owner", "COMPANY", otherContext, nil); got.Code != http.StatusNotFound {
		t.Fatalf("wrong context should be 404, got %d", got.Code)
	}

	// 7. Correction with optimistic locking and clinical_revisions
	staleUpdate := do(http.MethodPut, fmt.Sprintf("%s/%d", baseA, itemNew.ID), "integrity-owner", "COMPANY", patientA.ContextID, map[string]any{
		"expectedVersion": 99,
		"consultedAt":     tNew.Format(time.RFC3339),
		"content":         "Stale",
	})
	if staleUpdate.Code != http.StatusConflict {
		t.Fatalf("expected 409 on stale version, got %d", staleUpdate.Code)
	}

	updRes := do(http.MethodPut, fmt.Sprintf("%s/%d", baseA, itemNew.ID), "integrity-owner", "COMPANY", patientA.ContextID, map[string]any{
		"expectedVersion": itemNew.Version,
		"consultedAt":     tNew.Format(time.RFC3339),
		"content":         "Contenido corregido\nSegundo párrafo",
		"reason":          "Aclaración clínica",
	})
	if updRes.Code != http.StatusOK {
		t.Fatalf("update failed: %d %s", updRes.Code, updRes.Body.String())
	}
	var updated WhatsAppConsultation
	_ = json.Unmarshal(updRes.Body.Bytes(), &updated)
	if updated.Version != 1 || updated.Content != "Contenido corregido\nSegundo párrafo" {
		t.Fatalf("unexpected updated item: %+v", updated)
	}

	// 8. Annulment requires reason, creates clinical_revisions, hides from active list
	noReasonDel := do(http.MethodDelete, fmt.Sprintf("%s/%d", baseA, itemOld.ID), "integrity-owner", "COMPANY", patientA.ContextID, map[string]any{
		"reason":          " ",
		"expectedVersion": itemOld.Version,
	})
	if noReasonDel.Code != http.StatusBadRequest {
		t.Fatalf("annul without reason should fail 400, got %d", noReasonDel.Code)
	}

	delRes := do(http.MethodDelete, fmt.Sprintf("%s/%d", baseA, itemOld.ID), "integrity-owner", "COMPANY", patientA.ContextID, map[string]any{
		"reason":          "Registro cargado en paciente equivocado",
		"expectedVersion": itemOld.Version,
	})
	if delRes.Code != http.StatusNoContent {
		t.Fatalf("annul failed: %d %s", delRes.Code, delRes.Body.String())
	}

	// Verify revisions endpoint exposes WHATSAPP_CONSULTATION revisions
	revRes := do(http.MethodGet, fmt.Sprintf("/patients/%d/revisions", patientA.ID), "integrity-owner", "COMPANY", patientA.ContextID, nil)
	if revRes.Code != http.StatusOK {
		t.Fatalf("revisions failed: %d", revRes.Code)
	}
	if !strings.Contains(revRes.Body.String(), `"WHATSAPP_CONSULTATION"`) {
		t.Fatalf("expected WHATSAPP_CONSULTATION in revisions, got %s", revRes.Body.String())
	}

	// 9. Verify audit events recorded
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM clinical_access_events
		WHERE patient_id=$1 AND operation LIKE 'WHATSAPP_CONSULTATION%' AND outcome='AUTHORIZED'`, patientA.ID).Scan(&auditCount); err != nil || auditCount < 4 {
		t.Fatalf("expected >=4 authorized WhatsApp audit events, got %d (err=%v)", auditCount, err)
	}

	// 10. Verify zero side-effect rows in bookings, clinical_encounters, or patient_prescription_records
	var bookingsCount, encountersCount, prescriptionsCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM bookings WHERE patient_id=$1),
		(SELECT count(*) FROM clinical_encounters WHERE patient_id=$1),
		(SELECT count(*) FROM patient_prescription_records WHERE patient_id=$1)`, patientA.ID).Scan(&bookingsCount, &encountersCount, &prescriptionsCount); err != nil {
		t.Fatal(err)
	}
	if bookingsCount != 0 || encountersCount != 0 || prescriptionsCount != 0 {
		t.Fatalf("unexpected side effects: bookings=%d encounters=%d prescriptions=%d", bookingsCount, encountersCount, prescriptionsCount)
	}

	// 11. Annulling the patient cascades to active WhatsApp consultations
	if err := s.annulPatient(ctx, "integrity-owner", patientA.ID, annulment{reason: "Cierre de ficha", expected: &patientA.Version}); err != nil {
		t.Fatalf("annulPatient failed: %v", err)
	}
	var remainingActive int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM patient_whatsapp_consultations WHERE patient_id=$1 AND annulled_at IS NULL`, patientA.ID).Scan(&remainingActive); err != nil || remainingActive != 0 {
		t.Fatalf("expected 0 active WhatsApp consultations after patient annulment, got %d (err=%v)", remainingActive, err)
	}
}
