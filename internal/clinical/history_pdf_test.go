package clinical

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
	"time"

	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/syntah/doctorchart-backend-go/internal/growth"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

func TestRenderClinicalHistoryPDFWithChartsAndOptionalLicense(t *testing.T) {
	zone, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := growth.Load()
	if err != nil {
		t.Fatal(err)
	}
	weight, height := 10.2, 78.0

	var logoBuf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	img.Set(10, 10, color.NRGBA{R: 22, G: 91, B: 82, A: 255})
	if err := png.Encode(&logoBuf, img); err != nil {
		t.Fatal(err)
	}

	disease := "Bronquitis aguda"
	complaint := "Tos y fiebre"
	nid := "45123987"
	gender := "MALE"

	data := clinicalHistoryPDFData{
		Patient: Patient{
			ID: 1, FullName: "Mateo Gómez", BirthDate: "2024-10-01",
			NationalID: &nid, Gender: &gender, Values: json.RawMessage(`{"sys_sex":"MALE"}`),
			Insurance: json.RawMessage(`{"providerName":"OSDE","planName":"210","affiliateNumber":"123456"}`),
		},
		Encounter: Encounter{
			EncounterSummary: EncounterSummary{
				ID: 10, Type: "FOLLOW_UP", Complaint: &complaint, Disease: &disease,
				Measurements: Measurements{Weight: &weight, Height: &height},
				Specialty:    workspace.Summary{Name: "Pediatría"},
			},
			instant: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
		},
		AuthorName:    "Dra. Valentina Gómez",
		AuthorLicense: "", // Empty license MUST NOT print "Matrícula" or "No informada"
		Logo:          &workspace.LogoAsset{PNG: logoBuf.Bytes(), Width: 32, Height: 32, SHA256: "0000000000000000000000000000000000000000000000000000000000000001"},
		GeneratedAt:   time.Date(2026, 11, 26, 18, 0, 0, 0, time.UTC),
		Zone:          zone,
	}

	data.Charts, err = encounterGrowthCharts(refs, data.Patient, data.Encounter, zone, "AUTO")
	if err != nil {
		t.Fatal(err)
	}

	out, err := renderClinicalHistoryPDF(data)
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := pdfapi.ReadContext(context.Background(), bytes.NewReader(out), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.EnsurePageCount(); err != nil || pdf.PageCount < 1 {
		t.Fatalf("unexpected page count: %d err=%v", pdf.PageCount, err)
	}
	for _, want := range []string{"Registro de Consulta Clínica", "Mateo Gómez", "Bronquitis aguda", "Dra. Valentina Gómez", "Curvas de crecimiento de la consulta", "31/12/2025 22:00", "14 meses", "Valor en la curva: 10.20 kg"} {
		if !projectionFixturePDFContains(t, out, want) {
			t.Errorf("history PDF missing %q", want)
		}
	}
	for _, absent := range []string{"Matrícula", "No informada", "Perímetro cefálico para la edad", "Antecedentes y alertas clínicas", "2 años y 1 mes"} {
		if projectionFixturePDFContains(t, out, absent) {
			t.Errorf("consultation PDF unexpectedly included %q", absent)
		}
	}

	data.AuthorLicense = "MN 148.920"
	head := 65.0
	data.Encounter.Measurements.Head = &head
	data.Charts, err = encounterGrowthCharts(refs, data.Patient, data.Encounter, zone, "AUTO")
	if err != nil {
		t.Fatal(err)
	}
	withLicense, err := renderClinicalHistoryPDF(data)
	if err != nil {
		t.Fatal(err)
	}
	if !projectionFixturePDFContains(t, withLicense, "Matrícula profesional: MN 148.920") {
		t.Fatal("history PDF missing professional license when configured")
	}
	if !projectionFixturePDFContains(t, withLicense, "Valor en la curva: 65.00 cm") ||
		!projectionFixturePDFContains(t, withLicense, "Fuera del rango del gráfico") {
		t.Fatal("consultation PDF missing the out-of-frame measurement")
	}
}

func TestEncounterGrowthChartsUseOnlySelectedMeasurements(t *testing.T) {
	zone := time.FixedZone("Argentina", -3*3600)
	refs, err := growth.Load()
	if err != nil {
		t.Fatal(err)
	}
	sex := "MALE"
	patient := Patient{
		BirthDate: "2024-10-01", Gender: &sex,
		Values:   json.RawMessage(`{"sys_sex":"MALE"}`),
		Personal: json.RawMessage(`{"birthMeasurements":{"weightKg":3.2,"heightCm":50,"headCircumferenceCm":34}}`),
	}
	weight, height, head := 10.2, 78.0, 46.0
	encounter := Encounter{
		EncounterSummary: EncounterSummary{ID: 10, Values: json.RawMessage(`{"heightMeasurementMode":"RECUMBENT"}`)},
		instant:          time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
	}

	for _, tc := range []struct {
		name         string
		measurements Measurements
		reference    string
		want         map[string]float64
	}{
		{"complete", Measurements{Weight: &weight, Height: &height, Head: &head}, "AUTO", map[string]float64{
			"wfa06_boys": weight, "lfa06_boys": height, "hcfa06_boys": head,
			"wfl06_boys": weight, "bfa06_boys": weight / (.78 * .78),
		}},
		{"height only", Measurements{Height: &height}, "AUTO", map[string]float64{"lfa06_boys": height}},
		{"Down only", Measurements{Weight: &weight, Height: &height}, "DOWN", map[string]float64{
			"DOWN_HEIGHT_0": height, "DOWN_BMI_4": weight / (.78 * .78),
		}},
		{"no measurements", Measurements{}, "AUTO", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encounter.Measurements = tc.measurements
			charts, err := encounterGrowthCharts(refs, patient, encounter, zone, tc.reference)
			if err != nil || len(charts) != len(tc.want) {
				t.Fatalf("got %d charts, want %d: %v", len(charts), len(tc.want), err)
			}
			for _, chart := range charts {
				expected, ok := tc.want[chart.Definition.ID]
				if !ok || len(chart.Points) != 1 {
					t.Fatalf("unexpected chart %s with %d points", chart.Definition.ID, len(chart.Points))
				}
				point := chart.Points[0]
				if point.EncounterID == nil || *point.EncounterID != 10 || point.Date != "2025-12-31" || math.Abs(point.Value-expected) > 1e-9 {
					t.Errorf("%s: unexpected point %+v", chart.Definition.ID, point)
				}
			}
		})
	}
}
