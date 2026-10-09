package wizard

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type Evaluation struct {
	Projections map[string]any
	Results     map[string]FormulaResult
	Values      map[string]any
}
type formField struct {
	data    map[string]any
	enabled bool
}

var emailInput = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
var dniInput = regexp.MustCompile(`^[0-9]{6,8}$`)
var phoneInput = regexp.MustCompile(`^[+0-9\s().-]+$`)

func personalTextError(key, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	switch key {
	case "sys_email", "email", "patient.email":
		if !emailInput.MatchString(value) {
			return "INVALID_EMAIL"
		}
	case "sys_dni", "nationalId", "patient.nationalId", "patient.guardianInfo.firstGuardianNationalId", "patient.guardianInfo.secondGuardianNationalId":
		if !dniInput.MatchString(value) {
			return "INVALID_DNI"
		}
	case "sys_phone", "phone", "patient.phone", "primaryContactPhone", "patient.guardianInfo.primaryContactPhone":
		digits := 0
		for _, char := range value {
			if char >= '0' && char <= '9' {
				digits++
			}
		}
		if !phoneInput.MatchString(value) || digits < 6 || digits > 15 {
			return "INVALID_PHONE"
		}
	}
	return ""
}

func valuePath(key string) string {
	return "/values/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
func date(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || len(text) != 10 {
		return time.Time{}, false
	}
	d, err := time.Parse("2006-01-02", text)
	return d, err == nil
}

func (v *Validator) Evaluate(form, values, context map[string]any) (Evaluation, error) {
	result := Evaluation{Projections: map[string]any{}, Results: map[string]FormulaResult{}, Values: map[string]any{}}
	wrapped := map[string]any{"schemaVersion": json.Number("1"), "patientForm": form, "encounterForm": map[string]any{"categories": []any{}}, "modulesConfig": map[string]any{"MEDICATION_CATALOG": false, "AI_ASSISTANT": false, "GROWTH_CHARTS": false}}
	// Saved encounter forms do not contain the system profile. Validate them against a minimal core.
	profile := false
	for _, category := range list(form["categories"]) {
		if object(category)["key"] == "sys_patient_profile" {
			profile = true
		}
	}
	if !profile {
		wrapped["encounterForm"] = form
		wrapped["patientForm"] = minimalPatientForm()
	}
	if err := v.Validate(wrapped); err != nil {
		return result, err
	}
	fields := map[string]formField{}
	order := []string{}
	for _, category := range list(form["categories"]) {
		category := object(category)
		for _, raw := range list(category["fields"]) {
			data := object(raw)
			key := text(data["key"])
			fields[key] = formField{data, flag(category["enabled"])}
			order = append(order, key)
		}
	}
	violations := []api.FieldError{}
	issue := func(key, code string) {
		violations = append(violations, api.FieldError{Path: valuePath(key), Code: code})
	}
	if values == nil {
		return result, &api.Error{Status: 400, Code: "INVALID_FORM_VALUES", Message: "Las respuestas del formulario no son válidas.", Errors: []api.FieldError{{Path: "/values", Code: "INVALID_TYPE"}}}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		field, ok := fields[key]
		if !ok {
			issue(key, "UNKNOWN_FIELD")
		} else if field.data["type"] == "FORMULA" {
			issue(key, "FORMULA_INPUT_FORBIDDEN")
		}
	}
	memo := map[string]bool{}
	var visible func(string, map[string]any, map[string]bool) bool
	visible = func(key string, answers map[string]any, visiting map[string]bool) bool {
		if cached, ok := memo[key]; ok {
			return cached
		}
		field, ok := fields[key]
		if !ok || !field.enabled {
			return false
		}
		if visiting[key] {
			return false
		}
		visiting[key] = true
		shown := true
		if condition, ok := field.data["visibleWhen"].(map[string]any); ok {
			controller := text(condition["fieldKey"])
			value, boolean := answers[controller].(bool)
			shown = visible(controller, answers, visiting) && boolean && value == condition["equals"]
		}
		delete(visiting, key)
		memo[key] = shown
		return shown
	}
	reference, _ := date(context["referenceDate"])
	for _, key := range order {
		field := fields[key].data
		kind := text(field["type"])
		if kind == "FORMULA" {
			continue
		}
		value := values[key]
		shown := visible(key, values, map[string]bool{})
		result.Values[key] = value
		if value == nil {
			if shown && flag(field["required"]) {
				issue(key, "REQUIRED")
			}
			continue
		}
		valid := false
		switch kind {
		case "TEXT", "TEXTAREA":
			_, valid = value.(string)
		case "BOOLEAN":
			_, valid = value.(bool)
		case "NUMBER":
			_, valid = number(value)
		case "DATE":
			_, valid = date(value)
		case "SELECT":
			if selected, ok := value.(string); ok {
				for _, option := range list(field["options"]) {
					if object(option)["value"] == selected {
						valid = true
					}
				}
			}
		}
		if !valid {
			issue(key, "INVALID_TYPE")
			continue
		}
		if shown && flag(field["required"]) {
			if value, ok := value.(string); ok && strings.TrimSpace(value) == "" {
				issue(key, "REQUIRED")
			}
		}
		if value, ok := value.(string); ok && (kind == "TEXT" || kind == "TEXTAREA") {
			if code := personalTextError(key, value); code != "" {
				issue(key, code)
			}
		}
		if n, ok := number(value); ok {
			min, minOK := number(field["minimum"])
			max, maxOK := number(field["maximum"])
			if minOK && n < min || maxOK && n > max {
				issue(key, "OUT_OF_RANGE")
			}
			if contains([]string{"weightKg", "heightCm", "headCircumferenceCm", "weight", "height", "headCircumference", "measure", "encounter.weightKg", "encounter.heightCm", "encounter.headCircumferenceCm"}, key) && n <= 0 {
				issue(key, "POSITIVE_REQUIRED")
			}
			minimum := -1.0
			if contains([]string{"siblingsCount", "count", "patient.guardianInfo.siblingsCount"}, key) {
				minimum = 0
			}
			if contains([]string{"siblingOrder", "order", "patient.guardianInfo.siblingOrder"}, key) {
				minimum = 1
			}
			if minimum >= 0 && (n < minimum || n > math.MaxInt32 || n != math.RoundToEven(n)) {
				issue(key, "INVALID_INTEGER")
			}
		}
		if contains([]string{"sys_dob", "birthDate", "birth", "born", "patient.birthDate"}, key) {
			if reference.IsZero() {
				return result, fmt.Errorf("saved referenceDate is required")
			}
			born, ok := date(value)
			if !ok {
				return result, fmt.Errorf("birth date projection is not a date")
			}
			if born.After(reference) {
				issue(key, "FUTURE_BIRTH_DATE")
			}
		}
	}
	if len(violations) > 0 {
		return result, &api.Error{Status: 400, Code: "INVALID_FORM_VALUES", Message: "Las respuestas del formulario no son válidas.", Errors: violations}
	}
	for _, key := range order {
		field := fields[key].data
		shown := visible(key, result.Values, map[string]bool{})
		if field["type"] == "FORMULA" {
			if !shown {
				result.Results[key] = unavailable("NOT_APPLICABLE")
				continue
			}
			formula := object(field["formula"])
			inputs := map[string]any{}
			hidden := false
			for name, raw := range object(formula["inputs"]) {
				operand := object(raw)
				if source, ok := operand["fieldKey"].(string); ok {
					if visible(source, result.Values, map[string]bool{}) {
						inputs[name] = result.Values[source]
					} else {
						hidden = true
					}
				} else if source, ok := operand["context"].(string); ok {
					inputs[name] = context[source]
				} else {
					inputs[name] = operand["constant"]
				}
			}
			if hidden {
				result.Results[key] = unavailable("NOT_APPLICABLE")
			} else {
				result.Results[key] = Calculate(text(formula["kind"]), text(formula["operation"]), inputs)
			}
		} else if shown && result.Values[key] != nil {
			projectValue(key, result.Values[key], result.Projections)
		}
	}
	if _, ok := result.Projections["patient.fullName"]; !ok {
		first, firstOK := result.Projections["patient.firstName"].(string)
		last, lastOK := result.Projections["patient.lastName"].(string)
		if firstOK || lastOK {
			result.Projections["patient.fullName"] = strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
		}
	}
	return result, nil
}

func projectValue(key string, value any, projections map[string]any) {
	if strings.HasPrefix(key, "patient.") || strings.HasPrefix(key, "encounter.") {
		if strings.HasPrefix(key, "patient.familyHistory.") {
			value = historyLines(value)
		}
		projections[key] = value
		return
	}
	target := ""
	switch key {
	case "sys_firstName", "firstName":
		target = "patient.firstName"
	case "sys_lastName", "lastName":
		target = "patient.lastName"
	case "sys_dni", "nationalId":
		target = "patient.nationalId"
	case "sys_dob", "birthDate", "born":
		target = "patient.birthDate"
	case "sys_sex", "gender", "sex":
		target = "patient.gender"
	case "sys_phone", "phone":
		target = "patient.phone"
	case "sys_email", "email":
		target = "patient.email"
	case "sys_insurance", "insurance", "obraSocial", "obra_social", "cobertura":
		target = "patient.insurance"
	case "sys_insurance_plan", "insurancePlan", "planName", "plan":
		target = "patient.insurance.plan"
	case "sys_insurance_affiliate", "sys_affiliate", "affiliateNumber", "nroAfiliado", "numeroAfiliado", "afiliado":
		target = "patient.insurance.affiliate"
	case "insuranceInfo":
		data := object(value)
		for field, target := range map[string]string{"providerName": "patient.insurance", "planName": "patient.insurance.plan", "affiliateNumber": "patient.insurance.affiliate"} {
			if data[field] != nil {
				projections[target] = data[field]
			}
		}
		return
	case "identity", "fullName":
		target = "patient.fullName"
	case "address", "bloodType":
		target = "patient." + key
	case "primaryGuardian", "motherFullName", "fatherFullName", "primaryContactPhone", "hasSiblings", "livingArrangement":
		target = "patient.guardianInfo." + key
	case "siblingsCount", "count":
		target = "patient.guardianInfo.siblingsCount"
	case "siblingOrder", "order":
		target = "patient.guardianInfo.siblingOrder"
	case "paternalDiseases", "father":
		target = "patient.familyHistory.paternalDiseases"
		value = historyLines(value)
	case "maternalDiseases", "siblingDiseases":
		target = "patient.familyHistory." + key
		value = historyLines(value)
	case "chiefComplaint", "complaint":
		target = "encounter.chiefComplaint"
	case "presentIllness", "historyOfPresentIllness":
		target = "encounter.historyOfPresentIllness"
	case "physicalExam", "physicalExamination":
		target = "encounter.physicalExamination"
	case "diagnosis", "primaryDiagnosisDescription":
		target = "encounter.primaryDiagnosisDescription"
	case "treatment", "treatmentPlan":
		target = "encounter.treatmentPlan"
	case "followUpInstructions", "followUpDate":
		target = "encounter." + key
	case "weightKg", "weight":
		target = "encounter.weightKg"
	case "heightCm", "height":
		target = "encounter.heightCm"
	case "headCircumferenceCm", "headCircumference":
		target = "encounter.headCircumferenceCm"
	}
	if target != "" {
		projections[target] = value
	}
}
func historyLines(value any) any {
	if text, ok := value.(string); ok {
		result := []string{}
		for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				result = append(result, line)
			}
		}
		return result
	}
	return value
}
func minimalPatientForm() map[string]any {
	fields := []any{}
	for _, key := range mandatory {
		kind := "TEXT"
		if key == "sys_dob" {
			kind = "DATE"
		}
		if key == "sys_sex" {
			kind = "SELECT"
		}
		field := map[string]any{"key": key, "label": key, "type": kind, "required": contains(mandatory[:5], key), "system": true}
		if kind == "SELECT" {
			options := []any{}
			for _, value := range []string{"MALE", "FEMALE", "OTHER", "UNKNOWN"} {
				options = append(options, map[string]any{"value": value, "label": value})
			}
			field["options"] = options
		}
		fields = append(fields, field)
	}
	return map[string]any{"categories": []any{map[string]any{"key": "sys_patient_profile", "title": "Datos Personales", "enabled": true, "system": true, "fields": fields}}}
}
