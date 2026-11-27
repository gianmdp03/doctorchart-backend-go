package clinical

import (
	"encoding/json"

	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

func storedProjections(snapshot, values json.RawMessage) (map[string]any, error) {
	form, err := wizard.Decode(snapshot)
	if err != nil {
		return nil, err
	}
	answers, err := wizard.Decode(values)
	if err != nil {
		return nil, err
	}
	return wizard.ClinicalProjections(form, answers), nil
}

func projectEncounter(e *Encounter) error {
	projected, err := storedProjections(e.Snapshot, e.Values)
	if err != nil {
		return err
	}
	return projectEncounterText(e, projected)
}

// Only replace properties represented by this snapshot. Independent legacy
// columns and follow-up dates retain their existing semantics.
func projectEncounterText(e *Encounter, projected map[string]any) error {
	for key, destination := range map[string]**string{
		"chiefComplaint": &e.Complaint, "historyOfPresentIllness": &e.History,
		"physicalExamination": &e.Physical, "primaryDiagnosisDescription": &e.Diagnosis,
		"treatmentPlan": &e.Treatment, "followUpInstructions": &e.FollowUp,
		"disease": &e.Disease,
	} {
		if value, exists := projected["encounter."+key]; exists {
			text, err := nullableText(value)
			if err != nil {
				return err
			}
			*destination = text
		}
	}
	for key, destination := range map[string]**float64{"weightKg": &e.Measurements.Weight, "heightCm": &e.Measurements.Height, "headCircumferenceCm": &e.Measurements.Head} {
		if value, exists := projected["encounter."+key]; exists {
			if value == nil {
				*destination = nil
				continue
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			var measurement float64
			if err := json.Unmarshal(raw, &measurement); err != nil {
				return err
			}
			*destination = &measurement
		}
	}
	return nil
}

func projectPatientIdentity(p *Patient) error {
	projected, err := storedProjections(p.Snapshot, p.Values)
	if err != nil {
		return err
	}
	if name, ok := projected["patient.fullName"].(string); ok {
		p.FullName = name
	}
	for key, destination := range map[string]**string{"nationalId": &p.NationalID, "gender": &p.Gender, "phone": &p.Phone, "email": &p.Email} {
		if value, exists := projected["patient."+key]; exists {
			text, err := nullableText(value)
			if err != nil {
				return err
			}
			*destination = text
		}
	}
	return nil
}

// A revision/export keeps its original content alongside explicitly derived
// properties; it never replaces the stored JSON or fabricates a clinical edit.
func historicalProjections(raw json.RawMessage) (map[string]any, error) {
	content, err := wizard.Decode(raw)
	if err != nil {
		return nil, err
	}
	snapshot, err := json.Marshal(content["form_definition_snapshot"])
	if err != nil {
		return nil, err
	}
	values, err := json.Marshal(content["form_values"])
	if err != nil {
		return nil, err
	}
	return storedProjections(snapshot, values)
}

func chartRowProjections(section string, raw []byte) ([]byte, error) {
	if section != "ficha" && section != "consultas" && section != "revisiones" {
		return raw, nil
	}
	content, err := wizard.Decode(raw)
	if err != nil {
		return nil, err
	}
	origin := json.RawMessage(raw)
	if section == "revisiones" {
		if content["entity"] != "ENCOUNTER" && content["entity"] != "PATIENT" {
			return raw, nil
		}
		origin, err = json.Marshal(content["previous_content"])
		if err != nil {
			return nil, err
		}
	}
	content["derivedProjections"], err = historicalProjections(origin)
	if err != nil {
		return nil, err
	}
	return json.Marshal(content)
}
