package clinical

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

func TestGeneralMedicineCompletePatientAndEncounterWithBrowserTimeZone(t *testing.T) {
	services, pool := integrityFixture(t, 100, 100)
	s := services[0]
	ctx := zoneContext(t, "America/Buenos_Aires")
	const owner = "general-save-owner"
	var specialtyID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code='general_medicine'").Scan(&specialtyID); err != nil {
		t.Fatal(err)
	}
	setup, err := s.workspaces.Select(ctx, owner, map[string]any{"specialtyId": json.Number(fmt.Sprint(specialtyID)), "expectedVersion": nil})
	if err != nil {
		t.Fatal(err)
	}
	setup, err = s.workspaces.Edit(ctx, owner, "finish", map[string]any{"expectedVersion": json.Number(fmt.Sprint(setup.Version))})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := wizard.Decode(setup.Definition)
	if err != nil {
		t.Fatal(err)
	}
	complete := func(section string) map[string]any {
		values := map[string]any{}
		for _, raw := range definition[section].(map[string]any)["categories"].([]any) {
			category := raw.(map[string]any)
			if category["enabled"] != true {
				continue
			}
			for _, rawField := range category["fields"].([]any) {
				field := rawField.(map[string]any)
				key := field["key"].(string)
				switch field["type"] {
				case "TEXT", "TEXTAREA":
					values[key] = "Dato de prueba"
				case "DATE":
					values[key] = "1990-01-01"
				case "NUMBER":
					values[key] = json.Number("70")
				case "SELECT":
					values[key] = field["options"].([]any)[0].(map[string]any)["value"]
				case "BOOLEAN":
					values[key] = true
				}
			}
		}
		return values
	}
	values := complete("patientForm")
	for key, value := range map[string]any{"sys_dni": "11222333", "sys_phone": "1155554444", "sys_email": "prueba@example.test", "sys_allergyStatus": "PRESENT", "sys_conditionStatus": "PRESENT", "sys_medicationStatus": "PRESENT"} {
		values[key] = value
	}
	identity := api.Identity{UserID: owner, Plan: "BASIC", Roles: []string{"ROLE_COMPANY"}}
	patient, err := s.CreatePatient(ctx, identity, map[string]any{"workspaceVersion": json.Number(fmt.Sprint(setup.Version)), "values": values})
	if err != nil {
		t.Fatal(err)
	}
	patient, err = s.UpdatePatient(ctx, owner, patient.ID, map[string]any{"expectedVersion": json.Number(fmt.Sprint(patient.Version)), "values": values})
	if err != nil {
		t.Fatal(err)
	}
	encounter, err := s.CreateEncounter(ctx, identity, map[string]any{"patientId": json.Number(fmt.Sprint(patient.ID)), "workspaceVersion": json.Number(fmt.Sprint(setup.Version)), "consultationType": "FIRST_VISIT", "values": complete("encounterForm")})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := s.GetPatient(ctx, owner, patient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != patient.ID || loaded.Email == nil || *loaded.Email != "prueba@example.test" || loaded.Measurements == nil || loaded.Measurements.Weight == nil || *loaded.Measurements.Weight != 70 {
		t.Fatal("full patient or post-encounter reload lost saved data")
	}
	if encounter.PatientID != patient.ID {
		t.Fatal("encounter belongs to another patient")
	}
}
