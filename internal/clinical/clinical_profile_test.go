package clinical

import (
	"encoding/json"
	"strings"
	"testing"
)

func profileAdd(kind, date string) map[string]any {
	return map[string]any{"operation": "ADD", "kind": kind, "text": "Dato sintético", "details": "Detalle clínico", "date": date, "certainty": "REPORTED"}
}

func TestClinicalProfileDatesAndValidation(t *testing.T) {
	for _, test := range []struct {
		name, kind, date string
		valid            bool
	}{
		{"today", "ALLERGY", "2026-10-31", true},
		{"historical", "MEDICATION", "2020-02-29", true},
		{"pending future", "PENDING", "2027-01-01", true},
		{"future clinical fact", "PROBLEM", "2026-11-01", false},
		{"invalid leap day", "ALLERGY", "2025-02-29", false},
		{"before birth", "ALLERGY", "2019-12-31", false},
		{"unknown category", "OTHER", "2026-10-31", false},
		{"missing date", "ALLERGY", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := ClinicalProfile{Entries: []ClinicalEntry{}}
			err := updateClinicalProfile(&profile, nil, profileAdd(test.kind, test.date), "2020-01-01", "2026-10-31", "2026-10-31T15:00:00Z", "doctor")
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
			if !test.valid && len(profile.Entries) != 0 {
				t.Fatal("invalid request mutated profile")
			}
		})
	}
	for key, value := range map[string]any{"text": " ", "certainty": "ASSUMED", "details": strings.Repeat("x", 2001), "unexpected": true} {
		request := profileAdd("ALLERGY", "2026-10-31")
		request[key] = value
		if err := updateClinicalProfile(&ClinicalProfile{}, nil, request, "2020-01-01", "2026-10-31", "stamp", "doctor"); err == nil {
			t.Fatalf("accepted invalid %s", key)
		}
	}
}

func TestClinicalProfileAllergyAbsenceAndResolution(t *testing.T) {
	profile := ClinicalProfile{Entries: []ClinicalEntry{}}
	apply := func(request map[string]any) error {
		return updateClinicalProfile(&profile, nil, request, "2020-01-01", "2026-10-31", "2026-10-31T15:00:00Z", "doctor")
	}
	none := map[string]any{"operation": "NO_KNOWN_ALLERGIES"}
	if err := apply(none); err != nil || profile.NoKnownAllergiesAt == "" {
		t.Fatalf("absence not recorded: %v", err)
	}
	if err := apply(profileAdd("ALLERGY", "2026-10-01")); err != nil {
		t.Fatal(err)
	}
	entry := profile.Entries[0]
	if profile.NoKnownAllergiesAt != "" || entry.Date != "2026-10-01" || entry.RecordedAt != "2026-10-31T15:00:00Z" || entry.Author != "doctor" || entry.ID == "" {
		t.Fatalf("incorrect provenance: %+v", entry)
	}
	if err := apply(none); err == nil {
		t.Fatal("allowed contradicting active allergy")
	}
	if err := apply(map[string]any{"operation": "CONFIRM", "entryId": entry.ID}); err != nil {
		t.Fatal(err)
	}
	if profile.Entries[0].Date != entry.Date || profile.Entries[0].VerifiedAt != "2026-10-31T15:00:00Z" || profile.Entries[0].Certainty != "CONFIRMED" {
		t.Fatal("confirmation lost original date or did not record verification")
	}
	if err := apply(map[string]any{"operation": "RESOLVE", "entryId": entry.ID}); err != nil {
		t.Fatal(err)
	}
	if profile.Entries[0].Status != "RESOLVED" || profile.Entries[0].ResolvedAt == "" || profile.Entries[0].Text != entry.Text || profile.NoKnownAllergiesAt != "" {
		t.Fatal("resolution removed history or implied absence")
	}
	if err := apply(none); err != nil {
		t.Fatal(err)
	}
	if err := apply(map[string]any{"operation": "RESOLVE", "entryId": entry.ID}); err == nil {
		t.Fatal("accepted resolving stale record")
	}
	legacy := []ClinicalEntry{{Kind: "ALLERGY", Status: "HISTORICAL", Text: "Penicilina"}}
	if err := updateClinicalProfile(&ClinicalProfile{}, legacy, none, "2020-01-01", "2026-10-31", "stamp", "doctor"); err == nil {
		t.Fatal("historical allergy suppressed by absence confirmation")
	}
}

func TestClinicalProfilePreservesPersonalHistoryAndUnknownDates(t *testing.T) {
	raw := json.RawMessage(`{"birthMeasurements":{"weightKg":3.2},"allergies":["Penicilina"],"clinicalProfile":{"entries":[]}}`)
	profile, personal, err := readClinicalProfile(raw)
	if err != nil || profile.Entries == nil {
		t.Fatal(err)
	}
	if err := updateClinicalProfile(&profile, nil, profileAdd("PROBLEM", "2026-10-31"), "2020-01-01", "2026-10-31", "stamp", "doctor"); err != nil {
		t.Fatal(err)
	}
	personal["clinicalProfile"], err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if string(personal["birthMeasurements"]) != `{"weightKg":3.2}` || string(personal["allergies"]) != `["Penicilina"]` {
		t.Fatal("unrelated business data changed")
	}
	historical, err := historicalClinicalEntries(json.RawMessage(`["Penicilina"]`), json.RawMessage(`["Asma"]`), raw)
	if err != nil || len(historical) != 2 {
		t.Fatalf("historical deduplication: %+v %v", historical, err)
	}
	for _, item := range historical {
		if item.Date != "" || item.Certainty != "REPORTED" || item.RecordedAt != "" {
			t.Fatal("invented provenance for historical fact")
		}
	}
}
