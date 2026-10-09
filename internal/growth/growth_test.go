package growth

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func references(t *testing.T) *References {
	t.Helper()
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCatalogTwentyPDFCharts(t *testing.T) {
	r := references(t)
	catalog := Catalog()
	wantIDs := []string{"wfa06_girls", "wfa06_boys", "lfa06_girls", "lfa06_boys", "hfa06_girls", "hfa06_boys", "hcfa06_boys", "hcfa06_girls", "wfa07_girls", "wfa07_boys", "hfa07_girls", "hfa07_boys", "wfl06_girls", "wfl06_boys", "wfh06_girls", "wfh06_boys", "bfa06_girls", "bfa06_boys", "bfa07_girls", "bfa07_boys"}
	wantPages := []int{1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11}
	if len(catalog) != 20 {
		t.Fatalf("catalog count %d", len(catalog))
	}
	seen := map[string]bool{}
	for i, d := range catalog {
		if d.ID != wantIDs[i] || d.PDFPage != wantPages[i] || d.PDFNumber != i+1 || seen[d.ID] {
			t.Fatalf("PDF chart %d: %+v", i+1, d)
		}
		seen[d.ID] = true
		if len(d.Percentiles) != 5 || d.Percentiles[0] != 3 || d.Percentiles[1] != 15 || d.Percentiles[2] != 50 || d.Percentiles[3] != 85 || d.Percentiles[4] != 97 {
			t.Fatal(d.ID)
		}
		if d.X.Variable == "LENGTH" || d.X.Variable == "HEIGHT" {
			if d.X.Unit != "cm" || d.Y.Unit != "kg" {
				t.Fatal(d.ID)
			}
		} else if d.X.Variable != "AGE" || d.X.Unit != "meses" {
			t.Fatal(d.ID)
		}
		chart := r.Chart(d)
		if len(chart.Curves) == 0 || !json.Valid(chart.Provenance) {
			t.Fatal(d.ID)
		}
		for _, p := range chart.Curves {
			for j, v := range p.Values {
				if !finite(v) || v <= 0 || (j > 0 && v <= p.Values[j-1]) {
					t.Fatalf("%s: %v", d.ID, p)
				}
			}
		}
	}
	if catalog[16].AgeMaxMonths != 24 || catalog[17].AgeMaxMonths != 60 {
		t.Fatal("lost the PDF's girls/boys BMI discrepancy")
	}
}

func TestOfficialIndependentCentileWorkbooks(t *testing.T) {
	r := references(t)
	payload, err := os.ReadFile("data/official-centiles.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Dataset     string    `json:"dataset"`
		X           float64   `json:"x"`
		Percentiles []float64 `json:"percentiles"`
	}
	if err := json.Unmarshal(payload, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 90 {
		t.Fatal("insufficient independent WHO examples")
	}
	for _, c := range cases {
		p, ok := r.At(c.Dataset, c.X)
		if !ok {
			t.Fatal(c.Dataset, c.X)
		}
		for i, z := range centileZ {
			// Official expanded values are rounded to 0.001 units. Allow one
			// rounding unit plus floating-point/intermediate published precision.
			if delta := math.Abs(p.Value(z) - c.Percentiles[i]); delta > .0011 {
				t.Errorf("%s x=%v P%d got %.8f WHO %.3f delta %.8f", c.Dataset, c.X, []int{3, 15, 50, 85, 97}[i], p.Value(z), c.Percentiles[i], delta)
			}
			value := p.Value(z)
			e := p.Evaluate(value, true)
			if e == nil || e.Percentile == nil || math.Abs(*e.Percentile-float64([]int{3, 15, 50, 85, 97}[i])) > 1e-10 {
				t.Fatal("centile inversion", e)
			}
		}
	}
}

func TestAgeSexSelectionAndTransitions(t *testing.T) {
	for _, sex := range []string{"MALE", "FEMALE"} {
		for _, months := range []int{1, 6, 12, 23, 24, 25, 48, 60, 72, 120, 132, 180, 228, 240} {
			days := int(math.Floor(float64(months) * DaysPerMonth))
			selected := map[string]Definition{}
			for _, d := range Catalog() {
				if d.Applicable(sex, days) {
					if _, ok := selected[d.Indicator]; ok {
						t.Fatal("overlap", sex, months, d.Indicator)
					}
					selected[d.Indicator] = d
					if d.Sex != sex {
						t.Fatal("wrong sex")
					}
				}
			}
			want := 5
			if months >= 24 && months <= 60 && sex == "FEMALE" {
				want = 4
			}
			if months >= 72 && months <= 120 {
				want = 3
			}
			if months > 120 && months <= 228 {
				want = 2
			}
			if months > 228 {
				want = 0
			}
			// 24 WHO months = 730.5 days. Exact integer day 730 is still length.
			if months == 24 {
				want = 5
			}
			if len(selected) != want {
				t.Errorf("%s %d months: %d want %d", sex, months, len(selected), want)
			}
		}
	}
	for _, test := range []struct {
		days           int
		indicator, ref string
	}{
		{730, "LENGTH", "WHO 2006"}, {731, "HEIGHT", "WHO 2006"}, {1856, "WEIGHT", "WHO 2006"}, {1857, "WEIGHT", "WHO 2007"}, {3652, "WEIGHT", "WHO 2007"},
	} {
		found := false
		for _, d := range Catalog() {
			if d.Applicable("MALE", test.days) && d.Indicator == test.indicator {
				found = true
				if d.Reference != test.ref {
					t.Fatal(test)
				}
			}
		}
		if !found {
			t.Fatal(test)
		}
	}
	for _, days := range []int{-1, 3653, 6940} {
		for _, d := range Catalog() {
			if d.Indicator == "WEIGHT" && d.Applicable("MALE", days) {
				t.Fatal("extrapolated age", days)
			}
		}
	}
	for _, d := range Catalog() {
		if d.Applicable("UNKNOWN", 10) || d.Applicable("", 10) {
			t.Fatal("sex assumed")
		}
	}
}

func TestLMSInterpolationExtremesAndDomain(t *testing.T) {
	r := references(t)
	a, _ := r.At("bfa07_girls", 61)
	b, _ := r.At("bfa07_girls", 62)
	p, ok := r.At("bfa07_girls", 61.5)
	if !ok || p.M != (a.M+b.M)/2 {
		t.Fatal("monthly interpolation", p)
	}
	for _, x := range []float64{60, 228.1, math.NaN(), math.Inf(1)} {
		if _, ok := r.At("bfa07_girls", x); ok {
			t.Fatal("extrapolation", x)
		}
	}
	for _, key := range []string{"wfa06_girls", "bfa07_boys", "wfl06_girls", "wfh06_boys"} {
		p := r.tables[key][0]
		for _, sign := range []float64{-1, 1} {
			v3, v2 := p.Value(sign*3), p.Value(sign*2)
			v4 := v3 + (v3 - v2)
			e := p.Evaluate(v4, true)
			if math.Abs(e.Z-sign*4) > 1e-10 || e.Percentile != nil || e.PercentileLimit == "" {
				t.Fatalf("restricted LMS %s: %+v", key, e)
			}
		}
	}
	if (LMS{L: 0, M: 10, S: .1}).Evaluate(10, false).Z != 0 {
		t.Fatal("L=0")
	}
}

func TestSameObservationPositionAndHistoricalAge(t *testing.T) {
	r := references(t)
	w, h := 10., 75.
	id := int64(7)
	o := Observation{Date: "2025-01-01", AgeDays: 365, Weight: &w, Height: &h, Mode: "STANDING", EncounterID: &id}
	d := Catalog()[12]
	p := r.Point(d, o)
	if p == nil || p.X != 75.7 || p.Value != 10 || p.AdjustmentCm != .7 || p.AgeDays != 365 || p.Date != o.Date {
		t.Fatal(p)
	}
	o.Mode = "UNKNOWN"
	if r.Point(d, o) != nil || r.Point(Catalog()[16], o) != nil {
		t.Fatal("position invented")
	}
	o.Mode = "RECUMBENT"
	o.Weight = nil
	if r.Point(d, o) != nil || r.Point(Catalog()[16], o) != nil {
		t.Fatal("missing weight invented")
	}
	charts := r.PatientCharts("FEMALE", 365, []Observation{{Date: "2025-01-01", AgeDays: 365, Weight: &w}, {Date: "2025-02-01", AgeDays: 396, Height: &h, Mode: "RECUMBENT"}})
	for _, c := range charts {
		if c.Definition.Indicator == "BMI" && len(c.Points) != 0 {
			t.Fatal("mixed consultations")
		}
	}
}

func TestInvalidConvertedHeightNeverProducesBMI(t *testing.T) {
	r := references(t)
	w, h := 12., .5
	o := Observation{AgeDays: 731, Weight: &w, Height: &h, Mode: "RECUMBENT"}
	if r.Point(Catalog()[17], o) != nil {
		t.Fatal("negative converted height produced a BMI")
	}
	p := r.Point(Catalog()[5], o)
	if p == nil || p.Value != h || p.Evaluation != nil || p.Note == "" {
		t.Fatal("invalid height was evaluated or original measurement lost", p)
	}
	o.AgeDays = 2000
	h = 100
	if r.Point(Catalog()[19], o) != nil {
		t.Fatal("recumbent school-age height was silently converted")
	}
}
