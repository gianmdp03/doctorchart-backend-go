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
			{"key":"weightKg","label":"Peso","type":"NUMBER","required":false}
		]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	linked := WithHeightMeasurementMode(form)
	categories := list(linked["categories"])
	fields := list(object(categories[1])["fields"])
	if len(list(object(categories[0])["fields"])) != 0 || len(fields) != 3 ||
		object(fields[1])["key"] != heightMeasurementModeKey {
		t.Fatalf("measurement position did not follow height: %v", categories)
	}
	result, err := validator.Evaluate(linked, map[string]any{
		"heightCm": json.Number("80"), heightMeasurementModeKey: "RECUMBENT",
	}, nil)
	if err != nil || result.Values[heightMeasurementModeKey] != "RECUMBENT" {
		t.Fatalf("position was not accepted: %v", err)
	}
	withoutHeight := WithHeightMeasurementMode(map[string]any{"categories": list(form["categories"])[:1]})
	if len(list(object(list(withoutHeight["categories"])[0])["fields"])) != 0 ||
		len(list(object(list(form["categories"])[0])["fields"])) != 1 {
		t.Fatal("orphan position survived or the stored definition was modified")
	}
}
