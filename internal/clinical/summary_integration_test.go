package clinical

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"testing"
)

// Uses only the explicitly opted-in disposable fixture from integrity tests.
func TestClinicalSummaryPersistenceOwnershipAndConflicts(t *testing.T) {
	services, pool := integrityFixture(t, 100, 100)
	s := services[0]
	ctx := context.Background()
	p := integrityPatient(t, s)
	request := profileAdd("ALLERGY", "2026-01-01")
	request["expectedVersion"] = json.Number(fmt.Sprint(p.Version))
	saved, err := s.SaveClinicalProfile(ctx, "integrity-owner", p.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != p.Version+1 {
		t.Fatal("patient version not advanced")
	}
	if _, err := s.SaveClinicalProfile(ctx, "integrity-owner", p.ID, request); err == nil {
		t.Fatal("stale version accepted")
	} else {
		integrityError(t, err, 409, "VERSION_CONFLICT")
	}
	if _, err := s.ClinicalSummary(ctx, "integrity-other", p.ID); err == nil {
		t.Fatal("cross-owner summary exposed")
	} else {
		integrityError(t, err, 404, "PATIENT_NOT_FOUND")
	}
	request["expectedVersion"] = json.Number(fmt.Sprint(saved.Version))
	if _, err := s.SaveClinicalProfile(ctx, "integrity-other", p.ID, request); err == nil {
		t.Fatal("cross-owner write accepted")
	} else {
		integrityError(t, err, 404, "PATIENT_NOT_FOUND")
	}
	result, err := services[1].ClinicalSummary(ctx, "integrity-owner", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Profile.Entries) != 1 || result.Profile.Entries[0].Text != "Dato sintético" || result.PatientVersion != saved.Version || result.Historical == nil {
		t.Fatalf("persisted summary: %+v", result)
	}
	// A regular demographic edit must preserve the profile document.
	var values map[string]any
	if err := json.Unmarshal(saved.Values, &values); err != nil {
		t.Fatal(err)
	}
	edited, err := s.UpdatePatient(ctx, "integrity-owner", p.ID, map[string]any{"expectedVersion": json.Number(fmt.Sprint(saved.Version)), "values": values})
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, "SELECT personal_history FROM patients WHERE id=$1", p.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	profile, _, err := readClinicalProfile(raw)
	if err != nil || len(profile.Entries) != 1 {
		t.Fatalf("demographic edit lost profile: %s %v", raw, err)
	}
	request["expectedVersion"] = json.Number(fmt.Sprint(edited.Version))
	request["date"] = "2099-01-01"
	if _, err := s.SaveClinicalProfile(ctx, "integrity-owner", p.ID, request); err == nil {
		t.Fatal("future allergy accepted")
	}
	result, err = s.ClinicalSummary(ctx, "integrity-owner", p.ID)
	if err != nil || result.PatientVersion != edited.Version || len(result.Profile.Entries) != 1 {
		t.Fatalf("invalid write changed record: %+v %v", result, err)
	}
}

func TestSummaryDerivesHistoryFromPatientAndEncounters(t *testing.T) {
	services, pool := integrityFixture(t, 100, 100)
	s := services[0]
	ctx := context.Background()
	// Clinical history is now an explicit specialty choice, not a forced block.
	if _, err := pool.Exec(ctx, `WITH history AS (
 SELECT w.published_definition->'patientForm'->'categories'->1 AS category
 FROM wizard_defaults w JOIN specialties s ON s.id=w.specialty_id WHERE s.code='general_medicine'
) UPDATE doctor_specialty_contexts SET definition=jsonb_set(jsonb_set(definition,
 '{patientForm,categories}',(definition->'patientForm'->'categories') || jsonb_build_array(history.category)),
 '{encounterForm,categories}',(definition->'encounterForm'->'categories') || jsonb_build_array(history.category))
 FROM history WHERE workspace_id=(SELECT id FROM doctor_workspaces WHERE user_id='integrity-owner')`); err != nil {
		t.Fatal(err)
	}
	owner := api.Identity{UserID: "integrity-owner", Plan: "BASIC"}
	request := integrityPatientRequest()
	values := request["values"].(map[string]any)
	values["sys_allergyStatus"] = "PRESENT"
	values["sys_allergies"] = "Penicilina"
	values["sys_medicationStatus"] = "NONE"
	patient, err := s.CreatePatient(ctx, owner, request)
	if err != nil {
		t.Fatal(err)
	}
	first := integrityEncounterRequest(patient.ID, "FIRST_VISIT")
	answers := first["values"].(map[string]any)
	answers["sys_conditionStatus"] = "PRESENT"
	answers["sys_conditions"] = "Asma"
	answers["sys_historySource"] = "CONFIRMED"
	encounter, err := s.CreateEncounter(ctx, owner, first)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.ClinicalSummary(ctx, owner.UserID, patient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.History["ALLERGY"].Source != "PATIENT" || result.History["PROBLEM"].SourceID != encounter.ID || result.History["PROBLEM"].Items[0] != "Asma" || result.History["MEDICATION"].Status != "NONE" {
		t.Fatal(result.History)
	}
	originalDate := result.History["ALLERGY"].Date
	if _, err := s.CreateEncounter(ctx, owner, integrityEncounterRequest(patient.ID, "FOLLOW_UP")); err != nil {
		t.Fatal(err)
	}
	result, err = s.ClinicalSummary(ctx, owner.UserID, patient.ID)
	if err != nil || result.History["PROBLEM"].SourceID != encounter.ID {
		t.Fatalf("unreviewed consultation erased history: %+v %v", result, err)
	}
	var savedValues map[string]any
	if err := json.Unmarshal(patient.Values, &savedValues); err != nil {
		t.Fatal(err)
	}
	savedValues["sys_phone"] = "1155555555"
	if _, err := s.UpdatePatient(ctx, owner.UserID, patient.ID, map[string]any{"expectedVersion": json.Number(fmt.Sprint(patient.Version)), "values": savedValues}); err != nil {
		t.Fatal(err)
	}
	result, err = s.ClinicalSummary(ctx, owner.UserID, patient.ID)
	if err != nil || result.History["ALLERGY"].Date != originalDate {
		t.Fatal("demographic update changed clinical provenance", err)
	}
	negative := integrityEncounterRequest(patient.ID, "FOLLOW_UP")
	negative["values"].(map[string]any)["sys_allergyStatus"] = "NONE"
	reviewed, err := s.CreateEncounter(ctx, owner, negative)
	if err != nil {
		t.Fatal(err)
	}
	result, err = s.ClinicalSummary(ctx, owner.UserID, patient.ID)
	if err != nil || result.History["ALLERGY"].Status != "NONE" || result.History["ALLERGY"].SourceID != reviewed.ID {
		t.Fatal("negative review did not override earlier data", err)
	}
	// Removing it from future consultations preserves already recorded history.
	if _, err := pool.Exec(ctx, `UPDATE doctor_specialty_contexts SET definition=jsonb_set(definition,'{encounterForm,categories}',
 (SELECT jsonb_agg(c) FROM jsonb_array_elements(definition->'encounterForm'->'categories') c WHERE c->>'key'<>'sys_clinical_history')) WHERE workspace_id=(SELECT id FROM doctor_workspaces WHERE user_id='integrity-owner')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateEncounter(ctx, owner, integrityEncounterRequest(patient.ID, "FOLLOW_UP")); err != nil {
		t.Fatal(err)
	}
	result, err = s.ClinicalSummary(ctx, owner.UserID, patient.ID)
	if err != nil || result.History["ALLERGY"].SourceID != reviewed.ID || result.History["PROBLEM"].Items[0] != "Asma" {
		t.Fatal("removing history configuration changed saved clinical answers", err)
	}
}
