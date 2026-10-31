package wizard

import (
	"encoding/json"
	"testing"
)

func TestSystemClinicalHistoryDefinition(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	makeDefinition := func() map[string]any {
		return map[string]any{"schemaVersion": json.Number("1"), "patientForm": WithClinicalHistory(minimalPatientForm()), "encounterForm": WithClinicalHistory(map[string]any{"categories": []any{}}), "modulesConfig": map[string]any{"MEDICATION_CATALOG": false, "AI_ASSISTANT": false, "GROWTH_CHARTS": false}}
	}
	if err := v.Validate(makeDefinition()); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"patientForm", "encounterForm"} {
		for _, change := range []string{"disabled", "renamed", "field", "option", "extra"} {
			t.Run(section+change, func(t *testing.T) {
				d := makeDefinition()
				cats := list(object(d[section])["categories"])
				c := object(cats[len(cats)-1])
				switch change {
				case "disabled":
					c["enabled"] = false
				case "renamed":
					c["title"] = "Altered"
				case "field":
					object(list(c["fields"])[0])["required"] = true
				case "option":
					object(list(object(list(c["fields"])[0])["options"])[0])["label"] = "Altered"
				case "extra":
					c["fields"] = append(list(c["fields"]), map[string]any{"key": "extra", "label": "Extra", "type": "TEXT", "required": false})
				}
				if err := v.Validate(d); err == nil {
					t.Fatal("system block editable")
				}
			})
		}
		next := makeDefinition()
		form := object(next[section])
		categories := list(form["categories"])
		form["categories"] = categories[:len(categories)-1]
		if err := v.Transition(makeDefinition(), next); err == nil {
			t.Fatal("system block removable")
		}
	}
	original := minimalPatientForm()
	upgraded := WithClinicalHistory(original)
	if len(list(original["categories"])) != 1 || len(list(upgraded["categories"])) != 2 {
		t.Fatal("upgrade mutated saved form")
	}
	if len(list(WithClinicalHistory(upgraded)["categories"])) != 2 {
		t.Fatal("upgrade not idempotent")
	}
}

func TestClinicalHistoryValues(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	form := WithClinicalHistory(map[string]any{"categories": []any{}})
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
