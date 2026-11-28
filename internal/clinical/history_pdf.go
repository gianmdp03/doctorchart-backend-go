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

	const docTitle = "Registro de Consulta Clínica"
	enc := data.Encounter
	encounterLocal := enc.instant.In(data.Zone)
	specialty := enc.Specialty.Name
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
	if data.AuthorName != "" || specialty != "" || data.AuthorLicense != "" || hasLogo {
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
		if data.AuthorName != "" || specialty != "" || data.AuthorLicense != "" {
			text(left, authorW, 11, "Profesional tratante", "B", 8, muted, "L")
			if data.AuthorName != "" {
				text(left, authorW, 16, data.AuthorName, "B", 12, ink, "L")
			}
			metaParts := []string{}
			if specialty != "" {
				metaParts = append(metaParts, specialty)
			}
			if data.AuthorLicense != "" {
				metaParts = append(metaParts, "Matrícula profesional: "+data.AuthorLicense)
			}
			if len(metaParts) > 0 {
				text(left, authorW, 13, strings.Join(metaParts, "  ·  "), "", 9, muted, "L")
			}
		}
		pdf.SetY(max(pdf.GetY(), logoBottom))
		pdf.Ln(8)
	}

	// Section 1: Patient Demographics & Coverage
	sectionHeading("Datos del paciente")
	p := data.Patient
	birthStr := formatLocalDate(p.BirthDate)
	if birth, err := time.Parse("2006-01-02", p.BirthDate); err == nil {
		day := time.Date(encounterLocal.Year(), encounterLocal.Month(), encounterLocal.Day(), 0, 0, 0, 0, time.UTC)
		if age := printAge(birth, day); age != nil {
			birthStr += " (" + *age + ")"
		}
	}
	dniStr := "No informado"
	if p.NationalID != nil && strings.TrimSpace(*p.NationalID) != "" {
		dniStr = strings.TrimSpace(*p.NationalID)
	}
	genderStr := "No especificado"
	if p.Gender != nil {
		switch strings.ToUpper(strings.TrimSpace(*p.Gender)) {
		case "MALE":
			genderStr = "Varón"
		case "FEMALE":
			genderStr = "Mujer"
		case "OTHER":
			genderStr = "Otro"
		}
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

	boxY := pdf.GetY()
	pdf.SetFillColor(softBg[0], softBg[1], softBg[2])
	pdf.SetDrawColor(border[0], border[1], border[2])
	pdf.Rect(left, boxY, width, 74, "FD")
	pdf.SetXY(left+10, boxY+8)
	text(left+10, 245, 11, "Nombre completo", "B", 7.5, muted, "L")
	text(left+10, 245, 15, p.FullName, "B", 11, ink, "L")
	pdf.SetXY(left+265, boxY+8)
	text(left+265, 236, 11, "DNI / Documento", "B", 7.5, muted, "L")
	text(left+265, 236, 15, dniStr, "", 10, ink, "L")

	pdf.SetY(boxY + 38)
	text(left+10, 245, 11, "Fecha de nacimiento y edad en la consulta", "B", 7.5, muted, "L")
	text(left+10, 245, 14, birthStr, "", 9.5, ink, "L")
	pdf.SetY(boxY + 38)
	text(left+265, 236, 11, "Sexo / Cobertura", "B", 7.5, muted, "L")
	text(left+265, 236, 14, genderStr+" · "+coverage, "", 9.5, ink, "L")
	pdf.SetY(boxY + 80)

	room(70)
	sectionHeading("Datos de la consulta")
	dt := encounterLocal.Format("02/01/2006 15:04")
	headerLine := dt + "  ·  " + consultationTypeLabel(enc.Type)
	if enc.TemplateName != nil && *enc.TemplateName != "" {
		headerLine += "  ·  " + *enc.TemplateName
	}
	if enc.Specialty.Name != "" {
		headerLine += "  (" + enc.Specialty.Name + ")"
	}
	pdf.SetFillColor(238, 244, 242)
	pdf.SetX(left)
	font("B", 9.5, ink)
	pdf.CellFormat(width, 18, translate("  "+headerLine), "1", 1, "L", true, 0, "")
	pdf.Ln(4)

	printRow := func(label, val string) {
		if strings.TrimSpace(val) == "" {
			return
		}
		room(24)
		text(left+6, width-6, 11, label, "B", 8, muted, "L")
		text(left+6, width-6, 13, strings.TrimSpace(val), "", 9.5, ink, "L")
		pdf.Ln(3)
	}
	if enc.Disease != nil {
		printRow("Enfermedad", *enc.Disease)
	}
	if enc.Complaint != nil {
		printRow("Motivo de consulta", *enc.Complaint)
	}
	if enc.History != nil {
		printRow("Enfermedad actual / Evolución", *enc.History)
	}
	if enc.Physical != nil {
		printRow("Examen físico", *enc.Physical)
	}
	// Measurements inline if any
	var meas []string
	if enc.Measurements.Weight != nil {
		meas = append(meas, fmt.Sprintf("Peso: %.2f kg", *enc.Measurements.Weight))
	}
	if enc.Measurements.Height != nil {
		meas = append(meas, fmt.Sprintf("Talla: %.1f cm", *enc.Measurements.Height))
	}
	if enc.Measurements.Head != nil {
		meas = append(meas, fmt.Sprintf("Perímetro cefálico: %.1f cm", *enc.Measurements.Head))
	}
	if len(meas) > 0 {
		printRow("Mediciones", strings.Join(meas, "  ·  "))
	}
	if enc.Diagnosis != nil {
		diag := *enc.Diagnosis
		if enc.DiagnosisCode != nil && strings.TrimSpace(*enc.DiagnosisCode) != "" {
			diag = "[" + strings.TrimSpace(*enc.DiagnosisCode) + "] " + diag
		}
		printRow("Diagnóstico", diag)
	}
	if enc.Treatment != nil {
		printRow("Tratamiento / Plan", *enc.Treatment)
	}
	if enc.FollowUp != nil {
		fu := *enc.FollowUp
		if enc.FollowUpDate != nil && strings.TrimSpace(*enc.FollowUpDate) != "" {
			fu += " (Próximo control: " + formatLocalDate(*enc.FollowUpDate) + ")"
		}
		printRow("Seguimiento", fu)
	}

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
								printRow(lbl, formatted)
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
			printRow("Cálculos clínicos", strings.Join(calcParts, "  ·  "))
		}
	}
	pdf.Ln(6)

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
