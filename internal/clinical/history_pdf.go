package clinical

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/growth"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
	"golang.org/x/image/font/sfnt"
)

type clinicalHistoryPDFData struct {
	Patient       Patient
	Encounter     Encounter
	Charts        []growth.PatientChart
	AuthorName    string
	AuthorLicense string
	Logo          *workspace.LogoAsset
	Signature     *workspace.SignatureAsset
	GeneratedAt   time.Time
	Zone          *time.Location
}

func consultationTypeLabel(code string) string {
	switch code {
	case "FIRST_VISIT":
		return "Primera consulta"
	case "FOLLOW_UP":
		return "Control"
	case "ROUTINE_CHECKUP":
		return "Chequeo de rutina"
	case "EMERGENCY":
		return "Urgencia"
	case "INTERCONSULTATION":
		return "Interconsulta"
	default:
		return code
	}
}

func formatLocalDate(iso string) string {
	if len(iso) >= 10 {
		if t, err := time.Parse("2006-01-02", iso[:10]); err == nil {
			return t.Format("02/01/2006")
		}
	}
	return iso
}

func formatCustomValue(raw any, field map[string]any) string {
	if raw == nil {
		return ""
	}
	kind, _ := field["type"].(string)
	switch v := raw.(type) {
	case bool:
		if v {
			return "Sí"
		}
		return "No"
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return ""
		}
		if kind == "DATE" {
			return formatLocalDate(trimmed)
		}
		if kind == "SELECT" {
			if opts, ok := field["options"].([]any); ok {
				for _, rawOpt := range opts {
					if opt, ok := rawOpt.(map[string]any); ok && opt["value"] == trimmed {
						if lbl, ok := opt["label"].(string); ok && strings.TrimSpace(lbl) != "" {
							return strings.TrimSpace(lbl)
						}
					}
				}
			}
		}
		return trimmed
	case json.Number:
		unit, _ := field["unit"].(string)
		if strings.TrimSpace(unit) != "" {
			return v.String() + " " + strings.TrimSpace(unit)
		}
		return v.String()
	case float64:
		unit, _ := field["unit"].(string)
		s := fmt.Sprintf("%g", v)
		if strings.TrimSpace(unit) != "" {
			return s + " " + strings.TrimSpace(unit)
		}
		return s
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func (s *Service) ClinicalHistoryPDF(ctx context.Context, identity api.Identity, patientID int64, encounterID int64, sigMode string, sigID *int64, growthRef string) ([]byte, string, error) {
	now := time.Now()
	if s.pdfLimiter != nil && !s.pdfLimiter.allow(identity.UserID, now) {
		return nil, "", api.Failure(429, "PDF_RATE_LIMITED", "Se generaron demasiados PDFs en pocos segundos. Esperá unos instantes y reintentá.")
	}
	growthRef = strings.ToUpper(strings.TrimSpace(growthRef))
	switch growthRef {
	case "", "AUTO":
		growthRef = "AUTO"
	case "OMS", "DOWN", "BOTH":
	default:
		return nil, "", api.Invalid("La referencia de curvas de crecimiento no es válida.")
	}

	data := clinicalHistoryPDFData{
		GeneratedAt: now, Zone: s.viewZone(ctx),
		AuthorName:    strings.TrimSpace(identity.ClinicalName),
		AuthorLicense: strings.TrimSpace(identity.ClinicalLicense),
	}
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		patient, err := ownedPatient(ctx, tx, identity.UserID, patientID)
		if err != nil {
			return err
		}
		if err := projectPatientInsurance(&patient); err != nil {
			return err
		}
		data.Patient = patient

		enc, err := scanEncounter(tx.QueryRow(ctx, "SELECT "+encounterColumns+encounterFrom+"WHERE e.id=$1 AND e.patient_id=$2", encounterID, patientID))
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(404, "ENCOUNTER_NOT_FOUND", "No se encontró la consulta.")
		}
		if err != nil {
			return err
		}
		data.Encounter = enc

		if s.workspaces != nil {
			sig, err := s.workspaces.ResolveSignature(ctx, tx, identity.UserID, sigMode, sigID)
			if err != nil {
				return err
			}
			data.Signature = sig
			license, logo, err := s.workspaces.ResolvePrintConfig(ctx, tx, identity.UserID, identity.ClinicalLicense)
			if err != nil {
				return err
			}
			data.AuthorLicense = license
			data.Logo = logo

			if err := s.workspaces.Feature(ctx, tx, identity.UserID, "GROWTH"); err != nil {
				var failure *api.Error
				if errors.As(err, &failure) && failure.Code == "FEATURE_UNAVAILABLE" {
					return nil
				}
				return err
			}
		}
		data.Charts, err = encounterGrowthCharts(s.growth, patient, enc, data.Zone, growthRef)
		return err
	})
	if err != nil {
		return nil, "", err
	}

	pdfBytes, err := renderClinicalHistoryPDF(data)
	if err != nil {
		return nil, "", err
	}
	slug := strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		if r == ' ' || r == '-' {
			return '-'
		}
		return -1
	}, data.Patient.FullName))
	if slug == "" {
		slug = fmt.Sprintf("paciente-%d", patientID)
	}
	return pdfBytes, fmt.Sprintf("consulta-%d-%s.pdf", encounterID, slug), nil
}

func renderClinicalHistoryPDF(data clinicalHistoryPDFData) ([]byte, error) {
	pdf := fpdf.NewCustom(&fpdf.InitType{OrientationStr: "P", UnitStr: "pt", Size: fpdf.SizeType{Wd: 595, Ht: 842}})
	pdf.AliasNbPages("{nb}")
	pdf.SetMargins(42, 36, 42)
	pdf.SetCellMargin(0)
	pdf.SetAutoPageBreak(true, 44)

	regular, err := clinicalFonts.ReadFile("fonts/DejaVuSansCondensed.ttf")
	if err != nil {
		return nil, err
	}
	glyphFont, err := sfnt.Parse(regular)
	if err != nil {
		return nil, err
	}
	for style, file := range map[string]string{"": "DejaVuSansCondensed.ttf", "B": "DejaVuSansCondensed-Bold.ttf"} {
		b, err := clinicalFonts.ReadFile("fonts/" + file)
		if err != nil {
			return nil, err
		}
		pdf.AddUTF8FontFromBytes("Clinical", style, b)
	}

	var glyphBuffer sfnt.Buffer
	translate := func(value string) string {
		var clean strings.Builder
		for _, r := range value {
			if r == '\n' || r == '\r' || r == '\t' {
				clean.WriteRune(r)
				continue
			}
			glyph, err := glyphFont.GlyphIndex(&glyphBuffer, r)
			if err != nil || glyph == 0 || r > 0xffff {
				clean.WriteRune(' ')
				continue
			}
			clean.WriteRune(r)
		}
		return clean.String()
	}

	const left, right, width, bottom = 42.0, 553.0, 511.0, 788.0
	ink := [3]int{28, 43, 42}
	muted := [3]int{74, 93, 91}
	accent := [3]int{22, 91, 82}
	border := [3]int{213, 222, 217}
	softBg := [3]int{246, 249, 248}

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
		pdf.SetLineWidth(0.6)
		pdf.Line(x1, y, x2, y)
	}
	room := func(h float64) {
		if pdf.GetY()+h > bottom {
			pdf.AddPage()
		}
	}
	sectionHeading := func(title string) {
		room(34)
		pdf.Ln(6)
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.Rect(left, pdf.GetY()+2, 3, 12, "F")
		text(left+9, width-9, 16, title, "B", 11, ink, "L")
		rule(left, right, pdf.GetY()+2)
		pdf.SetY(pdf.GetY() + 8)
	}

	type field struct {
		label, value string
	}
	valueOf := func(value *string) string {
		if value == nil {
			return ""
		}
		return strings.TrimSpace(*value)
	}
	row := func(fill [3]int, prominent bool, fields ...field) {
		var visible []field
		for _, item := range fields {
			if item.value = strings.TrimSpace(item.value); item.value != "" {
				visible = append(visible, item)
			}
		}
		if len(visible) == 0 {
			return
		}
		const pad, labelLine, gap = 11.0, 11.0, 2.0
		col := width / float64(len(visible))
		valueStyle, valueSize, valueLine := "", 10.0, 13.0
		if prominent {
			valueStyle, valueSize, valueLine = "B", 13.0, 17.0
		}
		labelHeight, valueHeight := labelLine, valueLine
		for _, item := range visible {
			font("B", 8, muted)
			labelHeight = max(labelHeight, labelLine*float64(len(pdf.SplitText(translate(item.label), col-2*pad))))
			font(valueStyle, valueSize, ink)
			valueHeight = max(valueHeight, valueLine*float64(len(pdf.SplitText(translate(item.value), col-2*pad))))
		}
		height := 2*pad + labelHeight + gap + valueHeight
		room(height)
		top := pdf.GetY()
		pdf.SetFillColor(fill[0], fill[1], fill[2])
		pdf.SetDrawColor(border[0], border[1], border[2])
		pdf.SetLineWidth(0.55)
		pdf.RoundedRect(left, top, width, height, 6, "1234", "FD")
		for i, item := range visible {
			column := left + float64(i)*col
			if i > 0 {
				pdf.Line(column, top+pad, column, top+height-pad)
			}
			pdf.SetY(top + pad)
			text(column+pad, col-2*pad, labelLine, item.label, "B", 8, muted, "L")
			pdf.SetY(top + pad + labelHeight + gap)
			text(column+pad, col-2*pad, valueLine, item.value, valueStyle, valueSize, ink, "L")
		}
		pdf.SetY(top + height + 6)
	}
	white := [3]int{255, 255, 255}
	notes := func(fields ...field) {
		var visible []field
		for _, item := range fields {
			if item.value = strings.TrimSpace(item.value); item.value != "" {
				visible = append(visible, item)
			}
		}
		lineCount := func(value string, w float64) int {
			font("", 10, ink)
			return len(pdf.SplitText(translate(value), w))
		}
		for len(visible) > 0 {
			item := visible[0]
			visible = visible[1:]
			if len(visible) > 0 && lineCount(item.value, width/2-22) <= 3 && lineCount(visible[0].value, width/2-22) <= 3 {
				row(white, false, item, visible[0])
				visible = visible[1:]
			} else if lineCount(item.value, width-22) <= 7 {
				row(white, false, item)
			} else {
				// Let long notes flow across pages without a fixed-height box.
				font("B", 8, muted)
				labelHeight := 11 * float64(len(pdf.SplitText(translate(item.label), width-22)))
				room(labelHeight + 26)
				text(left+11, width-22, 11, item.label, "B", 8, muted, "L")
				text(left+11, width-22, 13, item.value, "", 10, ink, "L")
				pdf.Ln(8)
			}
		}
	}

	const docTitle = "Registro de Consulta Clínica"
	enc := data.Encounter
	encounterLocal := enc.instant.In(data.Zone)
	pdf.SetTitle(docTitle+" · "+data.Patient.FullName+" | DoctorChart", true)

	pdf.SetHeaderFunc(func() {
		if pdf.PageNo() > 1 {
			pdf.SetY(28)
			text(left, 260, 13, docTitle+" · "+data.Patient.FullName, "B", 8.5, accent, "L")
			pdf.SetY(28)
			text(right-160, 160, 13, data.GeneratedAt.In(data.Zone).Format("02/01/2006 15:04"), "", 8, muted, "R")
			rule(left, right, 45)
			pdf.SetY(54)
		}
	})
	pdf.SetFooterFunc(func() {
		pdf.SetY(-28)
		rule(left, right, pdf.GetY()-4)
		text(left, 260, 12, "DoctorChart · Documento clínico confidencial", "", 7.5, muted, "L")
		pdf.SetY(-28)
		text(right-150, 150, 12, fmt.Sprintf("Página %d de {nb}", pdf.PageNo()), "", 7.5, muted, "R")
	})

	pdf.AddPage()

	// Top editorial header
	pdf.SetFillColor(accent[0], accent[1], accent[2])
	pdf.Rect(left, 34, 24, 3, "F")
	pdf.SetY(42)
	text(left, 355.0, 13, "DoctorChart", "B", 10, accent, "L")
	text(left, 355.0, 24, docTitle, "B", 20, ink, "L")
	headerBottom := pdf.GetY()

	pdf.SetY(44)
	text(right-145, 145, 11, "Fecha de emisión", "B", 8, muted, "R")
	text(right-145, 145, 14, data.GeneratedAt.In(data.Zone).Format("02/01/2006 15:04"), "B", 9.5, ink, "R")
	pdf.SetY(max(headerBottom, pdf.GetY()) + 10)
	rule(left, right, pdf.GetY())
	pdf.Ln(10)

	// Attending physician block (license printed ONLY if non-empty)
	hasLogo := data.Logo != nil && len(data.Logo.PNG) > 0
	if data.AuthorName != "" || data.AuthorLicense != "" || hasLogo {
		authorStartY := pdf.GetY()
		authorW := width
		logoBottom := authorStartY
		if hasLogo {
			lw, lh := float64(data.Logo.Width), float64(data.Logo.Height)
			if lw <= 0 {
				lw = 56
			}
			if lh <= 0 {
				lh = 56
			}
			scale := min(56.0/lw, 56.0/lh)
			drawW, drawH := lw*scale, lh*scale
			logoName := "hc-logo-" + data.Logo.SHA256
			if len(logoName) > 20 {
				logoName = logoName[:20]
			}
			opts := fpdf.ImageOptions{ImageType: "PNG", ReadDpi: false}
			pdf.RegisterImageOptionsReader(logoName, opts, bytes.NewReader(data.Logo.PNG))
			pdf.ImageOptions(logoName, right-drawW, authorStartY, drawW, drawH, false, opts, 0, "")
			logoBottom = authorStartY + drawH
			authorW = width - drawW - 16
		}
		if data.AuthorName != "" || data.AuthorLicense != "" {
			text(left, authorW, 11, "Profesional tratante", "B", 8, muted, "L")
			if data.AuthorName != "" {
				text(left, authorW, 16, data.AuthorName, "B", 12, ink, "L")
			}
			if data.AuthorLicense != "" {
				text(left, authorW, 13, "Matrícula profesional: "+data.AuthorLicense, "", 9, muted, "L")
			}
		}
		pdf.SetY(max(pdf.GetY(), logoBottom))
		pdf.Ln(8)
	}

	p := data.Patient
	birthStr := formatLocalDate(p.BirthDate)
	ageStr := ""
	if birth, err := time.Parse("2006-01-02", p.BirthDate); err == nil {
		day := time.Date(encounterLocal.Year(), encounterLocal.Month(), encounterLocal.Day(), 0, 0, 0, 0, time.UTC)
		if age := printAge(birth, day); age != nil {
			ageStr = *age
		}
	}
	dniStr := valueOf(p.NationalID)
	if dniStr == "" {
		dniStr = "No informado"
	}
	gender := strings.ToUpper(valueOf(p.Gender))
	genderLabel := "No especificado"
	patientFill, badgeFill := softBg, [3]int{222, 237, 230}
	switch gender {
	case "MALE":
		genderLabel, patientFill, badgeFill = "Varón", [3]int{239, 248, 250}, [3]int{211, 233, 241}
	case "FEMALE":
		genderLabel, patientFill, badgeFill = "Mujer", [3]int{253, 245, 248}, [3]int{247, 225, 236}
	case "OTHER":
		genderLabel = "Otro"
	}

	coverage := "Particular / Sin cobertura registrada"
	if ins, err := wizard.Decode(p.Insurance); err == nil && ins != nil {
		if prov, ok := ins["providerName"].(string); ok && strings.TrimSpace(prov) != "" {
			coverage = strings.TrimSpace(prov)
			if plan, ok := ins["planName"].(string); ok && strings.TrimSpace(plan) != "" {
				coverage += " · Plan " + strings.TrimSpace(plan)
			}
			if aff, ok := ins["affiliateNumber"].(string); ok && strings.TrimSpace(aff) != "" {
				coverage += " · Afiliado " + strings.TrimSpace(aff)
			}
		}
	}

	const nameX, nameWidth, coverageX, coverageWidth = left + 59, 244.0, left + 325, 174.0
	font("B", 16, ink)
	nameHeight := 19 * float64(len(pdf.SplitText(translate(p.FullName), nameWidth)))
	font("", 9.5, ink)
	coverageHeight := 11 + 13*float64(len(pdf.SplitText(translate(coverage), coverageWidth)))
	cardHeight := max(56, max(nameHeight, coverageHeight)+22)
	room(30 + cardHeight)
	sectionHeading("Datos del paciente")
	top := pdf.GetY()
	pdf.SetFillColor(patientFill[0], patientFill[1], patientFill[2])
	pdf.SetDrawColor(border[0], border[1], border[2])
	pdf.SetLineWidth(0.55)
	pdf.RoundedRect(left, top, width, cardHeight, 7, "1234", "FD")
	drawConsultationPatientBadge(pdf, left+30, top+cardHeight/2, gender, badgeFill, accent)
	pdf.SetY(top + (cardHeight-nameHeight)/2)
	text(nameX, nameWidth, 19, p.FullName, "B", 16, ink, "L")
	pdf.SetY(top + (cardHeight-coverageHeight)/2)
	text(coverageX, coverageWidth, 11, "Cobertura", "B", 8, muted, "L")
	text(coverageX, coverageWidth, 13, coverage, "", 9.5, ink, "L")
	pdf.SetY(top + cardHeight + 5)
	row(white, false, field{"Documento", dniStr}, field{"Nacimiento", birthStr}, field{"Edad en la consulta", ageStr}, field{"Sexo registrado", genderLabel})

	room(80)
	sectionHeading("Datos de la consulta")
	row(softBg, false,
		field{"Fecha de atención", encounterLocal.Format("02/01/2006 15:04")},
		field{"Tipo de atención", consultationTypeLabel(enc.Type)},
		field{"Especialidad", enc.Specialty.Name},
		field{"Ficha", valueOf(enc.TemplateName)},
	)
	notes(
		field{"Motivo de consulta", valueOf(enc.Complaint)},
		field{"Enfermedad", valueOf(enc.Disease)},
		field{"Enfermedad actual / Evolución", valueOf(enc.History)},
		field{"Examen físico", valueOf(enc.Physical)},
	)

	var measurements []field
	if enc.Measurements.Weight != nil {
		measurements = append(measurements, field{"Peso", fmt.Sprintf("%.2f kg", *enc.Measurements.Weight)})
	}
	if enc.Measurements.Height != nil {
		measurements = append(measurements, field{"Longitud / talla", fmt.Sprintf("%.1f cm", *enc.Measurements.Height)})
	}
	if enc.Measurements.Head != nil {
		measurements = append(measurements, field{"Perímetro cefálico", fmt.Sprintf("%.1f cm", *enc.Measurements.Head)})
	}
	if len(measurements) > 0 {
		room(68)
		text(left, width, 15, "Mediciones", "B", 10, accent, "L")
		pdf.Ln(3)
		row(patientFill, true, measurements...)
	}

	diagnosis := valueOf(enc.Diagnosis)
	if code := valueOf(enc.DiagnosisCode); code != "" && diagnosis != "" {
		diagnosis = "[" + code + "] " + diagnosis
	}
	notes(
		field{"Diagnóstico", diagnosis},
		field{"Tratamiento / Plan", valueOf(enc.Treatment)},
		field{"Seguimiento", valueOf(enc.FollowUp)},
		field{"Próximo control", formatLocalDate(valueOf(enc.FollowUpDate))},
	)

	var customFields []field
	// Custom encounterForm fields not already projected above
	if eForm, err := wizard.Decode(enc.Snapshot); err == nil {
		if eVals, err := wizard.Decode(enc.Values); err == nil {
			visible := wizard.VisibleValues(eForm, eVals)
			projectedKeys := map[string]bool{
				"sys_disease": true, "disease": true, "enc_chief_complaint": true,
				"chiefComplaint": true, "complaint": true, "historyOfPresentIllness": true,
				"presentIllness": true, "physicalExamination": true, "physicalExam": true,
				"primaryDiagnosisDescription": true, "diagnosis": true, "treatmentPlan": true,
				"treatment": true, "followUpInstructions": true, "followUp": true, "followUpDate": true,
				"weightKg": true, "weight": true, "heightCm": true, "height": true,
				"headCircumferenceCm": true, "headCircumference": true,
			}
			if cats, ok := eForm["categories"].([]any); ok {
				for _, rawCat := range cats {
					cat, _ := rawCat.(map[string]any)
					if cat["key"] == "sys_clinical_history" {
						continue
					}
					if fields, ok := cat["fields"].([]any); ok {
						for _, rawF := range fields {
							f, _ := rawF.(map[string]any)
							k, _ := f["key"].(string)
							lbl, _ := f["label"].(string)
							if projectedKeys[k] || strings.HasPrefix(k, "encounter.") {
								continue
							}
							if formatted := formatCustomValue(visible[k], f); formatted != "" {
								customFields = append(customFields, field{lbl, formatted})
							}
						}
					}
				}
			}
		}
	}

	// Formula results if any available
	var fResults map[string]wizard.FormulaResult
	if len(enc.Results) > 0 && string(enc.Results) != "null" && json.Unmarshal(enc.Results, &fResults) == nil && len(fResults) > 0 {
		keys := make([]string, 0, len(fResults))
		for k, r := range fResults {
			if r.Status == "READY" && r.Value != nil {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			var calcParts []string
			for _, k := range keys {
				r := fResults[k]
				calcParts = append(calcParts, fmt.Sprintf("%s: %g", k, *r.Value))
			}
			customFields = append(customFields, field{"Cálculos clínicos", strings.Join(calcParts, "  ·  ")})
		}
	}
	notes(customFields...)
	pdf.Ln(4)

	if len(data.Charts) > 0 {
		room(growthChartHeight + 30)
		sectionHeading("Curvas de crecimiento de la consulta")
		for _, chart := range data.Charts {
			room(growthChartHeight)
			renderVectorGrowthChart(pdf, translate, chart, left, width)
		}
	}

	// Section 7: Signature Block
	if data.Signature != nil {
		stamp := strings.TrimSpace(data.Signature.StampText)
		w, h := float64(data.Signature.Width), float64(data.Signature.Height)
		if w <= 0 {
			w = 156
		}
		if h <= 0 {
			h = 54
		}
		scale := min(156.0/w, 54.0/h)
		drawW, drawH := w*scale, h*scale
		room(drawH + 64)
		pdf.Ln(16)
		sigY := pdf.GetY()
		sigX := (right - 182) + (182-drawW)/2
		imgName := "hc-sig-" + data.Signature.SHA256
		if len(imgName) > 20 {
			imgName = imgName[:20]
		}
		opts := fpdf.ImageOptions{ImageType: "PNG", ReadDpi: false}
		pdf.RegisterImageOptionsReader(imgName, opts, bytes.NewReader(data.Signature.ImagePNG))
		pdf.ImageOptions(imgName, sigX, sigY, drawW, drawH, false, opts, 0, "")
		lineY := sigY + drawH + 4
		rule(right-182, right, lineY)
		pdf.SetY(lineY + 5)
		text(right-182, 182, 14, "Firma y sello del profesional", "B", 9, ink, "C")
		if stamp != "" {
			text(right-182, 182, 13, stamp, "", 8.5, ink, "C")
		}
		if data.AuthorLicense != "" && (stamp == "" || !strings.Contains(stamp, data.AuthorLicense)) {
			text(right-182, 182, 13, "Matrícula profesional: "+data.AuthorLicense, "", 8.5, muted, "C")
		}
	} else {
		room(90)
		pdf.Ln(32)
		rule(right-182, right, pdf.GetY())
		pdf.Ln(5)
		text(right-182, 182, 14, "Firma y sello del profesional", "B", 9, ink, "C")
		if data.AuthorName != "" {
			text(right-182, 182, 13, data.AuthorName, "B", 8.5, ink, "C")
		}
		if data.AuthorLicense != "" {
			text(right-182, 182, 13, "Matrícula profesional: "+data.AuthorLicense, "", 8.5, muted, "C")
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, api.Failure(500, "PDF_GENERATION_FAILED", "No se pudo generar el PDF de la consulta: "+err.Error())
	}
	return buf.Bytes(), nil
}

// Draw the patient detail as vectors so it stays sharp on paper.
func drawConsultationPatientBadge(pdf *fpdf.Fpdf, x, y float64, gender string, fill, accent [3]int) {
	pdf.SetFillColor(fill[0], fill[1], fill[2])
	pdf.Circle(x, y, 19, "F")
	pdf.SetFillColor(255, 253, 250)
	pdf.SetDrawColor(accent[0], accent[1], accent[2])
	pdf.SetLineWidth(0.9)

	switch gender {
	case "MALE":
		pdf.Circle(x-8, y-7, 4.3, "FD")
		pdf.Circle(x+8, y-7, 4.3, "FD")
	case "FEMALE":
		pdf.Ellipse(x-4, y-8, 3, 6.7, 16, "FD")
		pdf.Ellipse(x+4, y-8, 3, 6.7, -16, "FD")
	default:
		pdf.Polygon([]fpdf.PointType{
			{X: x, Y: y - 10}, {X: x + 3, Y: y - 3}, {X: x + 10, Y: y},
			{X: x + 3, Y: y + 3}, {X: x, Y: y + 10}, {X: x - 3, Y: y + 3},
			{X: x - 10, Y: y}, {X: x - 3, Y: y - 3},
		}, "FD")
		return
	}
	pdf.Ellipse(x, y+2, 10, 8.3, 0, "FD")
	pdf.SetFillColor(accent[0], accent[1], accent[2])
	pdf.Circle(x-3.7, y+1, 0.8, "F")
	pdf.Circle(x+3.7, y+1, 0.8, "F")
	pdf.CurveBezierCubic(x-2.2, y+4, x-1.2, y+6, x+1.2, y+6, x+2.2, y+4, "D")
}
