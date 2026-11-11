package clinical

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestDermatologyContextPreservesClinicalFlow(t *testing.T) {
	services, pool := integrityFixture(t, 100, 100)
	s := services[0]
	ctx := zoneContext(t, "America/Buenos_Aires")
	const owner = "dermatology-owner"
	var specialty int64
	if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code='dermatology'").Scan(&specialty); err != nil {
		t.Fatal(err)
	}
	setup, err := s.workspaces.Select(ctx, owner, map[string]any{"specialtyId": json.Number(fmt.Sprint(specialty)), "expectedVersion": nil})
	if err != nil {
		t.Fatal(err)
	}
	setup, err = s.workspaces.Edit(ctx, owner, "finish", map[string]any{"specialtyContextId": json.Number(fmt.Sprint(setup.ContextID)), "expectedVersion": json.Number(fmt.Sprint(setup.Version))})
	if err != nil {
		t.Fatal(err)
	}
	identity := api.Identity{UserID: owner, Plan: "BASIC"}
	request := integrityPatientRequest()
	request["workspaceVersion"] = json.Number(fmt.Sprint(setup.Version))
	patient, err := s.CreatePatient(ctx, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	patient, err = s.UpdatePatient(ctx, owner, patient.ID, map[string]any{"expectedVersion": json.Number(fmt.Sprint(patient.Version)), "values": request["values"]})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"FIRST_VISIT", "FOLLOW_UP"} {
		encounter, err := s.CreateEncounter(ctx, identity, map[string]any{"patientId": json.Number(fmt.Sprint(patient.ID)), "workspaceVersion": json.Number(fmt.Sprint(setup.Version)), "consultationType": kind, "values": map[string]any{"chiefComplaint": "Control dermatológico"}})
		if err != nil {
			t.Fatal(err)
		}
		if encounter.SpecialtyID != specialty || encounter.WorkspaceVersion != setup.Version {
			t.Fatal("wrong effective context/version")
		}
	}
	if _, err := s.GetPatient(ctx, owner, patient.ID); err != nil {
		t.Fatal("record", err)
	}
	if _, err := s.ClinicalSummary(ctx, owner, patient.ID); err != nil {
		t.Fatal("summary", err)
	}
	_, err = s.GetPatient(ctx, "integrity-other", patient.ID)
	integrityError(t, err, 404, "PATIENT_NOT_FOUND")
	if err := s.workspaces.RequireFeature(ctx, owner, "GROWTH"); err == nil {
		t.Fatal("pediatric feature leaked into dermatology")
	}
}
