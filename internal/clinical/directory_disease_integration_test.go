package clinical

import (
	"net/url"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestListPatientsFilterByLatestEncounterDisease(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	svc := services[0]
	ctx := t.Context()
	owner := api.Identity{UserID: "integrity-owner", Plan: "BASIC"}

	pA, err := svc.CreatePatient(ctx, owner, integrityPatientRequest())
	if err != nil {
		t.Fatal(err)
	}
	reqB := integrityPatientRequest()
	reqB["values"].(map[string]any)["sys_firstName"] = "Paciente"
	reqB["values"].(map[string]any)["sys_lastName"] = "Dos"
	reqB["values"].(map[string]any)["sys_dni"] = "22334455"
	pB, err := svc.CreatePatient(ctx, owner, reqB)
	if err != nil {
		t.Fatal(err)
	}

	// Patient A: 5 encounters, 2nd has "Bronquitis aguda", 5th (latest) has "Control sano" -> must NOT match "bron"
	base := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Microsecond)
	for i := 1; i <= 5; i++ {
		var disease any
		if i == 2 {
			disease = "Bronquitis aguda"
		} else if i == 5 {
			disease = "Control sano"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO clinical_encounters
			(patient_id,specialty_id,encounter_date,consultation_type,disease,form_values,formula_results,form_definition_snapshot,formula_context,workspace_version,secondary_diagnoses,prescriptions,diagnostic_orders,version,updated_at)
			VALUES($1,1,$2,$3,$4,'{}','{}','{}','{}',0,'[]','[]','[]',0,clock_timestamp())`,
			pA.ID, base.Add(time.Duration(i)*24*time.Hour), map[bool]string{true: "FIRST_VISIT", false: "FOLLOW_UP"}[i == 1], disease); err != nil {
			t.Fatal(err)
		}
	}

	// Patient B: latest encounter has "Bronquitis espasmódica" -> MUST match "bron"
	if _, err := pool.Exec(ctx, `INSERT INTO clinical_encounters
		(patient_id,specialty_id,encounter_date,consultation_type,disease,form_values,formula_results,form_definition_snapshot,formula_context,workspace_version,secondary_diagnoses,prescriptions,diagnostic_orders,version,updated_at)
		VALUES($1,1,$2,'FIRST_VISIT','Bronquitis espasmódica','{}','{}','{}','{}',0,'[]','[]','[]',0,clock_timestamp())`,
		pB.ID, base.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ListPatients(ctx, owner.UserID, url.Values{"disease": {"bron"}})
	if err != nil {
		t.Fatal(err)
	}
	payload := res.(map[string]any)
	items := payload["content"].([]PatientList)
	if len(items) != 1 || items[0].ID != pB.ID {
		t.Fatalf("expected only patient B (%d) to match latest encounter disease 'bron', got %+v", pB.ID, items)
	}
	if items[0].LastEncounterDisease == nil || *items[0].LastEncounterDisease != "Bronquitis espasmódica" {
		t.Fatalf("unexpected LastEncounterDisease: %v", items[0].LastEncounterDisease)
	}
}
