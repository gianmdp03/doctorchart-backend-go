package wizard

import (
	"encoding/json"
	"testing"
)

func TestHeightMeasurementModeFollowsHeight(t *testing.T) {
	validator, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	form, err := Decode(json.RawMessage(`{"categories":[
		{"key":"reason","title":"Motivo","enabled":true,"fields":[
			{"key":"heightMeasurementMode","label":"Posición anterior","type":"TEXT","required":false}
		]},
		{"key":"measurements","title":"Mediciones","enabled":true,"fields":[
			{"key":"heightCm","label":"Talla","type":"NUMBER","required":false},
			{"key":"weightKg","label":"Peso","type":"NUMBER","required":false},
			{"key":"followUpDate","label":"Próximo control","type":"DATE","required":true}
		]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	linked := NewEncounterForm(form)
	categories := list(linked["categories"])
	fields := list(object(categories[1])["fields"])
	if len(list(object(categories[0])["fields"])) != 0 || len(fields) != 4 ||
		object(fields[1])["key"] != heightMeasurementModeKey {
		t.Fatalf("measurement position did not follow height: %v", categories)
	}
	result, err := validator.Evaluate(linked, map[string]any{
		"heightCm": json.Number("80"), heightMeasurementModeKey: "RECUMBENT",
	}, nil)
	result.DefaultStandingHeight()
	if err != nil || result.Values[heightMeasurementModeKey] != "RECUMBENT" {
		t.Fatalf("position was not accepted: %v", err)
	}
	result, err = validator.Evaluate(linked, map[string]any{"heightCm": json.Number("110")}, nil)
	result.DefaultStandingHeight()
	if err != nil || result.Values[heightMeasurementModeKey] != "STANDING" || result.Values["followUpDate"] != nil {
		t.Fatalf("new encounter did not default to standing without an appointment: %+v, %v", result, err)
	}
	result, err = validator.Evaluate(linked, map[string]any{"weightKg": json.Number("20")}, nil)
	result.DefaultStandingHeight()
	if err != nil || result.Values[heightMeasurementModeKey] != nil {
		t.Fatalf("position was populated without a height: %+v, %v", result, err)
	}
	withoutHeight := NewEncounterForm(map[string]any{"categories": list(form["categories"])[:1]})
	if len(list(object(list(withoutHeight["categories"])[0])["fields"])) != 0 ||
		len(list(object(list(form["categories"])[0])["fields"])) != 1 {
		t.Fatal("orphan position survived or the stored definition was modified")
	}
}
