package wizard

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

//go:embed definition.schema.json
var definitionSchema []byte

type Validator struct{ schema *jsonschema.Schema }

func NewValidator() (*Validator, error) {
	var doc any
	if err := json.Unmarshal(definitionSchema, &doc); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("urn:doctorchart:wizard-definition:1", doc); err != nil {
		return nil, err
	}
	schema, err := compiler.Compile("urn:doctorchart:wizard-definition:1")
	if err != nil {
		return nil, err
	}
	return &Validator{schema: schema}, nil
}
func Decode(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	err := decoder.Decode(&value)
	return value, err
}
func object(value any) map[string]any { v, _ := value.(map[string]any); return v }
func list(value any) []any            { v, _ := value.([]any); return v }
func text(value any) string           { v, _ := value.(string); return v }
func flag(value any) bool             { v, _ := value.(bool); return v }

var mandatory = []string{"sys_firstName", "sys_lastName", "sys_dni", "sys_dob", "sys_sex", "sys_phone", "sys_email", "sys_insurance"}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func genderOptions(field map[string]any) bool {
	options, ok := field["options"].([]any)
	if !ok {
		return false
	}
	for _, option := range options {
		if !contains([]string{"MALE", "FEMALE", "OTHER", "UNKNOWN"}, text(object(option)["value"])) {
			return false
		}
	}
	return true
}

func (v *Validator) Validate(definition map[string]any) error {
	errorsFound := []api.FieldError{}
	issue := func(path, code string) { errorsFound = append(errorsFound, api.FieldError{Path: path, Code: code}) }
	if err := v.schema.Validate(definition); err != nil {
		var validation *jsonschema.ValidationError
		if !errors.As(err, &validation) {
			return fmt.Errorf("schema validation: %w", err)
		}
		var collect func(*jsonschema.ValidationError)
		collect = func(e *jsonschema.ValidationError) {
			if _, ok := e.ErrorKind.(*kind.OneOf); ok {
				path := ""
				for _, part := range e.InstanceLocation {
					path += "/" + strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
				}
				issue(path, "INVALID_STRUCTURE")
			}
			if len(e.Causes) > 0 {
				for _, cause := range e.Causes {
					collect(cause)
				}
				return
			}
			path := ""
			for _, part := range e.InstanceLocation {
				path += "/" + strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
			}
			count := 1
			if required, ok := e.ErrorKind.(*kind.Required); ok {
				count = len(required.Missing)
			}
			if additional, ok := e.ErrorKind.(*kind.AdditionalProperties); ok {
				count = len(additional.Properties)
			}
			for i := 0; i < count; i++ {
				issue(path, "INVALID_STRUCTURE")
			}
		}
		collect(validation)
		return invalidDefinition(errorsFound)
	}
	for _, section := range []string{"patientForm", "encounterForm"} {
		categories := list(object(definition[section])["categories"])
		categoryKeys := map[string]bool{}
		fields := map[string]field{}
		ordered := []field{}
		foundProfile := false
		for c, rawCategory := range categories {
			category := object(rawCategory)
			key := text(category["key"])
			path := fmt.Sprintf("/%s/categories/%d", section, c)
			if categoryKeys[key] {
				issue(path+"/key", "DUPLICATE_CATEGORY_KEY")
			}
			categoryKeys[key] = true
			profile := section == "patientForm" && key == "sys_patient_profile"
			if profile {
				foundProfile = true
				if !flag(category["enabled"]) {
					issue(path+"/enabled", "MISSING_PATIENT_CORE")
				}
				if !flag(category["system"]) {
					issue(path+"/system", "INVALID_SYSTEM_CATEGORY")
				}
			} else if flag(category["system"]) || key == "sys_patient_profile" {
				issue(path+"/system", "INVALID_SYSTEM_CATEGORY")
			}
			for f, rawField := range list(category["fields"]) {
				data := object(rawField)
				key := text(data["key"])
				fieldPath := fmt.Sprintf("%s/fields/%d", path, f)
				current := field{data, fieldPath}
				if _, exists := fields[key]; exists {
					issue(fieldPath+"/key", "DUPLICATE_FIELD_KEY")
				} else {
					fields[key] = current
					ordered = append(ordered, current)
				}
				if min, ok := data["minimum"].(json.Number); ok {
					if max, ok := data["maximum"].(json.Number); ok {
						left, _ := new(big.Rat).SetString(string(min))
						right, _ := new(big.Rat).SetString(string(max))
						if left != nil && right != nil && left.Cmp(right) > 0 {
							issue(fieldPath, "INVALID_RANGE")
						}
					}
				}
				values := map[string]bool{}
				for i, rawOption := range list(data["options"]) {
					value := text(object(rawOption)["value"])
					if values[value] {
						issue(fmt.Sprintf("%s/options/%d/value", fieldPath, i), "DUPLICATE_OPTION_VALUE")
					}
					values[value] = true
				}
				if profile && contains(mandatory, key) {
					if !flag(data["system"]) {
						issue(fieldPath+"/system", "INVALID_SYSTEM_FIELD")
					}
					expected := "TEXT"
					if key == "sys_dob" {
						expected = "DATE"
					}
					if key == "sys_sex" {
						expected = "SELECT"
					}
					if data["type"] != expected {
						issue(fieldPath+"/type", "INVALID_SYSTEM_FIELD")
					}
					if key == "sys_sex" && !genderOptions(data) {
						issue(fieldPath+"/options", "INVALID_GENDER_OPTION")
					}
					if contains(mandatory[:5], key) {
						_, visible := data["visibleWhen"]
						if !flag(data["required"]) || visible {
							issue(fieldPath+"/required", "MISSING_PATIENT_CORE")
						}
					}
				} else if flag(data["system"]) || contains(mandatory, key) {
					issue(fieldPath+"/system", "INVALID_SYSTEM_FIELD")
				}
			}
		}
		for _, current := range ordered {
			data := current.data
			path := current.path
			if condition, ok := data["visibleWhen"].(map[string]any); ok {
				key := text(condition["fieldKey"])
				controller, ok := fields[key]
				if !ok || controller.data["type"] != "BOOLEAN" {
					issue(path+"/visibleWhen/fieldKey", "INVALID_VISIBILITY_REFERENCE")
				} else {
					seen := map[string]bool{text(data["key"]): true}
					cursor := data
					for {
						next, ok := cursor["visibleWhen"].(map[string]any)
						if !ok {
							break
						}
						key := text(next["fieldKey"])
						if seen[key] {
							issue(path+"/visibleWhen/fieldKey", "VISIBILITY_CYCLE")
							break
						}
						seen[key] = true
						cursor = fields[key].data
						if cursor == nil {
							break
						}
					}
				}
			}
			if formula, ok := data["formula"].(map[string]any); ok {
				// JSONB input keys have stable lexical ordering; output order is compared with Spring.
				inputs := object(formula["inputs"])
				for key, rawSource := range inputs {
					source := object(rawSource)
					gender := key == "gender"
					inputPath := path + "/formula/inputs/" + key
					if ref, ok := source["fieldKey"]; ok {
						referenced, exists := fields[text(ref)]
						expected := "NUMBER"
						if gender {
							expected = "SELECT"
						}
						if !exists || referenced.data["type"] != expected {
							issue(inputPath, "INVALID_FORMULA_REFERENCE")
						} else if gender && !genderOptions(referenced.data) {
							issue(inputPath, "INVALID_FORMULA_GENDER")
						}
					} else if context, ok := source["context"]; ok {
						if (context == "patient.gender") != gender {
							issue(inputPath, "FORMULA_INPUT_TYPE_MISMATCH")
						}
					} else if gender {
						issue(inputPath, "FORMULA_INPUT_TYPE_MISMATCH")
					}
				}
			}
		}
		if section == "patientForm" {
			if !foundProfile {
				issue("/patientForm", "MISSING_PATIENT_CORE")
			} else {
				for _, key := range mandatory {
					if _, ok := fields[key]; !ok {
						issue("/patientForm", "MISSING_PATIENT_CORE")
						break
					}
				}
			}
		}
	}
	if len(errorsFound) > 0 {
		return invalidDefinition(errorsFound)
	}
	return nil
}

type field struct {
	data map[string]any
	path string
}

func invalidDefinition(fields []api.FieldError) error {
	return &api.Error{Status: 400, Code: "INVALID_WIZARD_DEFINITION", Message: "La configuración contiene campos inválidos.", Errors: fields}
}
func (v *Validator) Transition(previous, next map[string]any) error {
	if err := v.Validate(next); err != nil {
		return err
	}
	if previous == nil {
		return nil
	}
	violations := []api.FieldError{}
	for _, section := range []string{"patientForm", "encounterForm"} {
		old := map[string]map[string]any{}
		for _, category := range list(object(previous[section])["categories"]) {
			for _, rawField := range list(object(category)["fields"]) {
				data := object(rawField)
				old[text(data["key"])] = data
			}
		}
		for c, category := range list(object(next[section])["categories"]) {
			for f, rawField := range list(object(category)["fields"]) {
				data := object(rawField)
				prior := old[text(data["key"])]
				if prior != nil && (!reflect.DeepEqual(prior["type"], data["type"]) || !reflect.DeepEqual(prior["system"], data["system"])) {
					violations = append(violations, api.FieldError{Path: fmt.Sprintf("/%s/categories/%d/fields/%d/key", section, c, f), Code: "FIELD_IDENTITY_CHANGED"})
				}
			}
		}
	}
	if len(violations) > 0 {
		return invalidDefinition(violations)
	}
	return nil
}
