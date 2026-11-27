package workspace_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestEncounterTemplatesLifecycleIsolationAndLazyBackfill(t *testing.T) {
	store, pool := contextFixture(t)
	ctx := t.Context()

	var pediatricsID, generalID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code='pediatrics'").Scan(&pediatricsID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code='general_medicine'").Scan(&generalID); err != nil {
		t.Fatal(err)
	}

	// 1. Pediatrics initialization seeds 3 defaults.
	setupPed, err := store.Select(ctx, "doctor-a", map[string]any{"specialtyId": json.Number(fmt.Sprint(pediatricsID)), "expectedVersion": nil})
	if err != nil {
		t.Fatal(err)
	}
	ctxPed := api.WithSpecialtyContext(ctx, setupPed.ContextID)
	listPed, err := store.ListEncounterTemplates(ctxPed, "doctor-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(listPed.Templates) != 3 {
		t.Fatalf("expected 3 pediatric templates, got %d", len(listPed.Templates))
	}

	// 2. Copy independence: copy first template, edit copy, verify source unchanged.
	source := listPed.Templates[0]
	copied, err := store.CreateEncounterTemplate(ctxPed, "doctor-a", map[string]any{
		"name":             "Control Neonatal Especial",
		"sourceTemplateId": json.Number(fmt.Sprint(source.ID)),
	})
	if err != nil {
		t.Fatal(err)
	}
	var copiedForm map[string]any
	if err := json.Unmarshal(copied.FormDefinition, &copiedForm); err != nil {
		t.Fatal(err)
	}
	cats := copiedForm["categories"].([]any)
	firstCat := cats[0].(map[string]any)
	firstCat["title"] = "Motivo Neonatal Editado"
	updatedCopy, err := store.UpdateEncounterTemplate(ctxPed, "doctor-a", copied.ID, map[string]any{
		"expectedVersion": json.Number(fmt.Sprint(copied.Version)),
		"name":            "Control Neonatal Especial v2",
		"formDefinition":  copiedForm,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updatedCopy.Name != "Control Neonatal Especial v2" {
		t.Fatalf("unexpected updated name: %s", updatedCopy.Name)
	}
	afterList, err := store.ListEncounterTemplates(ctxPed, "doctor-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range afterList.Templates {
		if tpl.ID == source.ID && string(tpl.FormDefinition) != string(source.FormDefinition) {
			t.Fatal("modifying copied template altered source template")
		}
	}

	// 3. Archive & restore & LAST_ACTIVE_TEMPLATE protection.
	for i := 0; i < 3; i++ {
		target := afterList.Templates[i]
		if _, err := store.ArchiveEncounterTemplate(ctxPed, "doctor-a", target.ID, map[string]any{
			"expectedVersion": json.Number(fmt.Sprint(target.Version)),
		}); err != nil {
			t.Fatalf("archive #%d failed: %v", i, err)
		}
	}
	remainingList, err := store.ListEncounterTemplates(ctxPed, "doctor-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(remainingList.Templates) != 1 || len(remainingList.ArchivedTemplates) != 3 {
		t.Fatalf("expected 1 active & 3 archived, got %d / %d", len(remainingList.Templates), len(remainingList.ArchivedTemplates))
	}
	last := remainingList.Templates[0]
	if _, err := store.ArchiveEncounterTemplate(ctxPed, "doctor-a", last.ID, map[string]any{
		"expectedVersion": json.Number(fmt.Sprint(last.Version)),
	}); err == nil || !strings.Contains(err.Error(), "al menos una plantilla activa") {
		t.Fatalf("expected LAST_ACTIVE_TEMPLATE error, got %v", err)
	}
	if _, err := store.RestoreEncounterTemplate(ctxPed, "doctor-a", remainingList.ArchivedTemplates[0].ID, map[string]any{
		"expectedVersion": json.Number(fmt.Sprint(remainingList.ArchivedTemplates[0].Version)),
	}); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	// 4. General medicine AddContext seeds 1 default and isolates contexts.
	if _, err := store.Edit(ctxPed, "doctor-a", "finish", map[string]any{
		"specialtyContextId": json.Number(fmt.Sprint(setupPed.ContextID)),
		"expectedVersion":    json.Number(fmt.Sprint(setupPed.Version)),
	}); err != nil {
		t.Fatal(err)
	}
	setupGen, err := store.AddContext(ctx, "doctor-a", map[string]any{"specialtyId": json.Number(fmt.Sprint(generalID))})
	if err != nil {
		t.Fatal(err)
	}
	ctxGen := api.WithSpecialtyContext(ctx, setupGen.ContextID)
	listGen, err := store.ListEncounterTemplates(ctxGen, "doctor-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(listGen.Templates) != 1 || listGen.Templates[0].Name != "Historia clínica general" {
		t.Fatalf("expected 1 default general template, got %+v", listGen.Templates)
	}
	// Cross-context access rejected.
	if _, err := store.UpdateEncounterTemplate(ctxGen, "doctor-a", last.ID, map[string]any{
		"expectedVersion": json.Number(fmt.Sprint(last.Version)),
		"name":            "Cross Context",
		"formDefinition":  copiedForm,
	}); err == nil {
		t.Fatal("expected cross-context update to fail")
	}

	// 5. Lazy backfill + import defaults on existing completed pediatrics context.
	if _, err := pool.Exec(ctx, `DELETE FROM doctor_encounter_templates WHERE specialty_context_id=$1`, setupPed.ContextID); err != nil {
		t.Fatal(err)
	}
	backfilled, err := store.ListEncounterTemplates(ctxPed, "doctor-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(backfilled.Templates) != 1 || backfilled.Templates[0].Name != "Historia clínica general" {
		t.Fatalf("expected lazy backfill template, got %+v", backfilled.Templates)
	}
	if len(backfilled.SystemDefaults) != 3 {
		t.Fatalf("expected 3 system defaults available, got %d", len(backfilled.SystemDefaults))
	}
	imported, err := store.ImportDefaultEncounterTemplates(ctxPed, "doctor-a", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Templates) != 4 {
		t.Fatalf("expected 4 templates after importing 3 defaults + existing, got %d", len(imported.Templates))
	}

	// 6. ROLE_STAFF is rejected with 403 FORBIDDEN on HTTP routes.
	mux := http.NewServeMux()
	store.Routes(mux)
	handler := api.Gateway("tpl-fixture", api.Dispatch(mux))
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/doctorchart/workspace/encounter-templates?specialtyContextId=%d", setupPed.ContextID), nil)
	req.Header.Set("X-Gateway-Secret", "tpl-fixture")
	req.Header.Set("X-User-Id", "staff-1")
	req.Header.Set("X-User-Role", "ROLE_STAFF")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for ROLE_STAFF, got %d (%s)", rec.Code, rec.Body.String())
	}
}
