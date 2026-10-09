package clinical

import (
	"encoding/json"
	"github.com/syntah/doctorchart-backend-go/internal/api"
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
func TestWHODayIndexAndRounding(t *testing.T) {
	tables, err := loadWHO()
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range []string{"wfa", "lhfa", "hcfa"} {
		for _, sex := range []string{"MALE", "FEMALE", "UNKNOWN"} {
			key := metric + "_male"
			if sex == "FEMALE" {
				key = metric + "_female"
			}
			table := tables[key]
			value := table[0].m
			evaluation := tables.evaluate(metric, sex, -10, &value)
			if evaluation.Z != 0 || evaluation.Percentile != 50 {
				t.Fatalf("median: %+v", evaluation)
			}
			value = table[len(table)-1].m
			evaluation = tables.evaluate(metric, sex, 100000, &value)
			if evaluation.Z != 0 || evaluation.Percentile != 50 {
				t.Fatalf("clamped median: %+v", evaluation)
			}
		}
	}
	if tables.evaluations("FEMALE", 0, Measurements{}) != nil {
		t.Fatal("empty measurements evaluated")
	}
	if javaRound(-1.5) != -1 || javaRound(1.5) != 2 {
		t.Fatal("Java rounding semantics differ")
	}
}
func TestExistingPatientHistoryMaterialization(t *testing.T) {
	p := Patient{Personal: json.RawMessage(`{"birthMeasurements":{"weightKg":3.2}}`), Perinatal: json.RawMessage(`{"gestationCount":2}`)}
	if err := normalizePatientHistory(&p); err != nil {
		t.Fatal(err)
	}
	var personal map[string]any
	if err := json.Unmarshal(p.Personal, &personal); err != nil {
		t.Fatal(err)
	}
	if len(personal) != 5 || personal["habitualMedication"] != nil {
		t.Fatalf("history: %s", p.Personal)
	}
	birth, ok := personal["birthMeasurements"].(map[string]any)
	if !ok || len(birth) != 3 || birth["heightCm"] != nil || birth["weightKg"] != 3.2 {
		t.Fatalf("birth: %v", birth)
	}
	if p.Emergency != nil {
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
