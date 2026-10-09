package wizard

import (
	"encoding/json"
	"math"
)

type FormulaResult struct {
	Value  *float64 `json:"value"`
	Status string   `json:"status"`
}

func unavailable(status string) FormulaResult { return FormulaResult{Status: status} }
func number(value any) (float64, bool) {
	switch value := value.(type) {
	case json.Number:
		n, err := value.Float64()
		return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
	case float64:
		return value, !math.IsNaN(value) && !math.IsInf(value, 0)
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	}
	return 0, false
}
func Calculate(kind, operation string, inputs map[string]any) FormulaResult {
	var keys []string
	switch kind {
	case "IMC", "BSA":
		keys = []string{"weightKg", "heightCm"}
	case "TFGE":
		keys = []string{"creatinineMgDl", "ageYears", "gender"}
	case "COCKCROFT":
		keys = []string{"creatinineMgDl", "weightKg", "ageYears", "gender"}
	case "CALCIO_CORREGIDO":
		keys = []string{"serumCalciumMgDl", "serumAlbuminGDl"}
	case "CUSTOM":
		keys = []string{"a", "b"}
	default:
		return unavailable("INVALID_INPUT")
	}
	if inputs == nil {
		return unavailable("INVALID_INPUT")
	}
	nums := map[string]float64{}
	for _, key := range keys {
		operand := inputs[key]
		if operand == nil {
			return unavailable("MISSING_INPUT")
		}
		if key == "gender" {
			gender, ok := operand.(string)
			if !ok {
				return unavailable("INVALID_INPUT")
			}
			if gender == "OTHER" || gender == "UNKNOWN" {
				return unavailable("NOT_APPLICABLE")
			}
			if gender != "MALE" && gender != "FEMALE" {
				return unavailable("INVALID_INPUT")
			}
		} else {
			n, ok := number(operand)
			if !ok {
				return unavailable("INVALID_INPUT")
			}
			nums[key] = n
		}
	}
	var value float64
	switch kind {
	case "IMC", "BSA":
		weight, height := nums["weightKg"], nums["heightCm"]
		if weight <= 0 || height <= 0 {
			return unavailable("INVALID_INPUT")
		}
		meters := height / 100
		value = weight / (meters * meters)
		if kind == "BSA" {
			value = math.Sqrt(weight * height / 3600)
		}
	case "TFGE", "COCKCROFT":
		creatinine, age := nums["creatinineMgDl"], nums["ageYears"]
		if creatinine <= 0 || age < 0 {
			return unavailable("INVALID_INPUT")
		}
		if age < 18 {
			return unavailable("NOT_APPLICABLE")
		}
		female := inputs["gender"] == "FEMALE"
		if kind == "TFGE" {
			k, alpha, factor := .9, -.302, 1.0
			if female {
				k, alpha, factor = .7, -.241, 1.012
			}
			ratio := creatinine / k
			value = 142 * math.Pow(math.Min(ratio, 1), alpha) * math.Pow(math.Max(ratio, 1), -1.2) * math.Pow(.9938, age) * factor
		} else {
			weight := nums["weightKg"]
			if weight <= 0 {
				return unavailable("INVALID_INPUT")
			}
			value = ((140 - age) * weight) / (72 * creatinine)
			if female {
				value *= .85
			}
		}
	case "CALCIO_CORREGIDO":
		calcium, albumin := nums["serumCalciumMgDl"], nums["serumAlbuminGDl"]
		if calcium <= 0 || albumin <= 0 {
			return unavailable("INVALID_INPUT")
		}
		value = calcium + .8*(4-albumin)
	case "CUSTOM":
		a, b := nums["a"], nums["b"]
		switch operation {
		case "ADD":
			value = a + b
		case "SUBTRACT":
			value = a - b
		case "MULTIPLY":
			value = a * b
		case "DIVIDE":
			if b == 0 {
				return unavailable("DIVISION_BY_ZERO")
			}
			value = a / b
		case "AVERAGE":
			value = (a + b) / 2
		default:
			return unavailable("INVALID_INPUT")
		}
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return unavailable("INVALID_INPUT")
	}
	return FormulaResult{&value, "READY"}
}
