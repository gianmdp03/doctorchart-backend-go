package wizard

// EnsureEncounterDiseaseField guarantees that encounterForm includes the
// system-locked optional "Enfermedad" field (`sys_disease`) inside an enabled category.
func EnsureEncounterDiseaseField(form map[string]any) map[string]any {
	if form == nil {
		return form
	}
	categories := list(form["categories"])
	if len(categories) == 0 {
		return form
	}
	for _, rawCategory := range categories {
		category := object(rawCategory)
		for _, rawField := range list(category["fields"]) {
			if text(object(rawField)["key"]) == "sys_disease" {
				return form
			}
		}
	}

	next := make(map[string]any, len(form))
	for k, v := range form {
		next[k] = v
	}
	updatedCategories := make([]any, len(categories))
	targetIdx := -1
	for i, rawCategory := range categories {
		category := object(rawCategory)
		if text(category["key"]) == "reason" {
			targetIdx = i
			break
		}
		if targetIdx == -1 && flag(category["enabled"]) {
			targetIdx = i
		}
	}
	if targetIdx == -1 {
		targetIdx = 0
	}

	diseaseField := map[string]any{
		"key":         "sys_disease",
		"label":       "Enfermedad",
		"type":        "TEXT",
		"required":    false,
		"system":      true,
		"placeholder": "Ej.: Bronquitis aguda (opcional)",
	}

	for i, rawCategory := range categories {
		category := object(rawCategory)
		if i != targetIdx {
			updatedCategories[i] = rawCategory
			continue
		}
		copiedCategory := make(map[string]any, len(category))
		for k, v := range category {
			copiedCategory[k] = v
		}
		copiedCategory["enabled"] = true
		origFields := list(category["fields"])
		newFields := make([]any, 0, len(origFields)+1)
		inserted := false
		for _, rawField := range origFields {
			newFields = append(newFields, rawField)
			if !inserted && text(object(rawField)["key"]) == "enc_chief_complaint" {
				newFields = append(newFields, diseaseField)
				inserted = true
			}
		}
		if !inserted {
			newFields = append([]any{diseaseField}, newFields...)
		}
		copiedCategory["fields"] = newFields
		updatedCategories[i] = copiedCategory
	}
	next["categories"] = updatedCategories
	return next
}

// HasEncounterDiseaseField returns true when encounterForm contains sys_disease in an enabled category.
func HasEncounterDiseaseField(definition map[string]any) bool {
	if definition == nil {
		return false
	}
	form := object(definition["encounterForm"])
	for _, rawCategory := range list(form["categories"]) {
		category := object(rawCategory)
		if !flag(category["enabled"]) {
			continue
		}
		for _, rawField := range list(category["fields"]) {
			if text(object(rawField)["key"]) == "sys_disease" {
				return true
			}
		}
	}
	return false
}
