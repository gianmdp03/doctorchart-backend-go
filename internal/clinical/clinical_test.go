package clinical

import (
	"encoding/json"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPagePolicy(t *testing.T) {
	for _, test := range []struct {
		query  string
		want   string
		reject bool
	}{
		{"", "e.encounter_date DESC,e.id DESC", false},
		{"sort=id,asc", "e.id ASC", false},
		{"sort=encounterDate,asc&sort=id,desc", "e.encounter_date ASC,e.id DESC", false},
		{"sort=fullName", "", true},
		{"sort=id,ignorecase", "", true},
		{"size=101", "", true},
		{"page=2147483647&size=100", "", true},
		{"sort=id%3BDROP%20TABLE%20patients", "", true},
	} {
		t.Run(test.query, func(t *testing.T) {
			query, err := url.ParseQuery(test.query)
			if err != nil {
				t.Fatal(err)
			}
			page, err := pageFor(query, map[string]string{"encounterDate": "encounter_date", "id": "id"}, "e", "encounter_date", "DESC")
			if (err != nil) != test.reject {
				t.Fatalf("reject=%v err=%v", test.reject, err)
			}
			if err == nil && page.order != test.want {
				t.Fatalf("order=%q", page.order)
			}
		})
	}
}
func TestAgeBoundaries(t *testing.T) {
	today := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		birth     string
		months    int
		group     string
		dashboard string
	}{
		{"2026-03-01", 0, "NEONATAL", "1 mes"},
		{"2026-01-31", 1, "NEONATAL", "1 mes"},
		{"2025-02-28", 12, "INFANT", "1 año"},
		{"2024-02-29", 24, "CHILD", "2 años"},
		{"2008-03-01", 216, "ADULT", "18 años"},
	} {
		birth, err := time.Parse("2006-01-02", test.birth)
		if err != nil {
			t.Fatal(err)
		}
		months := ageMonths(birth, today)
		if months == nil || *months != test.months || ageGroup(months) != test.group || dashboardAge(birth, today) != test.dashboard {
			t.Fatalf("birth %s: months=%v group=%s dashboard=%s", test.birth, months, ageGroup(months), dashboardAge(birth, today))
		}
	}
}
func TestPrescriptionStrictOrderedContent(t *testing.T) {
	decode := func(raw string) map[string]any {
		var body map[string]any
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	raw := `{"type":"PRESCRIPTION","prescriptions":[{"medicationName":" A ","dosage":" 5 mg ","administrationRoute":" oral ","frequency":" diaria ","durationDays":5},{"medicationName":"B","dosage":"10 mg","administrationRoute":"oral","frequency":"diaria"}],"diagnosticOrders":[" Estudio B ","Estudio A"]}`
	p, err := parsePrescription(decode(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Medications) != 2 || p.Medications[0].Name != "A" || p.Medications[1].Name != "B" || p.Medications[0].Days == nil || *p.Medications[0].Days != 5 || p.Medications[1].Days != nil || p.Orders[0] != "Estudio B" || p.Instructions != "" {
		t.Fatalf("unexpected prescription: %+v", p)
	}
	body := decode(raw)
	body["prescriptions"].([]any)[0].(map[string]any)["durationDays"] = json.Number("5.0")
	if _, err := parsePrescription(body); err == nil {
		t.Fatal("accepted fractional duration")
	}
	body = decode(raw)
	body["authorUserId"] = "caller"
	if _, err := parsePrescription(body); err == nil {
		t.Fatal("accepted caller identity")
	}
}
func TestInstantJSONPrecision(t *testing.T) {
	for _, text := range []string{"2026-01-01T00:00:00Z", "2026-01-01T00:00:00.120Z", "2026-01-01T00:00:00.120450Z", "2026-01-01T00:00:00.123450700Z"} {
		value, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			t.Fatal(err)
		}
		if got := api.FormatInstant(value); got != text {
			t.Fatalf("got %s, want %s", got, text)
		}
	}
}
func TestGrowthReferenceRequiresCanonicalSexOrConfirmation(t *testing.T) {
	male := "MALE"
	p := Patient{Gender: &male}
	sex, source, err := growthReferenceSex(p, "")
	if err != nil || sex != "" || source != "" {
		t.Fatal("legacy gender was treated as anthropometric sex")
	}
	p.Values = json.RawMessage(`{"sys_sex":"MALE"}`)
	sex, source, err = growthReferenceSex(p, "")
	if err != nil || sex != "MALE" || source != "SYSTEM_SEX" {
		t.Fatal(sex, source, err)
	}
	p.Values = json.RawMessage(`{"sys_sex":"UNKNOWN"}`)
	sex, _, err = growthReferenceSex(p, "")
	if err != nil || sex != "" {
		t.Fatal("unknown sex assumed")
	}
	sex, source, err = growthReferenceSex(p, "FEMALE")
	if err != nil || sex != "FEMALE" || source != "VIEW_CONFIRMATION" {
		t.Fatal(sex, source, err)
	}
}
func TestGrowthQueryRejectsInvalidReferencesAndModes(t *testing.T) {
	for _, query := range []string{"referenceSex=OTHER", "referenceSex=female", "measurementModes=%7B%22BIRTH%22:%22UNKNOWN%22%7D", "measurementModes=%7B%22-1%22:%22STANDING%22%7D", "measurementModes=broken"} {
		r := httptest.NewRequest("GET", "/patients/1/growth?"+query, nil)
		if _, err := parseGrowthOptions(r); err == nil {
			t.Fatal("accepted", query)
		}
	}
	r := httptest.NewRequest("GET", "/patients/1/growth?referenceSex=FEMALE&measurementModes=%7B%22BIRTH%22:%22RECUMBENT%22,%227%22:%22STANDING%22%7D", nil)
	options, err := parseGrowthOptions(r)
	if err != nil || options.sex != "FEMALE" || options.modes["7"] != "STANDING" {
		t.Fatal(options, err)
	}
}
func TestPatientHistoryDoesNotMaterializeAbsentFields(t *testing.T) {
	p := Patient{Personal: json.RawMessage(`{"birthMeasurements":{"weightKg":3.2}}`)}
	if err := normalizePatientHistory(&p); err != nil {
		t.Fatal(err)
	}
	var personal map[string]any
	if err := json.Unmarshal(p.Personal, &personal); err != nil {
		t.Fatal(err)
	}
	if len(personal) != 1 || personal["habitualMedication"] != nil {
		t.Fatalf("history: %s", p.Personal)
	}
	birth, ok := personal["birthMeasurements"].(map[string]any)
	if !ok || len(birth) != 1 || birth["heightCm"] != nil || birth["weightKg"] != 3.2 {
		t.Fatalf("birth: %v", birth)
	}
	if p.Insurance != nil {
		t.Fatal("null history became an object")
	}
	before := string(p.Personal)
	if err := normalizePatientHistory(&p); err != nil {
		t.Fatal(err)
	}
	if string(p.Personal) != before {
		t.Fatal("history normalization is not stable")
	}
}
