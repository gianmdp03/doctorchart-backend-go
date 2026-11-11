package wizard

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

//go:embed clinical-history.json
var clinicalHistoryJSON []byte

// WithClinicalHistory makes the former system history block specialty-owned.
// It never adds missing categories or fields: a deliberate removal stays removed.
// Stored snapshots and values are left intact.
func WithClinicalHistory(form map[string]any) map[string]any {
	next := make(map[string]any, len(form)+1)
	for k, v := range form {
		next[k] = v
	}
	categories := append([]any{}, list(form["categories"])...)
	for i, raw := range categories {
		category := object(raw)
		if category["key"] != "sys_clinical_history" {
			continue
		}
		copyCategory := make(map[string]any, len(category))
		for k, v := range category {
			copyCategory[k] = v
		}
		delete(copyCategory, "system")
		fields := make([]any, 0, len(list(category["fields"])))
		for _, rawField := range list(category["fields"]) {
			copyField := make(map[string]any)
			for k, v := range object(rawField) {
				copyField[k] = v
			}
			delete(copyField, "system")
			fields = append(fields, copyField)
		}
		copyCategory["fields"] = fields
		categories[i] = copyCategory
	}
	next["categories"] = categories
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

func clinicalHistoryErrors(form, values map[string]any) []api.FieldError {
	issues := []api.FieldError{}
	active := map[string]bool{}
	for _, raw := range list(form["categories"]) {
		category := object(raw)
		if !flag(category["enabled"]) {
			continue
		}
		for _, f := range list(category["fields"]) {
			active[text(object(f)["key"])] = true
		}
	}
	for _, group := range ClinicalHistoryGroups() {
		if !active[group.Status] || !active[group.Details] {
			continue
		}
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
