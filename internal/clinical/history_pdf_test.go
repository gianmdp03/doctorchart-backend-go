package clinical

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
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
	w1, h1 := 8.5, 70.0
	w2, h2 := 10.2, 78.0
	obs := []growth.Observation{
		{Source: "ENCOUNTER", Date: "2025-06-01", AgeDays: 240, AgeFormatted: "8 meses", Weight: &w1, Height: &h1, Mode: "RECUMBENT"},
		{Source: "ENCOUNTER", Date: "2026-01-01", AgeDays: 454, AgeFormatted: "15 meses", Weight: &w2, Height: &h2, Mode: "RECUMBENT"},
	}
	charts := refs.PatientCharts("MALE", 454, obs)

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
			NationalID: &nid, Gender: &gender,
			Insurance: json.RawMessage(`{"providerName":"OSDE","planName":"210","affiliateNumber":"123456"}`),
		},
		Encounters: []Encounter{{
			EncounterSummary: EncounterSummary{
				ID: 10, Type: "FOLLOW_UP", Complaint: &complaint, Disease: &disease,
				Measurements: Measurements{Weight: &w2, Height: &h2},
			},
			instant: time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC),
		}},
		Growth: &patientGrowth{
			PatientID: 1, BirthDate: "2024-10-01", ReferenceSex: "MALE",
			Observations: obs, Charts: charts,
		},
		SpecialtyName: "Pediatría",
		AuthorName:    "Dra. Valentina Gómez",
		AuthorLicense: "", // Empty license MUST NOT print "Matrícula" or "No informada"
		Logo:          &workspace.LogoAsset{PNG: logoBuf.Bytes(), Width: 32, Height: 32, SHA256: "0000000000000000000000000000000000000000000000000000000000000001"},
		GrowthRefMode: "BOTH",
		GeneratedAt:   time.Date(2026, 11, 26, 18, 0, 0, 0, time.UTC),
		Zone:          zone,
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
	for _, want := range []string{"Historia Clínica", "Mateo Gómez", "Bronquitis aguda", "Dra. Valentina Gómez", "Antropometría y curvas de crecimiento"} {
		if !projectionFixturePDFContains(t, out, want) {
			t.Errorf("history PDF missing %q", want)
		}
	}
	for _, absent := range []string{"Matrícula", "No informada"} {
		if projectionFixturePDFContains(t, out, absent) {
			t.Errorf("history PDF unexpectedly included %q when license was empty", absent)
		}
	}

	data.AuthorLicense = "MN 148.920"
	withLicense, err := renderClinicalHistoryPDF(data)
	if err != nil {
		t.Fatal(err)
	}
	if !projectionFixturePDFContains(t, withLicense, "Matrícula profesional: MN 148.920") {
		t.Fatal("history PDF missing professional license when configured")
	}
}
