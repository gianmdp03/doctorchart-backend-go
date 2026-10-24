package clinical

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"

	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func TestDocumentAgeAtIssue(t *testing.T) {
	birth := time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct{ date, expected string }{{"2020-02-29", "0 días"}, {"2020-03-01", "1 días"}, {"2020-03-29", "1 meses"}, {"2022-02-28", "23 meses"}, {"2022-03-01", "2 años"}, {"2022-03-29", "2 años 1m"}} {
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
	at := time.Date(2026, 10, 15, 1, 30, 0, 0, time.UTC)
	doc := printDocument{Source: "PRESCRIPTION_RECORD", ID: 1, Code: "RX-1-1", at: at, IssuanceZone: zone.String(), Issued: api.FormatInstant(at), Patient: printPatient{Name: "Paciente de prueba", Insurance: json.RawMessage("null")}, Medications: json.RawMessage("[]"), Orders: []string{"Laboratorio clínico de demostración"}}
	data, err := service.printPDF(doc, url.Values{"doctorName": {"Profesional de prueba"}})
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
	if path := os.Getenv("DOCTORCHART_PDF_VISUAL_PATH"); path != "" {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
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
