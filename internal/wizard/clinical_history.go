package wizard

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

//go:embed clinical-history.json
var clinicalHistoryJSON []byte

// WithClinicalHistory adds the code-owned block without mutating a saved form.
// Existing fields/snapshots retain their definitions. Missing blocks in older
// configurations are supplemented in memory, never by a bootstrap data rewrite.
func WithClinicalHistory(form map[string]any) map[string]any {
	next := make(map[string]any, len(form)+1)
	for k, v := range form {
		next[k] = v
	}
	categories := append([]any{}, list(form["categories"])...)
	for _, c := range categories {
		if object(c)["key"] == "sys_clinical_history" {
			return next
		}
	}
	category, err := Decode(clinicalHistoryJSON)
	if err != nil {
		panic("invalid embedded clinical history definition")
	}
	next["categories"] = append(categories, category)
	return next
}

func WithSystemHistory(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return raw, nil
	}
	definition, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	for _, section := range []string{"patientForm", "encounterForm"} {
		if form, ok := definition[section].(map[string]any); ok {
			definition[section] = WithClinicalHistory(form)
		}
	}
	return json.Marshal(definition)
}

func validClinicalHistory(category map[string]any) bool {
	expected, err := Decode(clinicalHistoryJSON)
	return err == nil && reflect.DeepEqual(expected, category)
}

func ClinicalHistoryGroups() [3]struct{ Status, Details, Kind string } {
	return [3]struct{ Status, Details, Kind string }{
		{"sys_allergyStatus", "sys_allergies", "ALLERGY"},
		{"sys_conditionStatus", "sys_conditions", "PROBLEM"},
		{"sys_medicationStatus", "sys_medications", "MEDICATION"},
	}
}

// Replacement updates from older clients must not silently erase a group they
// did not send. Explicit UNKNOWN/NONE/PRESENT choices remain deliberate edits.
func RetainUnreviewedHistory(previous, next map[string]any) {
	for _, g := range ClinicalHistoryGroups() {
		_, status := next[g.Status]
		_, details := next[g.Details]
		if !status && !details {
			if value, ok := previous[g.Status]; ok {
				next[g.Status] = value
			}
			if value, ok := previous[g.Details]; ok {
				next[g.Details] = value
			}
		}
	}
	if _, ok := next["sys_historySource"]; !ok {
		if source, ok := previous["sys_historySource"]; ok {
			next["sys_historySource"] = source
		}
	}
}

func clinicalHistoryErrors(values map[string]any) []api.FieldError {
	issues := []api.FieldError{}
	for _, group := range ClinicalHistoryGroups() {
		status, _ := values[group.Status].(string)
		details, _ := values[group.Details].(string)
		code := ""
		if status == "PRESENT" && strings.TrimSpace(details) == "" {
			code = "REQUIRED"
		}
		if status != "PRESENT" && strings.TrimSpace(details) != "" {
			code = "CLINICAL_STATUS_REQUIRED"
		}
		if api.JavaLength(details) > 4000 {
			code = "TEXT_TOO_LONG"
		}
		if code != "" {
			issues = append(issues, api.FieldError{Path: valuePath(group.Details), Code: code})
		}
	}
	return issues
}
