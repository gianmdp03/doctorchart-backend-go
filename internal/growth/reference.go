package growth

import (
	"crypto/sha256"
	"embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Only the validated normalized tables and provenance are loaded at runtime.
// Original WHO workbooks remain in data/originals for reproducible offline import.
//
//go:embed data/*.csv data/manifest.json
var files embed.FS

type LMS struct{ X, L, M, S float64 }
type References struct {
	tables     map[string][]LMS
	provenance map[string]json.RawMessage
}
type Evaluation struct {
	Z               float64  `json:"zScore"`
	Percentile      *float64 `json:"percentile"`
	PercentileLimit string   `json:"percentileLimit,omitempty"`
}
type CurvePoint struct {
	X      float64   `json:"x"`
	Values []float64 `json:"values"`
}
type ReferenceChart struct {
	Definition Definition      `json:"definition"`
	Curves     []CurvePoint    `json:"curves"`
	Provenance json.RawMessage `json:"provenance"`
}

func Load() (*References, error) {
	r := &References{tables: map[string][]LMS{}, provenance: map[string]json.RawMessage{}}
	manifest, err := files.ReadFile("data/manifest.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(manifest, &r.provenance); err != nil {
		return nil, err
	}
	for key := range r.provenance {
		payload, err := files.ReadFile("data/" + key + ".csv")
		if err != nil {
			return nil, err
		}
		var meta struct {
			NormalizedSHA256 string `json:"normalizedSha256"`
		}
		if err := json.Unmarshal(r.provenance[key], &meta); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(payload)
		if meta.NormalizedSHA256 != hex.EncodeToString(sum[:]) {
			return nil, fmt.Errorf("WHO %s: normalized checksum mismatch", key)
		}
		rows, err := csv.NewReader(strings.NewReader(string(payload))).ReadAll()
		if err != nil {
			return nil, err
		}
		if len(rows) < 2 || strings.Join(rows[0], ",") != "x,l,m,s" {
			return nil, fmt.Errorf("WHO %s: invalid header", key)
		}
		entries := []LMS{}
		for _, row := range rows[1:] {
			if len(row) != 4 {
				return nil, fmt.Errorf("WHO %s: invalid row", key)
			}
			v := [4]float64{}
			for i := range v {
				v[i], err = strconv.ParseFloat(row[i], 64)
				if err != nil || !finite(v[i]) {
					return nil, fmt.Errorf("WHO %s: invalid numeric parameter", key)
				}
			}
			if v[2] <= 0 || v[3] <= 0 || (len(entries) > 0 && v[0] <= entries[len(entries)-1].X) {
				return nil, fmt.Errorf("WHO %s: invalid LMS/order", key)
			}
			entries = append(entries, LMS{v[0], v[1], v[2], v[3]})
		}
		r.tables[key] = entries
	}
	if len(r.tables) != 18 {
		return nil, fmt.Errorf("WHO: expected 18 source tables")
	}
	for _, d := range Catalog() {
		if len(r.tables[d.Dataset]) == 0 {
			return nil, fmt.Errorf("WHO: missing %s", d.Dataset)
		}
	}
	return r, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// At uses exact daily rows for WHO 2006 and linear LMS interpolation between
// published monthly rows for WHO 2007 / tenths of cm for WFL/H. No extrapolation.
func (r *References) At(dataset string, x float64) (LMS, bool) {
	t := r.tables[dataset]
	if !finite(x) || len(t) == 0 || x < t[0].X || x > t[len(t)-1].X {
		return LMS{}, false
	}
	i := sort.Search(len(t), func(i int) bool { return t[i].X >= x })
	if t[i].X == x {
		return t[i], true
	}
	if i == 0 {
		return LMS{}, false
	}
	a, b := t[i-1], t[i]
	f := (x - a.X) / (b.X - a.X)
	return LMS{x, a.L + f*(b.L-a.L), a.M + f*(b.M-a.M), a.S + f*(b.S-a.S)}, true
}

func (p LMS) Value(z float64) float64 {
	if p.L == 0 {
		return p.M * math.Exp(p.S*z)
	}
	return p.M * math.Pow(1+p.L*p.S*z, 1/p.L)
}

func (p LMS) Evaluate(value float64, weightBased bool) *Evaluation {
	if value <= 0 || !finite(value) {
		return nil
	}
	z := math.Log(value/p.M) / p.S
	if p.L != 0 {
		z = math.Expm1(p.L*math.Log(value/p.M)) / (p.L * p.S)
	}
	if weightBased {
		lower, upper := p.Value(-3), p.Value(3)
		if value < lower {
			z = -3 + (value-lower)/(p.Value(-2)-lower)
		}
		if value > upper {
			z = 3 + (value-upper)/(upper-p.Value(2))
		}
	}
	if !finite(z) {
		return nil
	}
	result := &Evaluation{Z: z}
	// WHO does not give a numerical centile outside ±3 SD.
	if z < -3 {
		result.PercentileLimit = "<0.135"
	} else if z > 3 {
		result.PercentileLimit = ">99.865"
	} else {
		percentile := 50 * math.Erfc(-z/math.Sqrt2)
		result.Percentile = &percentile
	}
	return result
}

var centileZ = []float64{-1.8807936081512509, -1.0364333894937898, 0, 1.0364333894937898, 1.8807936081512509}

func (r *References) Chart(d Definition) ReferenceChart {
	chart := ReferenceChart{Definition: d, Curves: []CurvePoint{}, Provenance: r.provenance[d.Dataset]}
	for _, p := range r.tables[d.Dataset] {
		x := p.X
		if d.Reference == "WHO 2006" && d.X.Variable == "AGE" {
			x /= DaysPerMonth
		}
		// Keep the WHO 2006 tail through day 1856 available for historical points.
		if d.X.Variable == "AGE" {
			if d.AgeMinMonths == 24 && p.X < 731 {
				continue
			}
			if d.AgeMaxMonths == 24 && p.X > 730 {
				continue
			}
		}
		values := make([]float64, 5)
		for i, z := range centileZ {
			values[i] = p.Value(z)
		}
		chart.Curves = append(chart.Curves, CurvePoint{x, values})
	}
	return chart
}
