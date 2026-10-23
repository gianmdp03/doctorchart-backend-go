package wizard

import "testing"

func TestFollowUpDateProjection(t *testing.T) {
	validator, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	key := "followUpDate"
	for _, test := range []struct {
		name  string
		value any
		valid bool
	}{
		{"future leap day", "2028-02-29", true},
		{"past date remains editable", "2000-01-01", true},
		{"clear optional control", nil, true},
		{"invalid leap day", "2027-02-29", false},
		{"Argentine date is not a wire date", "29/02/2028", false},
		{"instant is not a calendar date", "2028-02-29T00:00:00Z", false},
		{"invalid month", "2028-13-01", false},
		{"PostgreSQL has no year zero", "0000-01-01", false},
	} {
		t.Run(key+"/"+test.name, func(t *testing.T) {
			form := map[string]any{"categories": []any{map[string]any{"key": "plan", "title": "Plan", "enabled": true, "fields": []any{
				map[string]any{"key": key, "type": "DATE", "label": "Próximo control", "required": false},
			}}}}
			result, err := validator.Evaluate(form, map[string]any{key: test.value}, nil)
			if !test.valid {
				if err == nil {
					t.Fatal("invalid date accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Projections["encounter.followUpDate"] != test.value || result.Values[key] != test.value {
				t.Fatal("follow-up date lost or shifted during projection", result)
			}
			form["categories"].([]any)[0].(map[string]any)["enabled"] = false
			result, err = validator.Evaluate(form, map[string]any{key: test.value}, nil)
			if err != nil || result.Projections["encounter.followUpDate"] != nil {
				t.Fatal("hidden follow-up date projected", result, err)
			}
		})
	}
}
