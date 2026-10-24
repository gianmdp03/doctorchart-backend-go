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
	pdf.SetMargins(40, 36, 40)
	pdf.SetCellMargin(0)
	pdf.SetAutoPageBreak(true, 36)

	regular, err := clinicalFonts.ReadFile("fonts/DejaVuSansCondensed.ttf")
	if err != nil {
		return nil, err
	}
	glyphFont, err := sfnt.Parse(regular)
	if err != nil {
		return nil, err
	}
	for style, file := range map[string]string{"": "DejaVuSansCondensed.ttf", "B": "DejaVuSansCondensed-Bold.ttf", "I": "DejaVuSansCondensed-Oblique.ttf"} {
		data, err := clinicalFonts.ReadFile("fonts/" + file)
		if err != nil {
			return nil, err
		}
		pdf.AddUTF8FontFromBytes("Clinical", style, data)
	}
	pdf.AddPage()
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
	has := func(value string) bool { return api.JavaTrim(value) != "" }
	fallback := func(value, otherwise string) string {
		if has(value) {
			return api.JavaTrim(value)
		}
		return otherwise
	}
	pointer := func(value *string) string {
		if value == nil {
			return ""
		}
		return *value
	}
	primary, muted, dark, accent := []int{15, 23, 42}, []int{100, 116, 139}, []int{51, 65, 85}, []int{13, 148, 136}
	font := func(style string, size float64, color []int) {
		pdf.SetFont("Clinical", style, size)
		pdf.SetTextColor(color[0], color[1], color[2])
	}
	line := func(width, height float64, value, style string, size float64, color []int, align string) {
		font(style, size, color)
		pdf.MultiCell(width, height, translate(value), "", align, false)
	}
	// Keep medicine names distinct from doses and field labels from clinical values.
	rich := func(x, width, height float64, label, value string, labelSize, valueSize float64, labelStyle, valueStyle string) {
		pdf.SetX(x)
		pdf.SetRightMargin(595 - x - width)
		font(labelStyle, labelSize, primary)
		pdf.Write(height, translate(label))
		font(valueStyle, valueSize, dark)
		pdf.Write(height, translate(value))
		pdf.Ln(height)
		pdf.SetRightMargin(40)
	}
	column := func(x, width, y float64, values []string, sizes []float64, boldFirst bool, align string) float64 {
		for index, value := range values {
			if !has(value) {
				continue
			}
			pdf.SetXY(x, y)
			style := ""
			color := muted
			if index == 0 {
				color = primary
				if boldFirst {
					style = "B"
				}
			}
			line(width, sizes[index]*1.5, value, style, sizes[index], color, align)
			y = pdf.GetY()
		}
		return y
	}
	clinic := fallback(metadata.Get("clinicName"), "DoctorChart")
	doctor := fallback(metadata.Get("doctorName"), "Profesional Médico")
	specialty := fallback(metadata.Get("specialty"), api.JavaTrim(pointer(doc.Author.Specialty)))
	phone := ""
	if has(metadata.Get("clinicPhone")) {
		phone = "Tel: " + api.JavaTrim(metadata.Get("clinicPhone"))
	}
	leftEnd := column(40, 283.25, 42, []string{clinic, api.JavaTrim(metadata.Get("clinicAddress")), phone}, []float64{13, 8.5, 8.5}, true, "L")
	rightEnd := column(323.25, 231.75, 41.4, []string{doctor, specialty}, []float64{12, 8.5}, true, "R")
	end := leftEnd
	if rightEnd > end {
		end = rightEnd
	}
	pdf.SetDrawColor(226, 232, 240)
	pdf.SetLineWidth(1)
	pdf.Line(40, end+8, 555, end+8)
	pdf.SetY(end + 22)
	title := "RECETA MÉDICA"
	if kind == "STUDY_ORDERS" {
		title = "ORDEN DE ESTUDIOS MÉDICOS"
	}
	y := pdf.GetY()
	pdf.SetX(40)
	line(309, 19.5, title, "B", 13, accent, "L")
	pdf.SetXY(349, y)
	line(206, 12.75, "Fecha: "+doc.at.In(s.documentZone(doc.IssuanceZone)).Format("02/01/2006")+"  |  ID: "+doc.Code, "", 8.5, dark, "R")
	pdf.SetY(y + 27.5)
	insurance, err := wizard.Decode(doc.Patient.Insurance)
	if err != nil {
		return nil, err
	}
	coverage := "Particular / Sin obra social informada"
	if provider, ok := insurance["providerName"].(string); ok && has(provider) {
		coverage = api.JavaTrim(provider)
		if plan, ok := insurance["planName"].(string); ok && has(plan) {
			coverage += " " + api.JavaTrim(plan)
		}
		if affiliate, ok := insurance["affiliateNumber"].(string); ok && has(affiliate) {
			coverage += " · N° " + api.JavaTrim(affiliate)
		}
	}
	y = pdf.GetY()
	pdf.SetFillColor(248, 250, 252)
	pdf.SetDrawColor(226, 232, 240)
	pdf.SetLineWidth(1)
	// Grow the patient box for wrapped names/coverage; no medical field is clipped.
	font("", 9.5, primary)
	leftLines := len(pdf.SplitText(translate("Paciente: "+fallback(doc.Patient.Name, "No especificado")), 287))
	coverageLines := len(pdf.SplitText(translate("Cobertura: "+coverage), 287))
	rightLines := len(pdf.SplitText(translate("DNI / Doc: "+fallback(pointer(doc.Patient.NationalID), "No informado")), 187))
	if rightLines > leftLines {
		leftLines = rightLines
	}
	if leftLines < 1 {
		leftLines = 1
	}
	if coverageLines < 1 {
		coverageLines = 1
	}
	height := 16 + float64(leftLines+coverageLines)*20
	pdf.Rect(40, y, 515, height, "DF")
	pdf.SetY(y + 10)
	rich(50, 287, 20, "Paciente: ", fallback(doc.Patient.Name, "No especificado"), 8.5, 10, "B", "B")
	pdf.SetY(y + 10)
	rich(349.4, 195.6, 20, "DNI / Doc: ", fallback(pointer(doc.Patient.NationalID), "No informado"), 8.5, 9.5, "B", "")
	secondY := y + 10 + float64(leftLines)*20
	pdf.SetY(secondY)
	rich(50, 287, 20, "Cobertura: ", coverage, 8.5, 9.5, "B", "")
	pdf.SetY(secondY)
	rich(349.4, 195.6, 20, "Edad: ", fallback(pointer(doc.Patient.Age), "No informada"), 8.5, 9.5, "B", "")
	pdf.SetY(y + height + 14)
	if kind == "PRESCRIPTION" {
		line(515, 22.5, "Rp/", "B", 15, accent, "L")
		pdf.Ln(8)
		for index, item := range medications {
			text := func(key string) string { value, _ := item[key].(string); return value }
			pdf.SetX(42)
			main := fmt.Sprintf("%d. %s", index+1, text("medicationName"))
			dose := ""
			if has(text("dosage")) {
				dose += "  " + api.JavaTrim(text("dosage"))
			}
			if has(text("administrationRoute")) {
				dose += " (" + api.JavaTrim(text("administrationRoute")) + ")"
			}
			rich(42, 511, 16, main, dose, 10, 9, "B", "")
			frequency := api.JavaTrim(text("frequency"))
			duration := ""
			if days, ok := item["durationDays"].(float64); ok && days > 0 {
				duration = fmt.Sprintf("Duración: %.0f días", days)
			}
			sub := ""
			if has(frequency) {
				sub = "Frecuencia: " + frequency
			}
			if sub != "" && duration != "" {
				sub += "  ·  "
			}
			sub += duration
			if sub != "" {
				pdf.SetX(54)
				line(499, 16, sub, "", 9, dark, "L")
			}
			if has(text("instructions")) {
				pdf.SetX(54)
				line(499, 16, "Indicación: "+api.JavaTrim(text("instructions")), "I", 8.5, dark, "L")
			}
			pdf.SetDrawColor(226, 232, 240)
			pdf.SetLineWidth(.5)
			pdf.Line(40, pdf.GetY()+6, 555, pdf.GetY()+6)
			pdf.Ln(10)
		}
	} else {
		line(515, 13.5, "Solicito la realización de los siguientes estudios diagnósticos y prácticas:", "", 9, dark, "L")
		pdf.Ln(8)
		for index, order := range doc.Orders {
			pdf.SetX(42)
			line(511, 15, fmt.Sprintf("%d. %s", index+1, order), "B", 10, primary, "L")
			pdf.SetDrawColor(226, 232, 240)
			pdf.SetLineWidth(.5)
			pdf.Line(40, pdf.GetY()+6, 555, pdf.GetY()+6)
			pdf.Ln(11)
		}
	}
	pdf.Ln(12)
	if has(pointer(doc.Diagnosis)) || has(pointer(doc.Instructions)) {
		notes := []string{}
		if has(pointer(doc.Diagnosis)) {
			label := "Diagnóstico: "
			if kind == "STUDY_ORDERS" {
				label = "Diagnóstico presuntivo: "
			}
			if has(pointer(doc.DiagnosisCode)) {
				label += "[" + api.JavaTrim(*doc.DiagnosisCode) + "] "
			}
			notes = append(notes, label+api.JavaTrim(*doc.Diagnosis))
		}
		if has(pointer(doc.Instructions)) {
			label := "Indicaciones generales: "
			if kind == "STUDY_ORDERS" {
				label = "Preparación / Indicaciones: "
			}
			notes = append(notes, label+api.JavaTrim(*doc.Instructions))
		}
		font("", 9, dark)
		y := pdf.GetY()
		height := 16.0
		for _, note := range notes {
			height += float64(len(pdf.SplitText(translate(note), 499))) * 16
		}
		if y+height > 806 {
			pdf.AddPage()
			y = pdf.GetY()
		}
		pdf.SetDrawColor(226, 232, 240)
		pdf.SetFillColor(248, 250, 252)
		pdf.SetLineWidth(.5)
		pdf.Rect(40, y, 515, height, "DF")
		pdf.SetY(y + 8)
		for _, note := range notes {
			label, value, _ := strings.Cut(note, ": ")
			rich(48, 499, 16, label+": ", value, 9, 9, "B", "")
		}
		pdf.SetY(y + height + 16)
	}
	// Keep the entire signature block on one page.
	if pdf.GetY()+100 > 806 {
		pdf.AddPage()
	}
	y = pdf.GetY() + 20
	origin := "Receta Médica Asistencial"
	if kind == "STUDY_ORDERS" {
		origin = "Orden de Prácticas y Estudios"
	}
	pdf.SetXY(40, y+44)
	line(231.75, 12, "Documento emitido: "+doc.Code+"\n"+origin, "", 8, muted, "L")
	pdf.SetXY(271.75, y)
	line(283.25, 12, "____________________________________", "", 8, muted, "C")
	pdf.SetX(271.75)
	line(283.25, 14.25, doctor, "B", 9.5, primary, "C")
	pdf.SetX(271.75)
	if specialty != "" {
		line(283.25, 12, specialty, "", 8, muted, "C")
	}
	pdf.SetX(271.75)
	line(283.25, 12, "Firma y Sello del Profesional", "", 8, muted, "C")
	var buffer bytes.Buffer
	if contentErr != nil {
		return nil, contentErr
	}
	if err := pdf.Output(&buffer); err != nil {
		return nil, api.Failure(500, "PDF_GENERATION_FAILED", "Error al generar el documento PDF clínico: "+err.Error())
	}
	return buffer.Bytes(), nil
}
