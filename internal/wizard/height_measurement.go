package wizard

import (
	"maps"
	"slices"
)

const heightMeasurementModeKey = "heightMeasurementMode"

// WithHeightMeasurementMode pairs the optional position with the numeric height field
// of a new encounter. Existing encounter snapshots keep their original definition.
func WithHeightMeasurementMode(form map[string]any) map[string]any {
	next := maps.Clone(form)
	categories := append([]any{}, list(form["categories"])...)
	target, height := -1, -1
	for i, raw := range categories {
		category := maps.Clone(object(raw))
		if category == nil {
			continue
		}
		fields := append([]any{}, list(category["fields"])...)
		fields = slices.DeleteFunc(fields, func(raw any) bool {
			return text(object(raw)["key"]) == heightMeasurementModeKey
		})
		if target < 0 || !flag(object(categories[target])["enabled"]) && flag(category["enabled"]) {
			for j, rawField := range fields {
				field := object(rawField)
				key := text(field["key"])
				if field["type"] == "NUMBER" && (key == "heightCm" || key == "height") {
					target, height = i, j
					break
				}
			}
		}
		category["fields"] = fields
		categories[i] = category
	}
	if target >= 0 {
		category := object(categories[target])
		fields := list(category["fields"])
		mode := map[string]any{
			"key": heightMeasurementModeKey, "label": "Cómo se midió la talla",
			"type": "SELECT", "required": false,
			"options": []any{
				map[string]any{"value": "RECUMBENT", "label": "Acostado (longitud)"},
				map[string]any{"value": "STANDING", "label": "De pie (estatura)"},
			},
		}
		if condition, ok := object(fields[height])["visibleWhen"]; ok {
			mode["visibleWhen"] = condition
		}
		category["fields"] = slices.Insert(fields, height+1, any(mode))
	}
	next["categories"] = categories
	return next
}
