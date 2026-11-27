package clinical

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"

	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func TestDocumentAgeAtIssue(t *testing.T) {
	birth := time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct{ date, expected string }{{"2020-02-29", "0 días"}, {"2020-03-01", "1 día"}, {"2020-03-29", "1 mes"}, {"2022-02-28", "23 meses"}, {"2022-03-01", "2 años"}, {"2022-03-29", "2 años y 1 mes"}, {"2022-04-29", "2 años y 2 meses"}} {
		reference, err := time.Parse("2006-01-02", test.date)
		if err != nil {
			t.Fatal(err)
		}
		age := printAge(birth, reference)
		if age == nil || *age != test.expected {
			t.Fatalf("%s: %v", test.date, age)
		}
	}
	if printAge(birth, birth.AddDate(0, 0, -1)) != nil {
		t.Fatal("future birth printed with an age")
	}
}

func TestClinicalPDFIssuanceDate(t *testing.T) {
	zone, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{zone: zone}
	at := time.Date(2026, 11, 26, 1, 30, 0, 0, time.UTC)
	nationalID, age := "12345678", "13 años y 7 meses"
	specialty, diagnosis, instructions := "Pediatría", "Diagnóstico de ejemplo", "Seguir las indicaciones del profesional tratante."
	doc := printDocument{
		Source: "PRESCRIPTION_RECORD", ID: 1, Code: "DC-0123456789abcdef0123456789abcdef",
		at: at, IssuanceZone: zone.String(), Issued: api.FormatInstant(at), emitted: true,
		Patient:     printPatient{Name: "Sofía Martín", NationalID: &nationalID, Age: &age, Insurance: json.RawMessage(`{"providerName":"Cobertura de ejemplo","planName":"Plan Familiar","affiliateNumber":"0004321"}`)},
		Author:      printAuthor{UserID: "11111111-1111-4111-8111-111111111111", name: "Lucía María Benítez", license: "12345", Specialty: &specialty},
		Medications: json.RawMessage(`[{"medicationName":"Medicamento de ejemplo","dosage":"1 dosis","administrationRoute":"Oral","frequency":"Según indicación médica","durationDays":1,"instructions":"Indicación de ejemplo."}]`),
		Diagnosis:   &diagnosis, Instructions: &instructions,
	}
	data, err := service.printPDF(doc, url.Values{"doctorName": {"Nombre ajeno"}})
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := pdfapi.ReadContext(context.Background(), bytes.NewReader(data), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.EnsurePageCount(); err != nil || pdf.PageCount != 1 {
		t.Fatal("document date broke pagination", err)
	}
	for _, want := range []string{"25/11/2026", "Lucía María Benítez", "Matrícula profesional: 12345", "Medicamentos indicados", "Documento de identidad: 12345678", "13 años y 7 meses", "Cobertura: Cobertura de ejemplo · Plan: Plan Familiar · Número de afiliación 0004321", "Vía de administración", "1 día", "Emitido sin firma digital"} {
		if !projectionFixturePDFContains(t, data, want) {
			t.Errorf("PDF is missing %q", want)
		}
	}
	for _, omitted := range []string{doc.Code, doc.Author.UserID, "Rp/", "Nombre ajeno"} {
		if projectionFixturePDFContains(t, data, omitted) {
			t.Errorf("PDF includes %q", omitted)
		}
	}
	if path := os.Getenv("DOCTORCHART_PDF_VISUAL_PATH"); path != "" {
		sample := doc
		sample.Author.license = ""
		sample.Patient.Insurance = json.RawMessage("null")
		preview, err := service.printPDF(sample, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, preview, 0600); err != nil {
			t.Fatal(err)
		}
	}
	longNotes := strings.Repeat("Texto clínico de ejemplo.\n", 70) + "Fin de las indicaciones"
	doc.Instructions = &longNotes
	longPDF, err := service.printPDF(doc, nil)
	if err != nil || !projectionFixturePDFContains(t, longPDF, "Fin de las indicaciones") || !projectionFixturePDFContains(t, longPDF, "Continuación") {
		t.Fatalf("long instructions did not continue on a new page: %v", err)
	}
}

func TestClinicalPDFOmitsMissingProfileAndShowsUnreportedCoverage(t *testing.T) {
	service := &Service{zone: time.UTC}
	blank := " \t "
	for _, test := range []struct {
		name, insurance, coverage string
	}{
		{"no coverage recorded", `null`, "Cobertura: No informada"},
		{"blank fields", `{"providerName":"   ","planName":"","affiliateNumber":null}`, "Cobertura: No informada"},
		{"plan and affiliate saved without provider", `{"planName":"Plan Familiar","affiliateNumber":"0004321"}`, "Cobertura: No informada · Plan: Plan Familiar · Número de afiliación 0004321"},
		{"only provider", `{"providerName":" Cobertura de ejemplo "}`, "Cobertura: Cobertura de ejemplo"},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := printDocument{
				at:          time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC),
				Patient:     printPatient{Name: "Sofía Martín", NationalID: &blank, Insurance: json.RawMessage(test.insurance)},
				Author:      printAuthor{name: blank, Specialty: &blank, license: blank},
				Medications: json.RawMessage(`[]`),
				Orders:      []string{"Estudio de ejemplo"},
			}
			data, err := service.printPDF(doc, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !projectionFixturePDFContains(t, data, test.coverage) {
				t.Errorf("PDF is missing %q", test.coverage)
			}
			for _, absent := range []string{"Profesional tratante", "Matrícula profesional", "Nombre no informado", "Documento de identidad", "Edad:", "Particular", "Diagnóstico registrado", "Indicaciones para los estudios"} {
				if projectionFixturePDFContains(t, data, absent) {
					t.Errorf("PDF includes %q", absent)
				}
			}
		})
	}
}

func TestClinicalPDFCharacterSafety(t *testing.T) {
	zone, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{zone: zone}
	for _, test := range []struct {
		text   string
		reject bool
	}{
		{"Control Ana María Ø œ; μg Ω Препарат", false},
		{"Estudio 薬", true},
		{"Control 🩺", true},
		{"Control سلام", true},
	} {
		t.Run(test.text, func(t *testing.T) {
			doc := printDocument{Code: "RX-FIXTURE", at: time.Now(), Patient: printPatient{Name: test.text, Insurance: json.RawMessage("null")}, Medications: json.RawMessage("[]"), Orders: []string{"Control clínico"}}
			data, err := service.printPDF(doc, url.Values{})
			if test.reject {
				failure, ok := err.(*api.Error)
				if !ok || failure.Status != 422 || failure.Code != "PDF_UNSUPPORTED_CHARACTER" || len(data) != 0 {
					t.Fatalf("data=%d err=%v", len(data), err)
				}
			} else if err != nil || !bytes.HasPrefix(data, []byte("%PDF")) {
				t.Fatalf("PDF missing: %v", err)
			}
		})
	}
}
func TestClinicalPDFStructureAndPagination(t *testing.T) {
	zone, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{zone: zone}
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	doc := printDocument{Source: "PRESCRIPTION_RECORD", ID: 1, Code: "RX-1-1", at: at, Issued: "2026-10-08T12:00:00Z", Patient: printPatient{Name: "Paciente de prueba", Insurance: json.RawMessage("null")}, Medications: json.RawMessage("[]"), Orders: []string{"Laboratorio clínico"}}
	for _, count := range []int{1, 100} {
		doc.Orders = make([]string, count)
		for index := range doc.Orders {
			doc.Orders[index] = "Estudio médico registrado para el paciente"
		}
		data, err := service.printPDF(doc, url.Values{"type": {"STUDY_ORDERS"}, "doctorName": {"Dra. Prueba"}})
		if err != nil {
			t.Fatal(err)
		}
		pdf, err := pdfapi.ReadContext(context.Background(), bytes.NewReader(data), model.NewDefaultConfiguration())
		if err != nil {
			t.Fatal(err)
		}
		if err := pdf.EnsurePageCount(); err != nil {
			t.Fatal(err)
		}
		if pdf.Encrypt != nil || pdf.PageCount < 1 || count == 100 && pdf.PageCount < 2 {
			t.Fatalf("invalid pagination %d", pdf.PageCount)
		}
	}
}

func TestClinicalStudyOrderPDF(t *testing.T) {
	zone, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{zone: zone}
	age, nationalID, specialty := "13 años y 7 meses", "12345678", "Pediatría"
	diagnosis, instructions := "Diagnóstico de ejemplo", "Indicaciones registradas por el profesional tratante."
	doc := printDocument{
		at: time.Date(2026, 11, 26, 1, 30, 0, 0, time.UTC), IssuanceZone: zone.String(), emitted: true,
		Code:        "DC-0123456789abcdef0123456789abcdef",
		Patient:     printPatient{Name: "Sofía Martín", Age: &age, NationalID: &nationalID, Insurance: json.RawMessage(`null`)},
		Author:      printAuthor{UserID: "11111111-1111-4111-8111-111111111111", name: "Lucía María Benítez", Specialty: &specialty},
		Medications: json.RawMessage(`[{"medicationName":"Medicamento que pertenece a la receta"}]`),
		Orders:      []string{"Electrocardiograma", "  ", "Ecocardiograma Doppler color", "Análisis de laboratorio"},
		Diagnosis:   &diagnosis, Instructions: &instructions,
	}
	selection := url.Values{"type": {"STUDY_ORDERS"}}
	data, err := service.printPDF(doc, selection)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Orden de estudios médicos", "Estudios solicitados", "25/11/2026", "Lucía María Benítez", "Pediatría",
		"Electrocardiograma", "Ecocardiograma Doppler color", "Análisis de laboratorio",
		"Diagnóstico registrado", diagnosis, "Indicaciones para los estudios", instructions, "Cobertura: No informada",
		"Documento de identidad: 12345678", "13 años y 7 meses",
	} {
		if !projectionFixturePDFContains(t, data, want) {
			t.Errorf("study order is missing %q", want)
		}
	}
	for _, omitted := range []string{doc.Code, doc.Author.UserID, "Matrícula profesional", "Medicamentos indicados", "Medicamento que pertenece a la receta", "4."} {
		if projectionFixturePDFContains(t, data, omitted) {
			t.Errorf("study order includes %q", omitted)
		}
	}
	if path := os.Getenv("DOCTORCHART_STUDY_PDF_VISUAL_PATH"); path != "" {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	doc.Orders = []string{strings.Repeat("Descripción del estudio para probar líneas largas.\n", 55) + "Fin del estudio extenso", "Último estudio"}
	longPDF, err := service.printPDF(doc, selection)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Continuación", "Fin del estudio extenso", "Último estudio"} {
		if !projectionFixturePDFContains(t, longPDF, want) {
			t.Errorf("paginated study order is missing %q", want)
		}
	}
}

func TestClinicalPDFWithHandwrittenSignatureAndStamp(t *testing.T) {
	service := &Service{zone: time.UTC}
	doc := printDocument{
		Source:       "PRESCRIPTION_RECORD",
		ID:           7,
		Code:         "DC-0123456789abcdef0123456789abcdef",
		at:           time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC),
		IssuanceZone: "UTC",
		emitted:      true,
		Patient:      printPatient{Name: "Sofía Martín", Insurance: json.RawMessage("null")},
		Author:       printAuthor{UserID: "doc-sig-1", name: "Dra. Paula Gómez"},
		Medications:  json.RawMessage(`[]`),
		Orders:       []string{"Laboratorio clínico"},
		signature:    testSignatureAsset(1, "Julieta", "Dra. Julieta Pérez — MN 54321", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
	}
	data, err := service.printPDF(doc, url.Values{"type": {"STUDY_ORDERS"}})
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := pdfapi.ReadContext(context.Background(), bytes.NewReader(data), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.EnsurePageCount(); err != nil || pdf.PageCount != 1 {
		t.Fatalf("unexpected page count: %d err=%v", pdf.PageCount, err)
	}
	for _, want := range []string{"Firma y sello del profesional", "Dra. Julieta Pérez — MN 54321"} {
		if !projectionFixturePDFContains(t, data, want) {
			t.Errorf("signed PDF missing %q", want)
		}
	}
	for _, omitted := range []string{"Emitido sin firma digital", "Vista previa sin firma digital"} {
		if projectionFixturePDFContains(t, data, omitted) {
			t.Errorf("signed PDF should not contain %q", omitted)
		}
	}

	// Without stampText, only "Firma y sello del profesional" appears.
	doc.signature.StampText = ""
	noStamp, err := service.printPDF(doc, url.Values{"type": {"STUDY_ORDERS"}})
	if err != nil {
		t.Fatal(err)
	}
	if !projectionFixturePDFContains(t, noStamp, "Firma y sello del profesional") ||
		projectionFixturePDFContains(t, noStamp, "Emitido sin firma digital") {
		t.Fatal("signed PDF without stampText did not render expected footer")
	}

	// Unsupported characters in stampText are rejected cleanly with 422.
	doc.signature.StampText = "Dra. Julieta 🩺"
	_, err = service.printPDF(doc, url.Values{"type": {"STUDY_ORDERS"}})
	failure, ok := err.(*api.Error)
	if !ok || failure.Status != 422 || failure.Code != "PDF_UNSUPPORTED_CHARACTER" {
		t.Fatalf("expected 422 PDF_UNSUPPORTED_CHARACTER, got %v", err)
	}
}
