package clinical

import "testing"

func TestRecordHistoryMergesPerGroupAndNeverAssumesFacts(t *testing.T) {
	patient := recordHistory(map[string]any{"sys_allergyStatus": "PRESENT", "sys_allergies": "Penicilina\r\nMani", "sys_medicationStatus": "NONE"}, "PATIENT", 10, map[string]string{"ALLERGY": "2026-10-01T15:00:00Z", "MEDICATION": "2026-10-01T15:00:00Z"})
	if len(patient["ALLERGY"].Items) != 2 || patient["ALLERGY"].Certainty != "REPORTED" {
		t.Fatal(patient)
	}
	encounter := recordHistory(map[string]any{"sys_conditionStatus": "PRESENT", "sys_conditions": "Asma", "sys_historySource": "CONFIRMED"}, "ENCOUNTER", 20, map[string]string{"PROBLEM": "2026-10-31T15:00:00Z"})
	mergeRecordHistory(patient, encounter)
	if patient["ALLERGY"].Source != "PATIENT" || patient["PROBLEM"].SourceID != 20 || patient["PROBLEM"].Certainty != "CONFIRMED" {
		t.Fatal("unreviewed consult cleared other sections", patient)
	}
	mergeRecordHistory(patient, recordHistory(map[string]any{}, "ENCOUNTER", 21, nil))
	if len(patient) != 3 {
		t.Fatal("empty encounter changed facts")
	}
	mergeRecordHistory(patient, recordHistory(map[string]any{"sys_allergyStatus": "NONE"}, "ENCOUNTER", 22, map[string]string{"ALLERGY": "2026-10-31T15:00:00Z"}))
	if patient["ALLERGY"].Status != "NONE" || len(patient["ALLERGY"].Items) != 0 {
		t.Fatal("negative review did not supersede earlier record")
	}
	mergeRecordHistory(patient, recordHistory(map[string]any{"sys_allergyStatus": "PRESENT", "sys_allergies": "Older"}, "ENCOUNTER", 23, map[string]string{"ALLERGY": "2020-01-01T15:00:00Z"}))
	if patient["ALLERGY"].Status != "NONE" {
		t.Fatal("older fact revived")
	}
}
func TestPatientHistoryDatesOnlyChangeWithClinicalAnswers(t *testing.T) {
	previous := map[string]any{"sys_conditionStatus": "PRESENT", "sys_conditions": "Asma"}
	dates := map[string]string{"PROBLEM": "2020-01-01T15:00:00Z"}
	patientHistoryDates(previous, map[string]any{"sys_conditionStatus": "PRESENT", "sys_conditions": "Asma", "sys_phone": "123456"}, dates, "2026-10-31T15:00:00Z")
	if dates["PROBLEM"] != "2020-01-01T15:00:00Z" {
		t.Fatal("demographic edit invented new confirmation")
	}
	patientHistoryDates(previous, map[string]any{"sys_conditionStatus": "NONE"}, dates, "2026-10-31T15:00:00Z")
	if dates["PROBLEM"] != "2026-10-31T15:00:00Z" {
		t.Fatal("review date not updated")
	}
}
