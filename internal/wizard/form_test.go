package wizard

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestCustomFieldValuesFollowDefinition(t *testing.T) {
	validator, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, kind, code string
		value            any
		required         bool
	}{
		{"numeric text", "NUMBER", "INVALID_TYPE", "42", false},
		{"numeric minimum", "NUMBER", "OUT_OF_RANGE", json.Number("-1"), false},
		{"numeric maximum", "NUMBER", "OUT_OF_RANGE", json.Number("101"), false},
		{"numeric zero", "NUMBER", "", json.Number("0"), true},
		{"numeric decimal", "NUMBER", "", json.Number("12.5"), false},
		{"optional omitted", "NUMBER", "", nil, false},
		{"required omitted", "NUMBER", "REQUIRED", nil, true},
		{"required blank", "TEXT", "REQUIRED", "  ", true},
		{"text numeric", "TEXT", "INVALID_TYPE", json.Number("42"), false},
		{"boolean text", "BOOLEAN", "INVALID_TYPE", "false", false},
		{"boolean false", "BOOLEAN", "", false, true},
		{"impossible date", "DATE", "INVALID_TYPE", "2024-02-31", false},
		{"leap date", "DATE", "", "2024-02-29", false},
		{"unknown option", "SELECT", "INVALID_TYPE", "b", false},
		{"configured option", "SELECT", "", "a", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			field := map[string]any{"key": "custom_relative_data", "label": "Dato familiar", "type": test.kind, "required": test.required}
			if test.kind == "NUMBER" {
				field["minimum"], field["maximum"] = json.Number("0"), json.Number("100")
			}
			if test.kind == "SELECT" {
				field["options"] = []any{map[string]any{"value": "a", "label": "Opción A"}}
			}
			form := map[string]any{"categories": []any{map[string]any{"key": "custom_family", "title": "Datos familiares", "enabled": true, "fields": []any{field}}}}
			_, err := validator.Evaluate(form, map[string]any{"custom_relative_data": test.value}, nil)
			if test.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var invalid *api.Error
			if !errors.As(err, &invalid) || invalid.Code != "INVALID_FORM_VALUES" || len(invalid.Errors) != 1 || invalid.Errors[0].Path != "/values/custom_relative_data" || invalid.Errors[0].Code != test.code {
				t.Fatalf("expected field error %s, got %#v", test.code, err)
			}
		})
	}
}

func TestPersonalInputFormats(t *testing.T) {
	validator, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		key, value, code string
	}{
		{"sys_email", "correo-invalido", "INVALID_EMAIL"},
		{"sys_email", "paciente@example.com", ""},
		{"sys_email", "", ""},
		{"sys_dni", "12.345.678", "INVALID_DNI"},
		{"sys_dni", "12345678", ""},
		{"sys_phone", "teléfono", "INVALID_PHONE"},
		{"sys_phone", "123", "INVALID_PHONE"},
		{"sys_phone", "+54 (11) 1234-5678", ""},
		{"sys_phone", "", ""},
	} {
		t.Run(test.key+"/"+test.value, func(t *testing.T) {
			values := map[string]any{"sys_firstName": "Ana", "sys_lastName": "Pérez", "sys_dni": "12345678", "sys_dob": "2000-01-01", "sys_sex": "FEMALE"}
			values[test.key] = test.value
			_, err := validator.Evaluate(minimalPatientForm(), values, map[string]any{"referenceDate": "2026-10-09"})
			if test.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var invalid *api.Error
			if !errors.As(err, &invalid) || len(invalid.Errors) != 1 || invalid.Errors[0].Code != test.code || invalid.Errors[0].Path != valuePath(test.key) {
				t.Fatalf("expected %s, got %#v", test.code, err)
			}
		})
	}
}
