package clinical

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

//go:embed calculators.json
var calculatorJSON []byte

type interpretation struct {
	Code     string   `json:"code"`
	Min      *float64 `json:"min"`
	Max      *float64 `json:"max"`
	Label    string   `json:"label"`
	Severity string   `json:"severity"`
	Action   *string  `json:"clinicalAction"`
}
type calculatorMetadata struct {
	Name      string           `json:"name"`
	Ranges    []interpretation `json:"ranges"`
	Evolution string           `json:"evolution"`
	// 0.9938^age from the pinned reference JVM, covering the bounded integer domain.
	AgeFactors []float64 `json:"ageFactors,omitempty"`
}
type secondaryOutput struct {
	Label       string  `json:"label"`
	Value       string  `json:"value"`
	Unit        *string `json:"unit"`
	Description *string `json:"description"`
}
type calculationResult struct {
	Code           string            `json:"calculatorId"`
	Name           string            `json:"calculationName"`
	Raw            float64           `json:"rawValue"`
	Formatted      string            `json:"formattedValue"`
	Unit           string            `json:"unit"`
	Interpretation interpretation    `json:"interpretation"`
	Outputs        []secondaryOutput `json:"secondaryOutputs"`
	Action         *string           `json:"clinicalAction"`
	Notes          []string          `json:"clinicalNotes"`
	At             string            `json:"calculatedAt"`
	Evolution      string            `json:"plainTextEvolution"`
}

func invalidCalculation() error {
	return api.Failure(400, "INVALID_CALCULATION_INPUTS", "Los parámetros de la calculadora no son válidos.")
}

// Java constructs BigDecimal from the exact binary double, then rounds HALF_UP.
func calculationFixed(value float64, decimals int) (string, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", invalidCalculation()
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	ratio := new(big.Rat).SetFloat64(value)
	ratio.Mul(ratio, new(big.Rat).SetInt(scale))
	numerator := new(big.Int).Abs(ratio.Num())
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, ratio.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(ratio.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	digits := quotient.String()
	if decimals > 0 {
		if len(digits) <= decimals {
			digits = strings.Repeat("0", decimals+1-len(digits)) + digits
		}
		digits = digits[:len(digits)-decimals] + "." + digits[len(digits)-decimals:]
	}
	if value < 0 && quotient.Sign() != 0 {
		digits = "-" + digits
	}
	return digits, nil
}
func calculationNumber(value any) (float64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	n, err := number.Float64()
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
func (s *Service) calculate(code string, inputs map[string]any, now time.Time) (calculationResult, error) {
	code = strings.ToLower(api.JavaTrim(code))
	meta, exists := s.calculators[code]
	if !exists {
		return calculationResult{}, api.Failure(404, "CALCULATOR_NOT_FOUND", "No se encontró la calculadora.")
	}
	keys := map[string][]string{
		"imc": {"weightKg", "heightCm"}, "bsa_mosteller": {"weightKg", "heightCm"},
		"tfge": {"creatinineMgDl", "ageYears", "gender"}, "cockcroft": {"weightKg", "ageYears", "creatinineMgDl", "gender"},
		"calcio_corregido": {"serumCalciumMgDl", "serumAlbuminGDl"}, "holliday_segar": {"weightKg"},
		"pediatric_dosage": {"weightKg", "targetDoseMgKg", "doseUnit", "drugConcentrationMg", "drugConcentrationMl", "dosesPerDay"},
		"chads_vasc":       {"ageYears", "isFemale", "congestiveHeartFailure", "hypertension", "diabetes", "strokeOrTia", "vascularDisease"},
	}[code]
	if len(inputs) != len(keys) {
		return calculationResult{}, invalidCalculation()
	}
	numbers := map[string]float64{}
	for _, key := range keys {
		value, present := inputs[key]
		if !present || value == nil {
			return calculationResult{}, invalidCalculation()
		}
		switch key {
		case "gender":
			if value != "FEMALE" && value != "MALE" {
				return calculationResult{}, invalidCalculation()
			}
		case "doseUnit":
			if value != "mg/kg/dia" && value != "mg/kg/dosis" {
				return calculationResult{}, invalidCalculation()
			}
		case "isFemale", "congestiveHeartFailure", "hypertension", "diabetes", "strokeOrTia", "vascularDisease":
			if _, ok := value.(bool); !ok {
				return calculationResult{}, invalidCalculation()
			}
		default:
			n, ok := calculationNumber(value)
			if !ok {
				return calculationResult{}, invalidCalculation()
			}
			if key == "ageYears" {
				minAge := 18.
				if code == "chads_vasc" {
					minAge = 0
				}
				if n < minAge || n > 120 || n != math.Trunc(n) {
					return calculationResult{}, invalidCalculation()
				}
			} else if key == "dosesPerDay" {
				if n < 1 || n > 4 || n != math.Trunc(n) {
					return calculationResult{}, invalidCalculation()
				}
			} else if n <= 0 {
				return calculationResult{}, invalidCalculation()
			}
			numbers[key] = n
		}
	}
	var formattingError error
	fixed := func(n float64, d int) string {
		v, err := calculationFixed(n, d)
		if err != nil {
			formattingError = err
		}
		return v
	}
	text := func(n float64) string {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			formattingError = invalidCalculation()
			return ""
		}
		if n == 0 {
			return "0"
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	output := func(label, value, unit, description string) secondaryOutput {
		o := secondaryOutput{Label: label, Value: value}
		if unit != "" {
			o.Unit = &unit
		}
		if description != "" {
			o.Description = &description
		}
		return o
	}
	n := func(key string) float64 { return numbers[key] }
	date := now.In(s.zone).Format("02/01/2006 15:04")
	if code == "bsa_mosteller" || code == "holliday_segar" || code == "pediatric_dosage" {
		date = now.In(s.zone).Format("02/01/2006 15:04:05")
	}
	result := calculationResult{Code: code, Name: meta.Name, At: api.FormatInstant(now), Outputs: []secondaryOutput{}, Notes: []string{}}
	rangeIndex := 0
	var evolutionArgs []any
	switch code {
	case "imc":
		weight, height := n("weightKg"), n("heightCm")
		meters := height / 100
		result.Raw = weight / (meters * meters)
		surface := fixed(math.Sqrt(weight*height/3600), 2)
		for _, boundary := range []float64{18.5, 25, 30, 35, 40} {
			if result.Raw >= boundary {
				rangeIndex++
			}
		}
		result.Formatted = fixed(result.Raw, 1)
		result.Unit = "kg/m²"
		minimum, maximum := fixed(18.5*(meters*meters), 1), fixed(24.9*(meters*meters), 1)
		result.Outputs = []secondaryOutput{output("Superficie Corporal (BSA)", surface, "m²", "Fórmula de Mosteller para dosificación farmacológica y hemodinamia."), output("Rango de Peso Saludable (OMS)", minimum+" - "+maximum, "kg", "Basado en IMC normal entre 18.5 y 24.9 kg/m² para su talla.")}
		result.Notes = []string{"Talla analizada: " + fixed(meters, 2) + " m.", "En pacientes muy musculosos o deportistas de alto rendimiento, el IMC puede sobreestimar la adiposidad real."}
		evolutionArgs = []any{date, text(weight), text(height), result.Formatted, meta.Ranges[rangeIndex].Label, surface, minimum, maximum, *meta.Ranges[rangeIndex].Action}
	case "cockcroft", "tfge":
		age, creatinine := n("ageYears"), n("creatinineMgDl")
		female := inputs["gender"] == "FEMALE"
		sex := "Masculino"
		if female {
			sex = "Femenino"
		}
		if code == "cockcroft" {
			result.Raw = ((140 - age) * n("weightKg")) / (72 * creatinine)
			if female {
				result.Raw *= 0.85
			}
			result.Formatted = fixed(result.Raw, 1)
			result.Unit = "mL/min"
			for _, boundary := range []float64{90, 60, 30, 15} {
				if result.Raw < boundary {
					rangeIndex++
				}
			}
			factor := "Masculino (factor 1.0)"
			if female {
				factor = "Femenino (factor 0.85)"
			}
			result.Outputs = []secondaryOutput{output("Sexo del paciente", factor, "", ""), output("Uso en Farmacovigilancia", "Ficha técnica / FDA", "", "Cockcroft-Gault es la referencia oficial en la mayoría de prospectos farmacéuticos.")}
			result.Notes = []string{"En pacientes obesos extremos (IMC > 30), Cockcroft-Gault puede sobreestimar el aclaramiento si se utiliza peso total sin ajustar."}
			evolutionArgs = []any{date, text(n("weightKg")), int(age), text(creatinine), sex, result.Formatted, meta.Ranges[rangeIndex].Label, *meta.Ranges[rangeIndex].Action}
		} else {
			k, alpha, factor := 0.9, -0.302, 1.
			if female {
				k, alpha, factor = 0.7, -0.241, 1.012
			}
			ratio := creatinine / k
			result.Raw = 142 * renalPower(math.Min(ratio, 1), alpha) * renalPower(math.Max(ratio, 1), -1.2) * meta.AgeFactors[int(age)] * factor
			result.Formatted = text(math.Floor(result.Raw + 0.5))
			result.Unit = "mL/min/1.73 m²"
			for _, boundary := range []float64{90, 60, 45, 30, 15} {
				if result.Raw < boundary {
					rangeIndex++
				}
			}
			result.Outputs = []secondaryOutput{output("Ecuación estándar", "CKD-EPI 2021", "", "Sin coeficiente de raza (recomendación KDIGO / NKF-ASN)."), output("Creatinina sérica", fixed(creatinine, 2), "mg/dL", "")}
			result.Notes = []string{"Ecuación validada para adultos ≥ 18 años.", "La presencia de Enfermedad Renal Crónica requiere persistencia del daño renal o eGFR < 60 durante ≥ 3 meses."}
			evolutionArgs = []any{date, text(creatinine), int(age), sex, result.Formatted, meta.Ranges[rangeIndex].Label, *meta.Ranges[rangeIndex].Action}
		}
	case "calcio_corregido":
		calcium, albumin := n("serumCalciumMgDl"), n("serumAlbuminGDl")
		result.Raw = calcium + 0.8*(4-albumin)
		switch {
		case result.Raw < 8.5:
			rangeIndex = 0
		case result.Raw <= 10.5:
			rangeIndex = 1
		case result.Raw <= 12:
			rangeIndex = 2
		default:
			rangeIndex = 3
		}
		result.Formatted = fixed(result.Raw, 2)
		result.Unit = "mg/dL"
		result.Outputs = []secondaryOutput{output("Calcio Medido Original", fixed(calcium, 2), "mg/dL", ""), output("Albúmina Medida", fixed(albumin, 2), "g/dL", "")}
		result.Notes = []string{"Ajuste calculado considerando una albúmina de referencia de 4.0 g/dL.", "En pacientes críticos con acidosis o alcalosis severa, se sugiere confirmar con dosaje directo de Calcio Iónico."}
		evolutionArgs = []any{date, text(calcium), text(albumin), result.Formatted, meta.Ranges[rangeIndex].Label, *meta.Ranges[rangeIndex].Action}
	case "bsa_mosteller":
		weight, height := n("weightKg"), n("heightCm")
		result.Formatted = fixed(math.Sqrt(weight*height/3600), 2)
		result.Raw, _ = strconv.ParseFloat(result.Formatted, 64)
		result.Unit = "m²"
		result.Outputs = []secondaryOutput{output("Fórmula aplicada", "Mosteller", "", "√[(Peso × Talla) / 3600]"), output("Peso corporal", fixed(weight, 1), "kg", ""), output("Talla", fixed(height, 1), "cm", "")}
		result.Notes = []string{"Fórmula estándar recomendada por la mayoría de protocolos oncológicos y cardiológicos internacionales."}
		evolutionArgs = []any{date, fixed(weight, 1), fixed(height, 1), result.Formatted}
	case "holliday_segar":
		weight := n("weightKg")
		total := weight * 100
		if weight > 10 {
			total = 1000 + (weight-10)*50
		}
		if weight > 20 {
			total = 1500 + (weight-20)*20
		}
		result.Raw = math.Floor(total + 0.5)
		result.Formatted = text(result.Raw)
		result.Unit = "mL/día"
		hourly, sodium, potassium, fraction := fixed(total/24, 1), fixed((total/100)*3, 1), fixed((total/100)*2, 1), text(math.Floor(total/3+0.5))
		result.Outputs = []secondaryOutput{output("Goteo Horario / Bomba", hourly, "mL/h (microgotas/min)", "Flujo continuo para 24 horas"), output("Fraccionado c/8 hrs", fraction, "mL", "Volumen a infundir en cada turno de enfermería"), output("Sodio sugerido (Na+)", sodium, "mEq/día", "Aporte basal estándar (3 mEq / 100 mL)"), output("Potasio sugerido (K+)", potassium, "mEq/día", "Aporte basal estándar (2 mEq / 100 mL)")}
		result.Notes = []string{"Ajustar según pérdidas extraordinarias (fiebre, diarrea, drenajes o vómitos).", "En pacientes con cardiopatía o nefropatía considerar restricción hídrica al 60-80% del basal."}
		evolutionArgs = []any{date, fixed(weight, 1), result.Formatted, hourly, sodium, potassium, fraction}
	case "pediatric_dosage":
		weight, dose, doses, mg, ml := n("weightKg"), n("targetDoseMgKg"), n("dosesPerDay"), n("drugConcentrationMg"), n("drugConcentrationMl")
		doseUnit := inputs["doseUnit"].(string)
		totalMg := weight * dose
		perDoseMg := totalMg / doses
		if doseUnit == "mg/kg/dosis" {
			perDoseMg = weight * dose
			totalMg = perDoseMg * doses
		}
		perDoseMl := (perDoseMg * ml) / mg
		totalMl := perDoseMl * doses
		interval := text(24 / doses)
		result.Formatted = fixed(perDoseMl, 2)
		result.Raw, _ = strconv.ParseFloat(result.Formatted, 64)
		result.Unit = "mL/dosis"
		result.Outputs = []secondaryOutput{output("Dosis por toma (mg)", fixed(perDoseMg, 1), "mg", "Masa activa en cada administración"), output("Volumen por toma (mL)", result.Formatted, "mL", "Volumen exacto a medir con jeringa dosificadora"), output("Intervalo horario", "Cada "+interval+" horas", "("+text(doses)+" tomas/día)", ""), output("Dosis diaria total", fixed(totalMg, 1), "mg/día", "Equivalente a "+fixed(totalMl, 1)+" mL diarios")}
		result.Notes = []string{"Verificar siempre la dosis máxima absoluta para evitar sobrepasar límites del adulto.", "Recomendar a los cuidadores el uso de jeringa oral graduada en lugar de cucharas domésticas."}
		evolutionArgs = []any{date, fixed(weight, 1), text(dose), doseUnit, text(mg), text(ml), interval, int(doses), fixed(perDoseMg, 1), result.Formatted, fixed(totalMg, 1), fixed(totalMl, 1)}
	case "chads_vasc":
		score := 0
		factors := []string{}
		add := func(condition bool, points int, label string) {
			if condition {
				score += points
				factors = append(factors, label)
			}
		}
		add(inputs["congestiveHeartFailure"].(bool), 1, "Insuficiencia Cardíaca Congestiva (+1)")
		add(inputs["hypertension"].(bool), 1, "Hipertensión Arterial (+1)")
		add(n("ageYears") >= 75, 2, "Edad ≥ 75 años (+2)")
		add(n("ageYears") >= 65 && n("ageYears") < 75, 1, "Edad 65-74 años (+1)")
		add(inputs["diabetes"].(bool), 1, "Diabetes Mellitus (+1)")
		add(inputs["strokeOrTia"].(bool), 2, "Antecedente de ACV / AIT / Tromboembolismo (+2)")
		add(inputs["vascularDisease"].(bool), 1, "Enfermedad Vascular periférica / IAM / Placa aórtica (+1)")
		add(inputs["isFemale"].(bool), 1, "Sexo Femenino (+1)")
		nonSex := score
		if inputs["isFemale"].(bool) {
			nonSex--
		}
		rangeIndex = 2
		if nonSex < 2 {
			rangeIndex = nonSex
		}
		risk := text([]float64{0.2, 0.6, 2.2, 3.2, 4.8, 7.2, 9.7, 11.2, 12.5, 15.2}[score])
		result.Raw = float64(score)
		result.Formatted = strconv.Itoa(score) + " / 9"
		result.Unit = "puntos"
		result.Outputs = []secondaryOutput{output("Riesgo Anual de ACV", risk+"%", "/ año", "Tasa anual ajustada de accidente cerebrovascular isquémico."), output("Puntaje no dependiente de sexo", strconv.Itoa(nonSex), "pts", "Criterio para toma de decisiones según guías ESC.")}
		result.Notes = factors
		identified := strings.Join(factors, ", ")
		if len(factors) == 0 {
			identified = "Ninguno"
			result.Notes = []string{"Sin factores de riesgo adicionales marcados."}
		}
		evolutionArgs = []any{date, score, meta.Ranges[rangeIndex].Label, risk, identified, *meta.Ranges[rangeIndex].Action}
	}
	result.Interpretation = meta.Ranges[rangeIndex]
	if code == "pediatric_dosage" {
		action := "Administrar " + result.Formatted + " mL por vía oral cada " + text(24/n("dosesPerDay")) + " horas."
		result.Interpretation.Action = &action
	}
	result.Action = result.Interpretation.Action
	result.Evolution = fmt.Sprintf(meta.Evolution, evolutionArgs...)
	if formattingError != nil {
		return calculationResult{}, formattingError
	}
	if math.IsNaN(result.Raw) || math.IsInf(result.Raw, 0) || api.JavaLength(api.JavaTrim(result.Formatted+" "+result.Unit)) > 100 {
		return calculationResult{}, invalidCalculation()
	}
	return result, nil
}
