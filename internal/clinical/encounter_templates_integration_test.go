package clinical

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestClinicalEncounterTemplateManualSelectionAndSnapshotImmutability(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	s := services[0]
	ctx := t.Context()

	var pediatricsID, generalID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code='pediatrics'").Scan(&pediatricsID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code='general_medicine'").Scan(&generalID); err != nil {
		t.Fatal(err)
	}

	setupPed, err := s.workspaces.Select(ctx, "tpl-owner", map[string]any{"specialtyId": json.Number(fmt.Sprint(pediatricsID)), "expectedVersion": nil})
	if err != nil {
		t.Fatal(err)
	}
	setupPed, err = s.workspaces.Edit(ctx, "tpl-owner", "finish", map[string]any{
		"specialtyContextId": json.Number(fmt.Sprint(setupPed.ContextID)),
		"expectedVersion":    json.Number(fmt.Sprint(setupPed.Version)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctxPed := api.WithSpecialtyContext(ctx, setupPed.ContextID)
	tplList, err := s.workspaces.ListEncounterTemplates(ctxPed, "tpl-owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(tplList.Templates) != 3 {
		t.Fatalf("expected 3 pediatric templates, got %d", len(tplList.Templates))
	}

	pReq := integrityPatientRequest()
	pReq["workspaceVersion"] = json.Number(fmt.Sprint(setupPed.Version))
	patient, err := s.CreatePatient(ctxPed, api.Identity{UserID: "tpl-owner", Plan: "BASIC"}, pReq)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Omitting templateId when >= 2 active templates returns ENCOUNTER_TEMPLATE_REQUIRED.
	missingReq := integrityEncounterRequest(patient.ID, "FIRST_VISIT")
	missingReq["workspaceVersion"] = json.Number(fmt.Sprint(setupPed.Version))
	if _, err := s.CreateEncounter(ctxPed, api.Identity{UserID: "tpl-owner", Plan: "BASIC"}, missingReq); err == nil {
		t.Fatal("expected ENCOUNTER_TEMPLATE_REQUIRED error")
	}

	// 2. Create first encounter with template 1 ("Recién nacidos") and second with template 3 ("Niños mayores y adolescentes") on same patient.
	tpl1 := tplList.Templates[0]
	tpl3 := tplList.Templates[2]

	req1 := integrityEncounterRequest(patient.ID, "FIRST_VISIT")
	req1["workspaceVersion"] = json.Number(fmt.Sprint(setupPed.Version))
	req1["templateId"] = json.Number(fmt.Sprint(tpl1.ID))
	enc1, err := s.CreateEncounter(ctxPed, api.Identity{UserID: "tpl-owner", Plan: "BASIC"}, req1)
	if err != nil {
		t.Fatalf("enc1 failed: %v", err)
	}
	if enc1.TemplateID == nil || *enc1.TemplateID != tpl1.ID || enc1.TemplateName == nil || *enc1.TemplateName != tpl1.Name {
		t.Fatalf("unexpected enc1 template metadata: %+v / %+v", enc1.TemplateID, enc1.TemplateName)
	}

	req2 := integrityEncounterRequest(patient.ID, "FOLLOW_UP")
	req2["workspaceVersion"] = json.Number(fmt.Sprint(setupPed.Version))
	req2["templateId"] = json.Number(fmt.Sprint(tpl3.ID))
	enc2, err := s.CreateEncounter(ctxPed, api.Identity{UserID: "tpl-owner", Plan: "BASIC"}, req2)
	if err != nil {
		t.Fatalf("enc2 failed: %v", err)
	}
	if enc2.TemplateID == nil || *enc2.TemplateID != tpl3.ID || enc2.TemplateName == nil || *enc2.TemplateName != tpl3.Name {
		t.Fatalf("unexpected enc2 template metadata: %+v / %+v", enc2.TemplateID, enc2.TemplateName)
	}

	// 3. Rename tpl1 and archive tpl3: existing encounters must preserve original template names and snapshots.
	var editedForm map[string]any
	if err := json.Unmarshal(tpl1.FormDefinition, &editedForm); err != nil {
		t.Fatal(err)
	}
	if _, err := s.workspaces.UpdateEncounterTemplate(ctxPed, "tpl-owner", tpl1.ID, map[string]any{
		"expectedVersion": json.Number(fmt.Sprint(tpl1.Version)),
		"name":            "Recién nacidos RENOMBRADA",
		"formDefinition":  editedForm,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.workspaces.ArchiveEncounterTemplate(ctxPed, "tpl-owner", tpl3.ID, map[string]any{
		"expectedVersion": json.Number(fmt.Sprint(tpl3.Version)),
	}); err != nil {
		t.Fatal(err)
	}

	var stored1, stored2 EncounterSummary
	if err := s.readSnapshot(ctxPed, func(tx pgx.Tx) error {
		list, err := s.encounterList(ctxPed, tx, "e.id IN ($1,$2)", []any{enc1.ID, enc2.ID}, "e.id")
		if err != nil {
			return err
		}
		if len(list) != 2 {
			return fmt.Errorf("expected 2 encounters, got %d", len(list))
		}
		stored1 = list[0]
		stored2 = list[1]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if stored1.TemplateName == nil || *stored1.TemplateName != tpl1.Name {
		t.Fatalf("expected frozen template name %q, got %+v", tpl1.Name, stored1.TemplateName)
	}
	if stored2.TemplateName == nil || *stored2.TemplateName != tpl3.Name {
		t.Fatalf("expected archived template name preserved %q, got %+v", tpl3.Name, stored2.TemplateName)
	}

	// 4. Creating an encounter with an archived template fails.
	reqArchived := integrityEncounterRequest(patient.ID, "FOLLOW_UP")
	reqArchived["workspaceVersion"] = json.Number(fmt.Sprint(setupPed.Version))
	reqArchived["templateId"] = json.Number(fmt.Sprint(tpl3.ID))
	if _, err := s.CreateEncounter(ctxPed, api.Identity{UserID: "tpl-owner", Plan: "BASIC"}, reqArchived); err == nil {
		t.Fatal("expected archived template to be rejected")
	}

	// 5. General medicine (1 active template) auto-selects when templateId is omitted.
	setupGen, err := s.workspaces.AddContext(ctx, "tpl-owner", map[string]any{"specialtyId": json.Number(fmt.Sprint(generalID))})
	if err != nil {
		t.Fatal(err)
	}
	setupGen, err = s.workspaces.Edit(ctx, "tpl-owner", "finish", map[string]any{
		"specialtyContextId": json.Number(fmt.Sprint(setupGen.ContextID)),
		"expectedVersion":    json.Number(fmt.Sprint(setupGen.Version)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctxGen := api.WithSpecialtyContext(ctx, setupGen.ContextID)
	pGenReq := integrityPatientRequest()
	pGenReq["workspaceVersion"] = json.Number(fmt.Sprint(setupGen.Version))
	pGen, err := s.CreatePatient(ctxGen, api.Identity{UserID: "tpl-owner", Plan: "BASIC"}, pGenReq)
	if err != nil {
		t.Fatal(err)
	}
	eGenReq := integrityEncounterRequest(pGen.ID, "FIRST_VISIT")
	eGenReq["workspaceVersion"] = json.Number(fmt.Sprint(setupGen.Version))
	eGen, err := s.CreateEncounter(ctxGen, api.Identity{UserID: "tpl-owner", Plan: "BASIC"}, eGenReq)
	if err != nil {
		t.Fatalf("single-template auto-selection failed: %v", err)
	}
	if eGen.TemplateName == nil || *eGen.TemplateName != "Historia clínica general" {
		t.Fatalf("expected auto-selected Historia clínica general, got %+v", eGen.TemplateName)
	}
}
