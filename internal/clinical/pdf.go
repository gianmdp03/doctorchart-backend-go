package clinical

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/go-pdf/fpdf"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"golang.org/x/image/font/sfnt"
)

// Embedded fonts keep clinical letters/symbols independent of host-installed fonts.
//
//go:embed fonts/*.ttf fonts/LICENSE.txt
var clinicalFonts embed.FS

func (s *Service) printPDF(doc printDocument, metadata url.Values) ([]byte, error) {
	var medications []map[string]any
	if err := json.Unmarshal(doc.Medications, &medications); err != nil {
		return nil, err
	}
	kind := metadata.Get("type")
	switch {
	case strings.EqualFold(kind, "STUDY_ORDERS") && len(doc.Orders) > 0:
		kind = "STUDY_ORDERS"
	case strings.EqualFold(kind, "PRESCRIPTION") && len(medications) > 0:
		kind = "PRESCRIPTION"
	case len(medications) > 0:
		kind = "PRESCRIPTION"
	case len(doc.Orders) > 0:
		kind = "STUDY_ORDERS"
	default:
		kind = "PRESCRIPTION"
	}
	pdf := fpdf.NewCustom(&fpdf.InitType{OrientationStr: "P", UnitStr: "pt", Size: fpdf.SizeType{Wd: 595, Ht: 842}})
	pdf.SetMargins(42, 34, 42)
	pdf.SetCellMargin(0)
	pdf.SetAutoPageBreak(true, 42)

	regular, err := clinicalFonts.ReadFile("fonts/DejaVuSansCondensed.ttf")
	if err != nil {
		return nil, err
	}
	glyphFont, err := sfnt.Parse(regular)
	if err != nil {
		return nil, err
	}
	for style, file := range map[string]string{"": "DejaVuSansCondensed.ttf", "B": "DejaVuSansCondensed-Bold.ttf"} {
		data, err := clinicalFonts.ReadFile("fonts/" + file)
		if err != nil {
			return nil, err
		}
		pdf.AddUTF8FontFromBytes("Clinical", style, data)
	}
	var contentErr error
	var glyphBuffer sfnt.Buffer
	translate := func(value string) string {
		for _, r := range value {
			if r == '\n' || r == '\r' || r == '\t' {
				continue
			}
			glyph, err := glyphFont.GlyphIndex(&glyphBuffer, r)
			// fpdf has no complex-script shaping. Reject unsupported clinical text
			// rather than emitting substituted, reordered or invisible characters.
			complexLetter := unicode.IsLetter(r) && !unicode.In(r, unicode.Latin, unicode.Greek, unicode.Cyrillic)
			if err != nil || glyph == 0 || r > 0xffff || complexLetter {
				contentErr = api.Failure(422, "PDF_UNSUPPORTED_CHARACTER", "El documento contiene caracteres que no se pueden representar de forma segura en el PDF clínico.")
				pdf.SetError(contentErr)
				return ""
			}
		}
		return value
	}
	has := func(value string) bool { return strings.TrimSpace(value) != "" }
	pointer := func(value *string) string {
		if value == nil {
			return ""
		}
		return *value
	}

	const left, right, width, bottom = 42.0, 553.0, 511.0, 800.0
	ink := [3]int{28, 43, 42}
	muted := [3]int{74, 93, 91}
	accent := [3]int{22, 91, 82}
	border := [3]int{213, 222, 217}
	font := func(style string, size float64, color [3]int) {
		pdf.SetFont("Clinical", style, size)
		pdf.SetTextColor(color[0], color[1], color[2])
	}
	text := func(x, w, leading float64, value, style string, size float64, color [3]int, align string) {
		font(style, size, color)
		pdf.SetX(x)
		pdf.MultiCell(w, leading, translate(value), "", align, false)
	}
	rule := func(x1, x2, y float64) {
		pdf.SetDrawColor(border[0], border[1], border[2])
		pdf.SetLineWidth(.75)
		pdf.Line(x1, y, x2, y)
	}
	room := func(height float64) {
		if pdf.GetY()+height > bottom {
			pdf.AddPage()
		}
	}
	field := func(x, w float64, label, value, style string, size float64) {
		text(x, w, 13, label, "B", 8.5, muted, "L")
		text(x, w, size+5, value, style, size, ink, "L")
	}
	title, section := "Receta médica", "Medicamentos indicados"
	if kind == "STUDY_ORDERS" {
		title, section = "Orden de estudios médicos", "Estudios solicitados"
	}
	pdf.SetTitle(title+" | DoctorChart", true)
	pdf.SetHeaderFunc(func() {
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.Rect(left, 34, 23, 3, "F")
		pdf.SetY(44)
		text(left, width, 16, "DoctorChart", "B", 11, accent, "L")
		if pdf.PageNo() > 1 {
			pdf.SetY(44)
			text(left+150, width-150, 16, title+" · Continuación", "", 9, muted, "R")
			rule(left, right, 67)
			pdf.SetY(83)
		}
	})
	pdf.AddPage()
	pdf.SetY(69)
	text(left, 365, 31, title, "B", 24, ink, "L")
	titleBottom := pdf.GetY()
	pdf.SetY(70)
	dateLabel := "Fecha del registro"
	if doc.emitted {
		dateLabel = "Fecha de emisión"
	}
	text(right-130, 130, 12, dateLabel, "B", 8.5, muted, "R")
	text(right-130, 130, 17, doc.at.In(s.documentZone(doc.IssuanceZone)).Format("02/01/2006"), "B", 11, ink, "R")
	pdf.SetY(max(titleBottom, pdf.GetY()) + 12)
	rule(left, right, pdf.GetY())
	pdf.Ln(12)
	name := strings.TrimSpace(doc.Author.name)
	specialty := strings.TrimSpace(pointer(doc.Author.Specialty))
	license := strings.TrimSpace(doc.Author.license)
	if name != "" || specialty != "" || license != "" {
		text(left, width, 13, "Profesional tratante", "B", 8.5, muted, "L")
		if name != "" {
			text(left, width, 19, name, "B", 14, ink, "L")
		}
		if specialty != "" {
			text(left, width, 15, specialty, "", 10, muted, "L")
		}
		if license != "" {
			text(left, width, 15, "Matrícula profesional: "+license, "", 10, muted, "L")
		}
		pdf.Ln(17)
	}

	insurance, err := wizard.Decode(doc.Patient.Insurance)
	if err != nil {
		return nil, err
	}
	provider, _ := insurance["providerName"].(string)
	coverage := strings.TrimSpace(provider)
	if coverage == "" {
		coverage = "No informada"
	}
	if plan, ok := insurance["planName"].(string); ok && has(plan) {
		coverage += " · Plan: " + strings.TrimSpace(plan)
	}
	if affiliate, ok := insurance["affiliateNumber"].(string); ok && has(affiliate) {
		coverage += " · Número de afiliación " + strings.TrimSpace(affiliate)
	}

	// A filled cell per wrapped line keeps long patient data inside the printable area.
	pdf.SetFillColor(246, 247, 242)
	patientLine := func(value, style string, size, leading float64, color [3]int) {
		font(style, size, color)
		value = strings.ReplaceAll(strings.ReplaceAll(translate(value), "\r", ""), "\t", " ")
		for _, part := range pdf.SplitText(value, width-30) {
			room(leading)
			y := pdf.GetY()
			pdf.Rect(left, y, width, leading, "F")
			pdf.SetX(left + 15)
			pdf.CellFormat(width-30, leading, part, "", 1, "L", false, 0, "")
		}
	}
	room(106)
	y := pdf.GetY()
	pdf.Rect(left, y, width, 11, "F")
	pdf.SetDrawColor(accent[0], accent[1], accent[2])
	pdf.SetLineWidth(1)
	pdf.Line(left, y, right, y)
	pdf.Ln(11)
	patientLine("Paciente", "B", 8.5, 13, muted)
	if patientName := strings.TrimSpace(doc.Patient.Name); patientName != "" {
		patientLine(patientName, "B", 15, 23, ink)
	}
	if has(pointer(doc.Patient.NationalID)) {
		patientLine("Documento de identidad: "+api.JavaTrim(*doc.Patient.NationalID), "", 10, 16, ink)
	}
	if has(pointer(doc.Patient.Age)) {
		patientLine("Edad: "+api.JavaTrim(*doc.Patient.Age), "", 10, 16, ink)
	}
	patientLine("Cobertura: "+coverage, "", 10, 16, ink)
	if pdf.GetY()+11 <= bottom {
		pdf.Rect(left, pdf.GetY(), width, 11, "F")
		pdf.Ln(11)
	}
	pdf.Ln(21)

	room(84)
	text(left, width, 23, section, "B", 15, accent, "L")
	rule(left, right, pdf.GetY()+3)
	pdf.Ln(11)

	if kind == "PRESCRIPTION" {
		for index, item := range medications {
			value := func(key string) string {
				field, _ := item[key].(string)
				return api.JavaTrim(field)
			}
			room(47)
			text(left, width, 19, fmt.Sprintf("%d. %s", index+1, value("medicationName")), "B", 13, ink, "L")
			duration := ""
			if days, ok := item["durationDays"].(float64); ok && days > 0 {
				duration = fmt.Sprintf("%.0f días", days)
				if days == 1 {
					duration = "1 día"
				}
			}
			for _, detail := range []struct{ label, value string }{
				{"Dosis", value("dosage")},
				{"Vía de administración", value("administrationRoute")},
				{"Frecuencia", value("frequency")},
				{"Duración", duration},
				{"Indicación", value("instructions")},
			} {
				if detail.value == "" {
					continue
				}
				room(16)
				y := pdf.GetY()
				text(left+13, 106, 16, detail.label, "B", 9, muted, "L")
				pdf.SetY(y)
				text(left+124, width-124, 16, detail.value, "", 10, ink, "L")
			}
			pdf.Ln(9)
			rule(left, right, pdf.GetY())
			pdf.Ln(9)
		}
	} else {
		const orderGutter = 32.0
		number := 0
		for _, savedOrder := range doc.Orders {
			order := strings.TrimSpace(savedOrder)
			if order == "" {
				continue
			}
			number++
			room(31)
			start := pdf.GetY()
			text(left, orderGutter-8, 19, fmt.Sprintf("%d.", number), "B", 10, accent, "L")
			pdf.SetY(start)
			text(left+orderGutter, width-orderGutter, 19, order, "B", 12, ink, "L")
			if pdf.GetY()+6 <= bottom {
				rule(left+orderGutter, right, pdf.GetY()+6)
			}
			pdf.Ln(14)
		}
	}
	if has(pointer(doc.Diagnosis)) {
		heading := "Diagnóstico"
		if kind == "STUDY_ORDERS" {
			heading = "Diagnóstico registrado"
		}
		diagnosis := api.JavaTrim(*doc.Diagnosis)
		if has(pointer(doc.DiagnosisCode)) {
			diagnosis = "[" + api.JavaTrim(*doc.DiagnosisCode) + "] " + diagnosis
		}
		room(36)
		field(left, width, heading, diagnosis, "", 10)
		pdf.Ln(8)
	}
	if has(pointer(doc.Instructions)) {
		heading := "Indicaciones generales"
		if kind == "STUDY_ORDERS" {
			heading = "Indicaciones para los estudios"
		}
		room(36)
		field(left, width, heading, api.JavaTrim(*doc.Instructions), "", 10)
	}
	room(86)
	pdf.Ln(41)
	rule(right-182, right, pdf.GetY())
	pdf.Ln(5)
	text(right-182, 182, 15, "Firma y sello del profesional", "B", 9, ink, "C")
	status := "Vista previa sin firma digital"
	if doc.emitted {
		status = "Emitido sin firma digital"
	}
	text(right-182, 182, 13, status, "", 8.5, muted, "C")
	var buffer bytes.Buffer
	if contentErr != nil {
		return nil, contentErr
	}
	if err := pdf.Output(&buffer); err != nil {
		return nil, api.Failure(500, "PDF_GENERATION_FAILED", "Error al generar el documento PDF clínico: "+err.Error())
	}
	return buffer.Bytes(), nil
}
