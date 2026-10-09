package clinical

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestClinicalDecimalRounding(t *testing.T) {
	for _, test := range []struct {
		value    float64
		digits   int
		expected string
	}{
		{2.675, 2, "2.67"}, {1.25, 1, "1.3"}, {-1.25, 1, "-1.3"}, {-0.001, 2, "0.00"}, {0.1, 20, "0.10000000000000000555"},
	} {
		actual, err := calculationFixed(test.value, test.digits)
		if err != nil || actual != test.expected {
			t.Fatalf("%v: %q %v", test.value, actual, err)
		}
	}
	if _, err := calculationFixed(math.Inf(1), 2); err == nil {
		t.Fatal("nonfinite clinical value accepted")
	}
}
func TestPinnedSpringRenalResults(t *testing.T) {
	data, err := os.ReadFile("testdata/renal_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Inputs         map[string]any `json:"inputs"`
		Raw            float64        `json:"rawValue"`
		Formatted      string         `json:"formattedValue"`
		Interpretation interpretation `json:"interpretation"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	zone, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{zone: zone}
	if err := json.Unmarshal(calculatorJSON, &service.calculators); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		result, err := service.calculate("tfge", test.Inputs, time.Now())
		if err != nil || result.Raw != test.Raw || result.Formatted != test.Formatted || !reflect.DeepEqual(result.Interpretation, test.Interpretation) {
			t.Errorf("%v: raw %.17g expected %.17g, err %v", test.Inputs, result.Raw, test.Raw, err)
		}
	}
}
func TestClinicalCalculationInputAndBoundary(t *testing.T) {
	zone, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{zone: zone}
	if err := json.Unmarshal(calculatorJSON, &service.calculators); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		weight   string
		expected string
	}{{"10", "1000"}, {"20", "1500"}, {"21", "1520"}} {
		result, err := service.calculate("holliday_segar", map[string]any{"weightKg": json.Number(test.weight)}, at)
		if err != nil || result.Formatted != test.expected {
			t.Fatalf("%v %v", result, err)
		}
	}
	for _, inputs := range []map[string]any{{"weightKg": "10"}, {"weightKg": json.Number("0")}, {"weightKg": json.Number("10"), "extra": true}} {
		if _, err := service.calculate("holliday_segar", inputs, at); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
}
