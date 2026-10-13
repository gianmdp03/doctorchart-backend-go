package growth

import "math"

type Observation struct {
	Source       string   `json:"source"`
	EncounterID  *int64   `json:"encounterId"`
	Date         string   `json:"date"`
	AgeDays      int      `json:"ageDays"`
	AgeFormatted string   `json:"ageFormatted"`
	Weight       *float64 `json:"weightKg"`
	Height       *float64 `json:"heightCm"`
	Head         *float64 `json:"headCircumferenceCm"`
	Mode         string   `json:"measurementMode"`
}

type Point struct {
	Observation
	X            float64     `json:"x"`
	Value        float64     `json:"value"`
	Evaluation   *Evaluation `json:"evaluation"`
	AdjustmentCm float64     `json:"adjustmentCm,omitempty"`
	Note         string      `json:"note,omitempty"`
}

type PatientChart struct {
	ReferenceChart
	Applicable bool    `json:"applicable"`
	Points     []Point `json:"points"`
	Omitted    int     `json:"omitted"`
}

func positive(v *float64) bool { return v != nil && finite(*v) && *v > 0 }

// ResolveMeasurementMode preserves documented positions. Missing positions use
// DoctorChart's standard measuring workflow: recumbent infants, then standing.
// This is a calculation default; the original clinical record is never rewritten.
func ResolveMeasurementMode(mode string, ageDays int) string {
	if mode == "RECUMBENT" || mode == "STANDING" {
		return mode
	}
	if ageDays < 0 {
		return "UNKNOWN"
	}
	if ageDays <= 730 {
		return "RECUMBENT"
	}
	return "STANDING"
}

// Convert only when a documented position differs from the reference position.
func normalizedHeight(o Observation) (float64, float64, bool) {
	if !positive(o.Height) || (o.Mode != "RECUMBENT" && o.Mode != "STANDING") {
		return 0, 0, false
	}
	adjustment := 0.0
	if o.AgeDays <= 730 && o.Mode == "STANDING" {
		adjustment = .7
	}
	if o.AgeDays >= 731 && o.Mode == "RECUMBENT" {
		// The 0.7 cm conversion is documented for the WHO child standards.
		// Recumbent measurements in schoolchildren are not converted silently.
		if o.AgeDays > 1856 {
			return 0, 0, false
		}
		adjustment = -.7
	}
	height := *o.Height + adjustment
	return height, adjustment, finite(height) && height > 0
}

func (r *References) Point(d Definition, o Observation) *Point {
	if !d.Applicable(d.Sex, o.AgeDays) {
		return nil
	}
	o.Mode = ResolveMeasurementMode(o.Mode, o.AgeDays)
	x := float64(o.AgeDays) / DaysPerMonth
	coordinate := float64(o.AgeDays)
	if d.Reference == "WHO 2007" {
		coordinate = x
	}
	value := 0.0
	note := ""
	adjustment := 0.0
	switch d.Indicator {
	case "WEIGHT":
		if !positive(o.Weight) {
			return nil
		}
		value = *o.Weight
	case "HEAD_CIRCUMFERENCE":
		if !positive(o.Head) {
			return nil
		}
		value = *o.Head
	default:
		height, delta, ok := normalizedHeight(o)
		adjustment = delta
		if !ok {
			if (d.Indicator == "LENGTH" || d.Indicator == "HEIGHT") && positive(o.Height) {
				note := "Posición de medición sin confirmar: valor registrado, sin evaluación OMS."
				if o.Mode == "RECUMBENT" || o.Mode == "STANDING" {
					note = "La medida o su posición no permite la conversión OMS para esta edad; se conserva el valor registrado sin evaluación."
				}
				return &Point{Observation: o, X: x, Value: *o.Height, Note: note}
			}
			return nil
		}
		switch d.Indicator {
		case "LENGTH", "HEIGHT":
			value = height
		case "BMI":
			if !positive(o.Weight) {
				return nil
			}
			value = *o.Weight / math.Pow(height/100, 2)
		case "WEIGHT_LENGTH", "WEIGHT_HEIGHT":
			if !positive(o.Weight) {
				return nil
			}
			value = *o.Weight
			x = height
			coordinate = height
		}
		if delta != 0 {
			note = "Conversión OMS de posición de medición; se conserva el valor original."
		}
	}
	if !finite(value) || value <= 0 {
		return nil
	}
	p, ok := r.At(d.Dataset, coordinate)
	point := &Point{Observation: o, X: x, Value: value, AdjustmentCm: adjustment, Note: note}
	if !ok {
		point.Note = "Medición fuera del dominio de la tabla OMS; sin extrapolación."
		return point
	}
	point.Evaluation = p.Evaluate(value, d.Indicator == "WEIGHT" || d.Indicator == "BMI" || d.Indicator == "WEIGHT_LENGTH" || d.Indicator == "WEIGHT_HEIGHT")
	return point
}

func (r *References) PatientCharts(sex string, days int, observations []Observation) []PatientChart {
	charts := []PatientChart{}
	for _, d := range Catalog() {
		if d.Sex != sex {
			continue
		}
		chart := PatientChart{ReferenceChart: r.Chart(d), Applicable: d.Applicable(sex, days), Points: []Point{}}
		for _, o := range observations {
			if !d.Applicable(sex, o.AgeDays) {
				continue
			}
			if p := r.Point(d, o); p != nil {
				chart.Points = append(chart.Points, *p)
			} else {
				chart.Omitted++
			}
		}
		charts = append(charts, chart)
	}
	return charts
}
