package wizard

import (
	"encoding/json"
	"testing"
)

func historyForm(t *testing.T) map[string]any {
	t.Helper()
	category, err := Decode(clinicalHistoryJSON)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"categories": []any{category}}
}

func TestSpecialtyClinicalHistoryIsEditable(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	makeDefinition := func() map[string]any {
		patient := minimalPatientForm()
		patient["categories"] = append(list(patient["categories"]), list(historyForm(t)["categories"])...)
		return map[string]any{"schemaVersion": json.Number("1"), "patientForm": patient, "encounterForm": historyForm(t), "modulesConfig": map[string]any{"MEDICATION_CATALOG": false, "AI_ASSISTANT": false, "GROWTH_CHARTS": false}}
	}
	for _, section := range []string{"patientForm", "encounterForm"} {
		for _, change := range []string{"disabled", "renamed", "field", "option", "extra", "removedField", "removedCategory"} {
			t.Run(section+change, func(t *testing.T) {
				d := makeDefinition()
				form := object(d[section])
				cats := list(form["categories"])
				c := object(cats[len(cats)-1])
				switch change {
				case "disabled":
					c["enabled"] = false
				case "renamed":
					c["title"] = "Personalizados"
				case "field":
					object(list(c["fields"])[0])["required"] = true
				case "option":
					object(list(object(list(c["fields"])[0])["options"])[0])["label"] = "Otra etiqueta"
				case "extra":
					c["fields"] = append(list(c["fields"]), map[string]any{"key": "extra", "label": "Extra", "type": "TEXT", "required": false})
				case "removedField":
					c["fields"] = list(c["fields"])[1:]
				case "removedCategory":
					form["categories"] = cats[:len(cats)-1]
				}
				if err := v.Transition(makeDefinition(), d); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	form := minimalPatientForm()
	if len(list(WithClinicalHistory(form)["categories"])) != 1 {
		t.Fatal("removed history was reinserted")
	}
	legacy := historyForm(t)
	c := object(list(legacy["categories"])[0])
	c["system"] = true
	object(list(c["fields"])[0])["system"] = true
	normalized := WithClinicalHistory(legacy)
	if !flag(c["system"]) || flag(object(list(normalized["categories"])[0])["system"]) {
		t.Fatal("legacy locks not normalized without mutation")
	}
}

func TestClinicalHistoryValues(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	form := historyForm(t)
	for _, test := range []struct {
		name   string
		values map[string]any
		valid  bool
	}{
		{"unreviewed", map[string]any{}, true},
		{"negative", map[string]any{"sys_allergyStatus": "NONE"}, true},
		{"positive", map[string]any{"sys_allergyStatus": "PRESENT", "sys_allergies": "Penicilina: urticaria", "sys_historySource": "REPORTED"}, true},
		{"missing detail", map[string]any{"sys_allergyStatus": "PRESENT"}, false},
		{"contradiction", map[string]any{"sys_allergyStatus": "NONE", "sys_allergies": "Penicilina"}, false},
		{"unclassified", map[string]any{"sys_conditions": "Asma"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := v.Evaluate(form, test.values, map[string]any{"referenceDate": "2026-10-31"})
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
	prev := map[string]any{"sys_allergyStatus": "PRESENT", "sys_allergies": "Penicilina"}
	next := map[string]any{"chiefComplaint": "Control"}
	RetainUnreviewedHistory(prev, next)
	if next["sys_allergies"] != "Penicilina" {
		t.Fatal("old client erased allergy")
	}
	next = map[string]any{"sys_allergyStatus": "NONE"}
	RetainUnreviewedHistory(prev, next)
	if next["sys_allergies"] != nil {
		t.Fatal("explicit negative review resurrected allergy")
	}
}

func TestRemovedHistoryFieldsDoNotRemainMandatory(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	form := historyForm(t)
	category := object(list(form["categories"])[0])
	// A doctor keeps a free-text allergy field and removes its status selector.
	category["fields"] = []any{list(category["fields"])[1]}
	if _, err := v.Evaluate(form, map[string]any{"sys_allergies": "Penicilina"}, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	category["enabled"] = false
	if _, err := v.Evaluate(form, map[string]any{}, map[string]any{}); err != nil {
		t.Fatal(err)
	}
}
