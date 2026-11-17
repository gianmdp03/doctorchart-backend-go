package wizard

import (
	"os"
	"reflect"
	"testing"
)

func TestProvisionalSpecialtyTemplates(t *testing.T) {
	v, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	history, err := Decode(clinicalHistoryJSON)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"cardiology", "endocrinology", "gastroenterology", "pulmonology", "urology", "otolaryngology", "gynecology", "allergy_immunology", "neurology", "rheumatology", "traumatology_orthopedics", "geriatrics", "internal_medicine", "ophthalmology", "psychiatry", "obstetrics"} {
		t.Run(code, func(t *testing.T) {
			raw, err := os.ReadFile("../../templates/" + code + ".provisional.json")
			if err != nil {
				t.Fatal(err)
			}
			template, err := Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			create := object(template["createSpecialty"])
			if template["status"] != "PROVISIONAL_TEST_ONLY" || create["code"] != code {
				t.Fatal("unexpected template status or code")
			}
			definition := object(create["definition"])
			if err := v.Validate(definition); err != nil {
				t.Fatal(err)
			}
			for _, section := range []string{"patientForm", "encounterForm"} {
				foundHistory := false
				for _, rawCategory := range list(object(definition[section])["categories"]) {
					c := object(rawCategory)
					if c["key"] == "sys_clinical_history" {
						foundHistory = reflect.DeepEqual(c, history)
					}
					for _, rawField := range list(c["fields"]) {
						f := object(rawField)
						if f["system"] != true && f["required"] != false || f["type"] == "FORMULA" {
							t.Fatalf("unexpected clinical requirement or formula: %v", f["key"])
						}
						for _, property := range []string{"default", "defaultValue", "minimum", "maximum", "formula"} {
							if _, exists := f[property]; exists {
								t.Fatalf("unexpected preset or clinical rule on %v: %s", f["key"], property)
							}
						}
					}
				}
				if !foundHistory {
					t.Fatalf("%s must reuse the common clinical history", section)
				}
			}
			form := object(definition["encounterForm"])
			fields, _ := formFields(form)
			values := map[string]any{"chiefComplaint": "Control sintético"}
			if code == "cardiology" {
				values["symptomDetails"] = "Oculto"
			}
			result, err := v.Evaluate(form, values, nil)
			if err != nil || result.Values["symptomDetails"] != nil || result.Projections["encounter.chiefComplaint"] != "Control sintético" {
				t.Fatalf("consultation without measurements: %+v %v", result, err)
			}
			for key, target := range map[string]string{
				"chiefComplaint": "chiefComplaint", "presentIllness": "historyOfPresentIllness", "physicalExam": "physicalExamination",
				"diagnosis": "primaryDiagnosisDescription", "treatment": "treatmentPlan", "followUpInstructions": "followUpInstructions", "followUpDate": "followUpDate",
			} {
				if _, present := fields[key]; !present {
					continue
				}
				value := "Dato sintético"
				if key == "followUpDate" {
					value = "2099-01-15"
				}
				evaluated, err := v.Evaluate(form, map[string]any{key: value}, nil)
				if err != nil || evaluated.Projections["encounter."+target] != value {
					t.Fatalf("projection %s: %+v %v", key, evaluated, err)
				}
			}
			if code == "cardiology" || code == "endocrinology" {
				evaluated, err := v.Evaluate(form, map[string]any{"weightKg": 70.0, "heightCm": 170.0}, nil)
				if err != nil || evaluated.Projections["encounter.weightKg"] != 70.0 || evaluated.Projections["encounter.heightCm"] != 170.0 {
					t.Fatalf("measurement projections: %+v %v", evaluated, err)
				}
			}
			if code == "pulmonology" {
				fields, _ := formFields(form)
				f := fields["oxygenSaturation"].data
				if f["type"] != "NUMBER" || f["unit"] != "%" || result.Values["oxygenSaturation"] != nil {
					t.Fatalf("saturation must remain optional, unfilled and expressed in percent: %v", f)
				}
				evaluated, err := v.Evaluate(form, map[string]any{"oxygenSaturation": 95.0}, nil)
				if err != nil || evaluated.Values["oxygenSaturation"] != 95.0 || evaluated.Projections["encounter.oxygenSaturation"] != nil {
					t.Fatalf("saturation is a custom value, not a new projection: %+v %v", evaluated, err)
				}
			}
			empty, err := v.Evaluate(form, map[string]any{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range empty.Values {
				if value != nil {
					t.Fatalf("empty consultation must not imply a clinical answer: %s=%v", key, value)
				}
			}
			if code == "otolaryngology" || code == "traumatology_orthopedics" {
				for _, side := range []string{"RIGHT", "LEFT", "BILATERAL", "UNKNOWN", "NOT_APPLICABLE"} {
					evaluated, err := v.Evaluate(form, map[string]any{"laterality": side}, nil)
					if err != nil || evaluated.Values["laterality"] != side {
						t.Fatalf("laterality %s: %+v %v", side, evaluated, err)
					}
				}
				if _, err := v.Evaluate(form, map[string]any{"laterality": "INVALID"}, nil); err == nil {
					t.Fatal("unsupported laterality accepted")
				}
			}
			if code == "gynecology" {
				patientForm := object(definition["patientForm"])
				for _, status := range []string{"KNOWN", "UNKNOWN", "NOT_REVIEWED", "NOT_APPLICABLE"} {
					input := map[string]any{"sys_firstName": "Prueba", "sys_lastName": "Sintética", "sys_dni": "12345678", "sys_dob": "1980-01-01", "sys_sex": "MALE", "menstrualDataStatus": status}
					evaluated, err := v.Evaluate(patientForm, input, map[string]any{"referenceDate": "2026-11-16"})
					if err != nil || evaluated.Values["menstrualDataStatus"] != status || evaluated.Values["menstrualHistory"] != nil || evaluated.Values["reproductiveHistory"] != nil {
						t.Fatalf("optional reproductive information %s: %+v %v", status, evaluated, err)
					}
				}
			}
			if code == "allergy_immunology" {
				for _, certainty := range []string{"SUSPECTED", "CONFIRMED", "UNDETERMINED"} {
					evaluated, err := v.Evaluate(form, map[string]any{"suspectedExposure": "Exposición sintética", "allergicHypothesisCertainty": certainty}, nil)
					if err != nil || evaluated.Values["allergicHypothesisCertainty"] != certainty || evaluated.Values["sys_allergyStatus"] != nil || evaluated.Values["sys_allergies"] != nil {
						t.Fatalf("hypothesis must not write known allergies: %+v %v", evaluated, err)
					}
				}
			}
			if key := map[string]string{"neurology": "symptomOnsetDate", "traumatology_orthopedics": "injuryEventDate"}[code]; key != "" {
				evaluated, err := v.Evaluate(form, map[string]any{key: "2026-01-15"}, nil)
				if err != nil || evaluated.Values[key] != "2026-01-15" || empty.Values[key] != nil {
					t.Fatalf("optional event date: %+v %v", evaluated, err)
				}
				if _, err := v.Evaluate(form, map[string]any{key: "desconocida"}, nil); err == nil {
					t.Fatal("unknown dates must remain blank, not be coerced into a date")
				}
			}
			if code == "rheumatology" {
				fields, _ := formFields(form)
				if fields["stiffnessDurationMinutes"].data["unit"] != "min" {
					t.Fatal("stiffness duration must display minutes")
				}
				for _, duration := range []float64{0, 15} {
					evaluated, err := v.Evaluate(form, map[string]any{"stiffnessDurationMinutes": duration, "functionalImpact": "Descripción sintética"}, nil)
					if err != nil || evaluated.Values["stiffnessDurationMinutes"] != duration || empty.Values["stiffnessDurationMinutes"] != nil || evaluated.Projections["encounter.stiffnessDurationMinutes"] != nil {
						t.Fatalf("optional raw duration: %+v %v", evaluated, err)
					}
				}
			}
			if code == "geriatrics" {
				input := map[string]any{"sys_firstName": "Paciente", "sys_lastName": "Sintético", "sys_dni": "12345678", "sys_dob": "1980-01-01", "sys_sex": "UNKNOWN", "informantName": "Informante de prueba", "informantRelationship": "Vínculo informado", "informantContact": "informante@local.test"}
				evaluated, err := v.Evaluate(object(definition["patientForm"]), input, map[string]any{"referenceDate": "2026-11-16"})
				if err != nil || evaluated.Values["informantName"] != input["informantName"] || evaluated.Values["baselineFunction"] != nil || evaluated.Projections["patient.firstName"] != "Paciente" || evaluated.Projections["patient.guardianInfo.primaryGuardian"] != nil {
					t.Fatalf("informant data must remain optional and separate from patient identity: %+v %v", evaluated, err)
				}
			}
			if code == "internal_medicine" {
				if _, exists := fields["followUp"]; exists {
					t.Fatal("new internal medicine must use the canonical followUpInstructions key")
				}
				if _, exists := fields["followUpInstructions"]; !exists {
					t.Fatal("canonical follow-up field missing")
				}
			}
			if code == "ophthalmology" {
				for _, side := range []string{"Right", "Left"} {
					other := "Left"
					if side == "Left" {
						other = "Right"
					}
					input := map[string]any{"visualAcuity" + side: "20/40", "visualAcuityMethod" + side: "Método y distancia documentados", "visualCorrection" + side: "Corrección informada", "intraocularPressure" + side: 15.0, "tonometryMethod" + side: "Método documentado"}
					evaluated, err := v.Evaluate(form, input, nil)
					if err != nil || evaluated.Values["visualAcuity"+side] != "20/40" || evaluated.Values["visualAcuity"+other] != nil || evaluated.Values["intraocularPressure"+other] != nil || fields["intraocularPressure"+side].data["unit"] != "mmHg" {
						t.Fatalf("unilateral measurement: %+v %v", evaluated, err)
					}
				}
			}
			if code == "psychiatry" {
				if _, exists := fields["physicalExam"]; exists {
					t.Fatal("mental status must not masquerade as physical examination")
				}
				evaluated, err := v.Evaluate(form, map[string]any{"mentalStatusExam": "Descripción ficticia", "safetyAssessment": "Evaluación profesional ficticia"}, nil)
				if err != nil || evaluated.Values["mentalStatusExam"] != "Descripción ficticia" || evaluated.Projections["encounter.physicalExamination"] != nil {
					t.Fatalf("mental status semantics: %+v %v", evaluated, err)
				}
			}
			if code == "obstetrics" {
				first, err := v.Evaluate(form, map[string]any{"pregnancyReference": "Referencia A", "documentedGestationalAge": "Expresión informada A"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				second, err := v.Evaluate(form, map[string]any{"pregnancyReference": "Referencia B", "documentedGestationalAge": "Expresión informada B", "gestationalAgeReferenceDate": "2026-01-15"}, nil)
				if err != nil || first.Values["pregnancyReference"] != "Referencia A" || first.Values["documentedGestationalAge"] != "Expresión informada A" || first.Values["obstetricRelevantDate"] != nil || first.Values["gestationalAgeReferenceDate"] != nil || second.Values["pregnancyReference"] != "Referencia B" || second.Values["documentedGestationalAge"] != "Expresión informada B" {
					t.Fatalf("independent pregnancy narratives: %+v %+v %v", first, second, err)
				}
			}
		})
	}
}
