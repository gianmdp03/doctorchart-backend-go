package wizard

import (
	"fmt"
	"sort"
	"strings"
)

// projectionTargets uses the existing, explicit clinical vocabulary. Unknown
// custom fields remain answers, rather than becoming new clinical properties.
func projectionTargets(key string) []string {
	if key == "insuranceInfo" {
		return []string{"patient.insurance", "patient.insurance.plan", "patient.insurance.affiliate"}
	}
	projected := map[string]any{}
	projectValue(key, nil, projected)
	targets := make([]string, 0, len(projected))
	for target := range projected {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

func projectionPriority(key, target string) int {
	if strings.HasPrefix(key, "sys_") {
		return 0
	}
	if key == "followUpInstructions" {
		return 1
	}
	if key == target {
		return 2
	}
	if key == strings.TrimPrefix(strings.TrimPrefix(target, "patient."), "encounter.") {
		return 3
	}
	return 4
}

// ClinicalProjections reads a record's own snapshot without changing its answers.
// Selection is based on definition presence, before checking visibility or value:
// an empty or hidden canonical field must never resurrect an older alias.
func ClinicalProjections(form, values map[string]any) map[string]any {
	fields, _ := formFields(form)
	visible := fieldVisibility(fields, values)
	winners := map[string]string{}
	for key := range fields {
		for _, target := range projectionTargets(key) {
			previous, exists := winners[target]
			priority, oldPriority := projectionPriority(key, target), projectionPriority(previous, target)
			if !exists || priority < oldPriority || priority == oldPriority && key < previous {
				winners[target] = key
			}
		}
	}
	result := make(map[string]any, len(winners))
	for target, key := range winners {
		result[target] = nil
		field := fields[key]
		if !visible(key) || field.data["type"] == "FORMULA" {
			continue
		}
		if target == "encounter.followUpInstructions" && field.data["type"] != "TEXT" && field.data["type"] != "TEXTAREA" {
			continue
		}
		if target == "encounter.followUpInstructions" && values[key] != nil {
			if _, text := values[key].(string); !text {
				continue
			}
		}
		projected := map[string]any{}
		projectValue(key, values[key], projected)
		if target == "patient.nationalId" {
			if dni, ok := projected[target].(string); ok {
				dni = strings.TrimSpace(dni)
				if dni == "" {
					continue
				}
				projected[target] = dni
			}
		}
		result[target] = projected[target]
	}
	_, protectedFirst := fields["sys_firstName"]
	_, protectedLast := fields["sys_lastName"]
	_, named := result["patient.fullName"]
	if protectedFirst || protectedLast || !named {
		if protectedFirst || protectedLast {
			result["patient.fullName"] = nil
		}
		first, firstOK := result["patient.firstName"].(string)
		last, lastOK := result["patient.lastName"].(string)
		if firstOK || lastOK {
			result["patient.fullName"] = strings.TrimSpace(first + " " + last)
		}
	}
	return result
}

func projectionCollisions(form map[string]any) []string {
	seen := map[string]bool{}
	fields, _ := formFields(form)
	if _, first := fields["sys_firstName"]; first {
		// Full name is derived from the protected system identity.
		seen["patient.fullName"] = true
	}
	issues := []string{}
	for c, category := range list(form["categories"]) {
		for f, raw := range list(object(category)["fields"]) {
			key := text(object(raw)["key"])
			collision := false
			for _, target := range projectionTargets(key) {
				collision = collision || seen[target]
				seen[target] = true
			}
			if collision {
				issues = append(issues, fmt.Sprintf("/categories/%d/fields/%d/key", c, f))
			}
		}
	}
	return issues
}
