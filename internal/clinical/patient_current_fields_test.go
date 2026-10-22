package clinical

import (
	"encoding/json"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"testing"
)

func TestPatientProjectionDoesNotKeepRetiredFields(t *testing.T) {
	p := Patient{Family: json.RawMessage(`{"familyAllergies":["obsolete"],"geneticDiseases":null}`)}
	evaluation := wizard.Evaluation{Projections: map[string]any{
		"patient.fullName": "Synthetic patient", "patient.birthDate": "2000-01-01",
		"patient.familyHistory.paternalDiseases": []any{"current value"},
		"patient.guardianInfo.primaryGuardian":   "Madre", "patient.guardianInfo.hasSiblings": false,
		"patient.guardianInfo.siblingsCount": json.Number("0"),
	}}
	if err := applyPatient(&p, evaluation); err != nil {
		t.Fatal(err)
	}
	var family, guardian map[string]any
	if err := json.Unmarshal(p.Family, &family); err != nil {
		t.Fatal(err)
	}
	if len(family) != 1 {
		t.Fatalf("unexpected family fields: %s", p.Family)
	}
	if family["paternalDiseases"].([]any)[0] != "current value" {
		t.Fatal("current history lost")
	}
	if err := json.Unmarshal(p.Guardian, &guardian); err != nil {
		t.Fatal(err)
	}
	if len(guardian) != 3 || guardian["hasSiblings"] != false || guardian["siblingsCount"] != float64(0) {
		t.Fatalf("guardian lost meaningful zero/false values: %s", p.Guardian)
	}
	if err := normalizePatientHistory(&p); err != nil {
		t.Fatal(err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(p.Family, &normalized); err != nil || len(normalized) != 1 {
		t.Fatal("read reintroduced absent fields", err)
	}
}

func TestHistoryNormalizationRemovesRetiredAndNullFields(t *testing.T) {
	p := Patient{Family: json.RawMessage(`{"familyAllergies":["retired"],"paternalDiseases":["current"],"maternalDiseases":null}`), Guardian: json.RawMessage(`{"firstGuardianFullName":"retired","primaryGuardian":"Madre","hasSiblings":false}`)}
	if err := normalizePatientHistory(&p); err != nil {
		t.Fatal(err)
	}
	var family, guardian map[string]any
	if err := json.Unmarshal(p.Family, &family); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(p.Guardian, &guardian); err != nil {
		t.Fatal(err)
	}
	if len(family) != 1 || len(guardian) != 2 || guardian["primaryGuardian"] != "Madre" || guardian["hasSiblings"] != false {
		t.Fatalf("unexpected normalized fields: %s %s", p.Family, p.Guardian)
	}
}
