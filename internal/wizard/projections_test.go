package wizard

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func projectionForm(keys ...string) map[string]any {
	fields := []any{}
	for _, key := range keys {
		fields = append(fields, map[string]any{"key": key, "label": key, "type": "TEXTAREA", "required": false})
	}
	return map[string]any{"categories": []any{map[string]any{"key": "synthetic", "title": "Synthetic", "enabled": true, "fields": fields}}}
}

func TestHistoricalFollowUpProjection(t *testing.T) {
	for _, test := range []struct {
		name   string
		keys   []string
		values map[string]any
		hidden string
		want   any
	}{
		{"legacy", []string{"followUp"}, map[string]any{"followUp": "Synthetic historical instruction"}, "", "Synthetic historical instruction"},
		{"canonical", []string{"followUpInstructions"}, map[string]any{"followUpInstructions": "Synthetic canonical instruction"}, "", "Synthetic canonical instruction"},
		{"both", []string{"followUp", "followUpInstructions"}, map[string]any{"followUp": "old", "followUpInstructions": "new"}, "", "new"},
		{"empty", []string{"followUp", "followUpInstructions"}, map[string]any{"followUp": "old", "followUpInstructions": ""}, "", ""},
		{"null", []string{"followUp", "followUpInstructions"}, map[string]any{"followUp": "old", "followUpInstructions": nil}, "", nil},
		{"absent canonical answer", []string{"followUp", "followUpInstructions"}, map[string]any{"followUp": "old"}, "", nil},
		{"hidden canonical", []string{"followUp", "followUpInstructions"}, map[string]any{"followUp": "old", "followUpInstructions": "hidden"}, "followUpInstructions", nil},
		{"hidden legacy", []string{"followUp"}, map[string]any{"followUp": "hidden"}, "followUp", nil},
		{"unknown legacy", []string{"notes"}, map[string]any{"followUp": "unknown"}, "", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			form := projectionForm(test.keys...)
			if test.hidden != "" {
				fields, _ := formFields(form)
				fields[test.hidden].data["visibleWhen"] = map[string]any{"fieldKey": "absentController", "equals": true}
			}
			before, _ := json.Marshal(test.values)
			for range 25 {
				got := ClinicalProjections(form, test.values)
				if !reflect.DeepEqual(got["encounter.followUpInstructions"], test.want) {
					t.Fatalf("got %#v want %#v", got, test.want)
				}
				categories := list(form["categories"])
				fields := list(object(categories[0])["fields"])
				for i, j := 0, len(fields)-1; i < j; i, j = i+1, j-1 {
					fields[i], fields[j] = fields[j], fields[i]
				}
			}
			after, _ := json.Marshal(test.values)
			if string(before) != string(after) {
				t.Fatal("read changed original answers")
			}
		})
	}
}

func TestClinicalProjectionCollisionsAndProtectedIdentity(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{
		{"sys_firstName", "firstName"}, {"sys_dob", "born"}, {"sys_sex", "sex"}, {"sys_insurance", "insuranceInfo"},
		{"sys_lastName", "lastName"}, {"sys_dni", "nationalId"}, {"sys_phone", "phone"}, {"sys_email", "email"},
		{"chiefComplaint", "complaint"}, {"historyOfPresentIllness", "presentIllness"}, {"physicalExamination", "physicalExam"},
		{"primaryDiagnosisDescription", "diagnosis"}, {"treatmentPlan", "treatment"}, {"followUpInstructions", "followUp"},
		{"weightKg", "weight"}, {"heightCm", "height"}, {"headCircumferenceCm", "headCircumference"},
		{"paternalDiseases", "father"}, {"siblingsCount", "count"}, {"insurancePlan", "planName"},
	} {
		t.Run(pair[0]+"/"+pair[1], func(t *testing.T) {
			definition := fixtureDefinition(t)
			section := "encounterForm"
			if len(pair[0]) > 4 && pair[0][:4] == "sys_" {
				section = "patientForm"
			}
			form := object(definition[section])
			cats := list(form["categories"])
			added := []any{}
			for _, key := range pair {
				if fieldByKey(definition, section, key) == nil {
					added = append(added, map[string]any{"key": key, "label": key, "type": "TEXTAREA", "required": false})
				}
			}
			form["categories"] = append(cats, map[string]any{"key": "collision", "title": "Synthetic collision", "enabled": true, "fields": added})
			var failure *api.Error
			if err := v.Validate(definition); !errors.As(err, &failure) {
				t.Fatalf("new collision accepted: %v", err)
			}
			found := false
			for _, issue := range failure.Errors {
				found = found || issue.Code == "CLINICAL_PROJECTION_COLLISION"
			}
			if !found {
				t.Fatalf("unexpected error: %+v", failure)
			}
			if err := v.ValidateStored(definition); err != nil {
				t.Fatalf("existing definition cannot be loaded: %v", err)
			}
		})
	}
	form := projectionForm("firstName", "fullName", "sys_firstName", "sys_lastName", "patient.firstName")
	values := map[string]any{"firstName": "Ambiguous", "fullName": "Forged", "patient.firstName": "Other", "sys_firstName": "Synthetic", "sys_lastName": "Protected"}
	for range 50 {
		projected := ClinicalProjections(form, values)
		if projected["patient.firstName"] != "Synthetic" || projected["patient.fullName"] != "Synthetic Protected" {
			t.Fatalf("identity overwritten: %+v", projected)
		}
		fields := list(object(list(form["categories"])[0])["fields"])
		fields[0], fields[len(fields)-1] = fields[len(fields)-1], fields[0]
	}
	// A historical collision remains editable under its saved snapshot.
	definition := fixtureDefinition(t)
	patientForm := object(definition["patientForm"])
	cats := list(patientForm["categories"])
	object(cats[1])["fields"] = append(list(object(cats[1])["fields"]), map[string]any{"key": "firstName", "label": "Ambiguous historical name", "type": "TEXT", "required": false})
	input := map[string]any{"sys_firstName": "Synthetic", "sys_lastName": "Protected", "sys_dni": "12345678", "sys_dob": "2000-01-01", "sys_sex": "UNKNOWN", "firstName": "Conflicting", "consent": true}
	if result, err := v.Evaluate(patientForm, input, map[string]any{"referenceDate": "2026-11-17"}); err != nil || result.Projections["patient.fullName"] != "Synthetic Protected" {
		t.Fatalf("historical evaluation: %+v %v", result, err)
	}
}

func TestHistoricalUnknownAnswerSurvivesAnActualEdit(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	form := projectionForm("followUp")
	previous := map[string]any{"followUp": "Old synthetic instruction", "retiredHistoricalField": "Original unknown synthetic value"}
	result, err := v.EvaluateUpdate(form, map[string]any{"followUp": "Corrected synthetic instruction"}, nil, previous)
	if err != nil {
		t.Fatal(err)
	}
	if result.Values["retiredHistoricalField"] != previous["retiredHistoricalField"] || result.Projections["encounter.followUpInstructions"] != "Corrected synthetic instruction" || len(result.Projections) != 1 {
		t.Fatalf("unknown answer was lost or became a new projection: %+v", result)
	}
}
