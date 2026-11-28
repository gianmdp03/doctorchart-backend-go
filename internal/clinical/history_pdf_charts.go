package clinical

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/syntah/doctorchart-backend-go/internal/growth"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

//go:embed data/myrelid-2002.json
var downMyrelidJSON []byte

const growthChartHeight = 222.0

type downSeriesPoint struct {
	X      float64   `json:"x"`
	Values []float64 `json:"values"`
}

func encounterGrowthCharts(refs *growth.References, patient Patient, enc Encounter, zone *time.Location, reference string) ([]growth.PatientChart, error) {
	m := enc.Measurements
	if m.Weight == nil && m.Height == nil && m.Head == nil {
		return nil, nil
	}
	birth, err := time.Parse("2006-01-02", patient.BirthDate)
	if err != nil {
		return nil, err
	}
	local := enc.instant.In(zone)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	if day.Before(birth) {
		return nil, nil
	}
	sex, _, err := growthReferenceSex(patient, "")
	if err != nil || sex == "" {
		return nil, err
	}
	values, err := wizard.Decode(enc.Values)
	if err != nil {
		return nil, err
	}
	mode, _ := values["heightMeasurementMode"].(string)
	days := int(day.Sub(birth).Hours() / 24)
	observations := []growth.Observation{{
		Source: "ENCOUNTER", EncounterID: &enc.ID, Date: day.Format("2006-01-02"),
		AgeDays: days, AgeFormatted: pediatricAge(birth, day),
		Weight: m.Weight, Height: m.Height, Head: m.Head,
		Mode: growth.ResolveMeasurementMode(mode, days),
	}}

	var charts []growth.PatientChart
	if reference != "DOWN" {
		for _, chart := range refs.PatientCharts(sex, days, observations) {
			if len(chart.Points) > 0 {
				charts = append(charts, chart)
			}
		}
	}
	includeDown := reference == "DOWN" || reference == "BOTH"
	if reference == "AUTO" && enc.Diagnosis != nil {
		diagnosis := strings.ToLower(*enc.Diagnosis)
		includeDown = strings.Contains(diagnosis, "síndrome de down") || strings.Contains(diagnosis, "trisomía 21")
	}
	if includeDown {
		charts = append(charts, downChartsForPatient(patient.BirthDate, sex, observations)...)
	}
	return charts, nil
}

func downChartsForPatient(birthDate, sex string, observations []growth.Observation) []growth.PatientChart {
	if (sex != "MALE" && sex != "FEMALE") || len(observations) == 0 {
		return nil
	}
	var tables [][]downSeriesPoint
	if err := json.Unmarshal(downMyrelidJSON, &tables); err != nil || len(tables) < 6 {
		return nil
	}
	birth, err := time.Parse("2006-01-02", birthDate)
	if err != nil {
		return nil
	}
	type spec struct {
		idx         int
		sex         string
		indicator   string
		title       string
		yLabel      string
		yUnit       string
		startMonths float64
		endMonths   float64
	}
	specs := []spec{
		{0, "MALE", "HEIGHT", "Síndrome de Down · Talla para la edad (0–36 m)", "Talla", "cm", 0, 36},
		{1, "MALE", "HEIGHT", "Síndrome de Down · Talla para la edad (3–18 años)", "Talla", "cm", 36, 216},
		{2, "FEMALE", "HEIGHT", "Síndrome de Down · Talla para la edad (0–36 m)", "Talla", "cm", 0, 36},
		{3, "FEMALE", "HEIGHT", "Síndrome de Down · Talla para la edad (3–18 años)", "Talla", "cm", 36, 216},
		{4, "MALE", "BMI", "Síndrome de Down · IMC para la edad (0–18 años)", "IMC", "kg/m²", 0, 216},
		{5, "FEMALE", "BMI", "Síndrome de Down · IMC para la edad (0–18 años)", "IMC", "kg/m²", 0, 216},
	}

	var charts []growth.PatientChart
	for _, sp := range specs {
		if sp.sex != sex {
			continue
		}
		table := tables[sp.idx]
		if len(table) == 0 {
			continue
		}
		var pts []growth.Point
		minY, maxY := math.Inf(1), math.Inf(-1)
		curves := make([]growth.CurvePoint, len(table))
		for i, row := range table {
			// Select -2, -1, 0, +1, +2 SD (indices 1..5 of the 7-column Myrelid series)
			vals := make([]float64, 0, 5)
			for idx := 1; idx <= 5 && idx < len(row.Values); idx++ {
				v := row.Values[idx]
				vals = append(vals, v)
				if v < minY {
					minY = v
				}
				if v > maxY {
					maxY = v
				}
			}
			curves[i] = growth.CurvePoint{X: row.X, Values: vals}
		}
		for _, obs := range observations {
			if obs.Height == nil || *obs.Height <= 0 {
				continue
			}
			d, err := time.Parse("2006-01-02", obs.Date)
			if err != nil {
				continue
			}
			months := float64(d.Sub(birth).Hours()/24.0) / 30.4375
			if months < sp.startMonths || months > sp.endMonths {
				continue
			}
			var val float64
			if sp.indicator == "HEIGHT" {
				val = *obs.Height
			} else {
				if obs.Weight == nil || *obs.Weight <= 0 {
					continue
				}
				val = *obs.Weight / math.Pow(*obs.Height/100.0, 2)
			}
			if !math.IsNaN(val) && !math.IsInf(val, 0) && val > 0 {
				if val < minY {
					minY = val
				}
				if val > maxY {
					maxY = val
				}
				pts = append(pts, growth.Point{Observation: obs, X: months, Value: val})
			}
		}
		if len(pts) == 0 {
			continue
		}
		stepY := 10.0
		if sp.indicator == "BMI" {
			stepY = 2.0
		}
		minY = math.Floor(minY/stepY) * stepY
		maxY = math.Ceil(maxY/stepY) * stepY
		if maxY <= minY {
			maxY = minY + stepY
		}
		stepX := 6.0
		if sp.endMonths > 36 {
			stepX = 24.0
		}
		charts = append(charts, growth.PatientChart{
			Applicable: true,
			Points:     pts,
			ReferenceChart: growth.ReferenceChart{
				Definition: growth.Definition{
					ID:          fmt.Sprintf("DOWN_%s_%d", sp.indicator, sp.idx),
					Title:       sp.title,
					Sex:         sex,
					Reference:   "Myrelid 2002 (Síndrome de Down)",
					Percentiles: []int{-2, -1, 0, 1, 2},
					X:           growth.Axis{Variable: "Edad", Unit: "meses", Min: sp.startMonths, Max: sp.endMonths, Step: stepX},
					Y:           growth.Axis{Variable: sp.yLabel, Unit: sp.yUnit, Min: minY, Max: maxY, Step: stepY},
				},
				Curves: curves,
			},
		})
	}
	return charts
}

func renderVectorGrowthChart(pdf *fpdf.Fpdf, translate func(string) string, chart growth.PatientChart, left, width float64) {
	const cardHeight = growthChartHeight
	top := pdf.GetY()
	// Outer paper card
	pdf.SetDrawColor(213, 222, 217)
	pdf.SetFillColor(255, 255, 255)
	pdf.SetLineWidth(0.6)
	pdf.RoundedRect(left, top, width, cardHeight, 4, "1234", "FD")

	// Title & Reference
	pdf.SetFont("Clinical", "B", 9.5)
	pdf.SetTextColor(28, 43, 42)
	pdf.SetXY(left+10, top+8)
	pdf.CellFormat(width-170, 13, translate(chart.Definition.Title), "", 0, "L", false, 0, "")

	pdf.SetFont("Clinical", "", 7.5)
	pdf.SetTextColor(74, 93, 91)
	pdf.SetXY(left+width-165, top+8)
	pdf.CellFormat(155, 13, translate(strings.Replace(chart.Definition.Reference, "WHO ", "OMS ", 1)), "", 0, "R", false, 0, "")

	plotX := left + 42
	plotY := top + 28
	plotW := width - 78
	plotH := cardHeight - 70

	xMin, xMax := chart.Definition.X.Min, chart.Definition.X.Max
	yMin, yMax := chart.Definition.Y.Min, chart.Definition.Y.Max
	if xMax <= xMin {
		xMax = xMin + 1
	}
	if yMax <= yMin {
		yMax = yMin + 1
	}

	toX := func(v float64) float64 {
		clamped := math.Max(xMin, math.Min(xMax, v))
		return plotX + ((clamped-xMin)/(xMax-xMin))*plotW
	}
	toY := func(v float64) float64 {
		clamped := math.Max(yMin, math.Min(yMax, v))
		return plotY + plotH - ((clamped-yMin)/(yMax-yMin))*plotH
	}

	// Gridlines (Y)
	stepY := chart.Definition.Y.Step
	if stepY <= 0 {
		stepY = (yMax - yMin) / 5
	}
	pdf.SetFont("Clinical", "", 6.8)
	for yv := yMin; yv <= yMax+stepY*0.001; yv += stepY {
		py := toY(yv)
		pdf.SetDrawColor(232, 238, 236)
		pdf.SetLineWidth(0.4)
		pdf.Line(plotX, py, plotX+plotW, py)
		pdf.SetTextColor(74, 93, 91)
		pdf.SetXY(left+6, py-5)
		pdf.CellFormat(32, 10, strings.TrimSuffix(fmt.Sprintf("%.1f", yv), ".0"), "", 0, "R", false, 0, "")
	}

	// Gridlines (X)
	stepX := chart.Definition.X.Step
	if stepX <= 0 {
		stepX = (xMax - xMin) / 6
	}
	for xv := xMin; xv <= xMax+stepX*0.001; xv += stepX {
		px := toX(xv)
		pdf.SetDrawColor(232, 238, 236)
		pdf.SetLineWidth(0.4)
		pdf.Line(px, plotY, px, plotY+plotH)
		pdf.SetTextColor(74, 93, 91)
		pdf.SetXY(px-14, plotY+plotH+3)
		pdf.CellFormat(28, 9, fmt.Sprintf("%.0f", xv), "", 0, "C", false, 0, "")
	}

	// Plot frame
	pdf.SetDrawColor(168, 184, 179)
	pdf.SetLineWidth(0.6)
	pdf.Rect(plotX, plotY, plotW, plotH, "D")

	// Reference curves
	if len(chart.Curves) > 1 {
		curveCount := len(chart.Curves[0].Values)
		isDown := strings.HasPrefix(chart.Definition.ID, "DOWN_")
		splitBMI := chart.Definition.ID == "bfa06_boys"
		for ci := 0; ci < curveCount; ci++ {
			color := [3]int{217, 119, 6} // amber
			lineW := 0.65
			if ci == 0 || ci == curveCount-1 {
				color = [3]int{220, 38, 38} // red outer
			} else if ci == curveCount/2 {
				color = [3]int{22, 163, 74} // green median
				lineW = 0.95
			}
			pdf.SetDrawColor(color[0], color[1], color[2])
			pdf.SetLineWidth(lineW)
			for i := 1; i < len(chart.Curves); i++ {
				if ci >= len(chart.Curves[i-1].Values) || ci >= len(chart.Curves[i].Values) {
					continue
				}
				if splitBMI && chart.Curves[i-1].X < 24 && chart.Curves[i].X >= 24 {
					continue
				}
				x1, y1 := toX(chart.Curves[i-1].X), toY(chart.Curves[i-1].Values[ci])
				x2, y2 := toX(chart.Curves[i].X), toY(chart.Curves[i].Values[ci])
				pdf.Line(x1, y1, x2, y2)
			}
			// Right-side curve label
			last := chart.Curves[len(chart.Curves)-1]
			if ci < len(last.Values) {
				label := ""
				if ci < len(chart.Definition.Percentiles) {
					p := chart.Definition.Percentiles[ci]
					if isDown {
						if p > 0 {
							label = fmt.Sprintf("+%d DE", p)
						} else {
							label = fmt.Sprintf("%d DE", p)
						}
					} else {
						label = fmt.Sprintf("P%d", p)
					}
				}
				if label != "" {
					pdf.SetFont("Clinical", "", 6.2)
					pdf.SetTextColor(color[0], color[1], color[2])
					pdf.SetXY(plotX+plotW+3, toY(last.Values[ci])-4.5)
					pdf.CellFormat(28, 9, translate(label), "", 0, "L", false, 0, "")
				}
			}
		}
	}

	// The chart contains a single observation from the printed encounter.
	pt := chart.Points[0]
	inFrame := pt.X >= xMin && pt.X <= xMax && pt.Value >= yMin && pt.Value <= yMax
	if inFrame {
		cx, cy := toX(pt.X), toY(pt.Value)
		pdf.SetFillColor(255, 255, 255)
		pdf.SetDrawColor(29, 78, 216)
		pdf.SetLineWidth(1.2)
		pdf.Circle(cx, cy, 3.0, "FD")
		pdf.SetFillColor(29, 78, 216)
		pdf.Circle(cx, cy, 1.5, "F")
	}

	// Axis captions
	axisNames := map[string]string{
		"AGE": "Edad", "WEIGHT": "Peso", "LENGTH": "Longitud", "HEIGHT": "Talla",
		"HEAD_CIRCUMFERENCE": "Perímetro cefálico", "BMI": "IMC",
	}
	axisCaption := func(axis growth.Axis) string {
		name := axisNames[axis.Variable]
		if name == "" {
			name = axis.Variable
		}
		return name + " (" + axis.Unit + ")"
	}
	pdf.SetFont("Clinical", "", 7.2)
	pdf.SetTextColor(74, 93, 91)
	pdf.SetXY(plotX, top+cardHeight-30)
	pdf.CellFormat(plotW, 10, translate(axisCaption(chart.Definition.X)), "", 0, "C", false, 0, "")
	pdf.SetXY(left+8, top+cardHeight-30)
	pdf.CellFormat(110, 10, translate(axisCaption(chart.Definition.Y)), "", 0, "L", false, 0, "")

	value := fmt.Sprintf("Valor en la curva: %.2f %s · %s", pt.Value, chart.Definition.Y.Unit, pt.AgeFormatted)
	if !inFrame {
		value += " · Fuera del rango del gráfico"
	}
	pdf.SetXY(left+8, top+cardHeight-16)
	pdf.CellFormat(width-16, 10, translate(value), "", 0, "L", false, 0, "")

	pdf.SetY(top + cardHeight + 10)
}
