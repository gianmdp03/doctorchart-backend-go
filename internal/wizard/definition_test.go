package wizard

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func fixtureDefinition(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/custom-specialty.json")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return object(payload["definition"])
}
func fieldByKey(definition map[string]any, section, key string) map[string]any {
	for _, category := range list(object(definition[section])["categories"]) {
		for _, raw := range list(object(category)["fields"]) {
			data := object(raw)
			if data["key"] == key {
				return data
			}
		}
	}
	return nil
}
func TestDefinitionIdentityAndVisibility(t *testing.T) {
	validator, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if err := validator.Validate(fixtureDefinition(t)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, code string
		mutate     func(map[string]any)
	}{
		{"required core", "MISSING_PATIENT_CORE", func(d map[string]any) { object(d["patientForm"])["categories"] = []any{} }},
		{"range", "INVALID_RANGE", func(d map[string]any) { fieldByKey(d, "encounterForm", "readingA")["minimum"] = json.Number("101") }},
		{"system field", "INVALID_SYSTEM_FIELD", func(d map[string]any) { fieldByKey(d, "patientForm", "sys_firstName")["system"] = false }},
		{"visibility cross form", "INVALID_VISIBILITY_REFERENCE", func(d map[string]any) {
			fieldByKey(d, "encounterForm", "notes")["visibleWhen"] = map[string]any{"fieldKey": "consent", "equals": true}
		}},
		{"visibility cycle", "VISIBILITY_CYCLE", func(d map[string]any) {
			fieldByKey(d, "patientForm", "consent")["visibleWhen"] = map[string]any{"fieldKey": "consent", "equals": true}
		}},
		{"formula unknown field", "INVALID_FORMULA_REFERENCE", func(d map[string]any) {
			object(object(fieldByKey(d, "encounterForm", "result")["formula"])["inputs"])["a"] = map[string]any{"fieldKey": "missing"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := fixtureDefinition(t)
			test.mutate(d)
			err := validator.Validate(d)
			var failure *api.Error
			if !errors.As(err, &failure) {
				t.Fatalf("missing typed validation error: %v", err)
			}
			found := false
			for _, violation := range failure.Errors {
				if violation.Code == test.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected %s, got %+v", test.code, failure.Errors)
			}
		})
	}
}
func TestRequiredStructuralErrorsHaveOneEntryPerMissingProperty(t *testing.T) {
	validator, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	err = validator.Validate(map[string]any{})
	var failure *api.Error
	if !errors.As(err, &failure) || len(failure.Errors) != 4 {
		t.Fatalf("expected four root errors: %v", err)
	}
}
func TestFormulaDomainBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, kind, operation string
		inputs                map[string]any
		status                string
		value                 float64
	}{
		{"bmi", "IMC", "", map[string]any{"weightKg": 80.0, "heightCm": 200.0}, "READY", 20},
		{"missing", "IMC", "", map[string]any{"weightKg": 80.0}, "MISSING_INPUT", 0},
		{"zero divisor", "CUSTOM", "DIVIDE", map[string]any{"a": 3.0, "b": 0.0}, "DIVISION_BY_ZERO", 0},
		{"child renal", "TFGE", "", map[string]any{"creatinineMgDl": 1.0, "ageYears": 17.0, "gender": "MALE"}, "NOT_APPLICABLE", 0},
		{"unknown sex", "COCKCROFT", "", map[string]any{"creatinineMgDl": 1.0, "weightKg": 70.0, "ageYears": 45.0, "gender": "UNKNOWN"}, "NOT_APPLICABLE", 0},
		{"string number", "IMC", "", map[string]any{"weightKg": "80", "heightCm": 200.0}, "INVALID_INPUT", 0},
		{"custom average", "CUSTOM", "AVERAGE", map[string]any{"a": 3.0, "b": 5.0}, "READY", 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Calculate(test.kind, test.operation, test.inputs)
			if result.Status != test.status {
				t.Fatalf("got %+v", result)
			}
			if result.Status == "READY" && (result.Value == nil || *result.Value != test.value) {
				t.Fatalf("value mismatch %+v", result)
			}
			if result.Status != "READY" && result.Value != nil {
				t.Fatal("unavailable result must encode null")
			}
		})
	}
}
