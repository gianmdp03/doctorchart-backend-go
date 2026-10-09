// Package growth implements the WHO 2006 standards and WHO 2007 reference.
// Scientific coordinates and values are independent of the PDF's plotting frame.
package growth

type Axis struct {
	Variable  string  `json:"variable"`
	Unit      string  `json:"unit"`
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	Step      float64 `json:"step"`
	MinorStep float64 `json:"minorStep"`
}

type Definition struct {
	ID              string  `json:"id"`
	PDFNumber       int     `json:"pdfNumber"`
	PDFPage         int     `json:"pdfPage"`
	Indicator       string  `json:"indicator"`
	Title           string  `json:"title"`
	Sex             string  `json:"sex"`
	AgeMinMonths    float64 `json:"ageMinMonths"`
	AgeMaxMonths    float64 `json:"ageMaxMonths"`
	Reference       string  `json:"reference"`
	Dataset         string  `json:"dataset"`
	MeasurementMode string  `json:"measurementMode"`
	X               Axis    `json:"x"`
	Y               Axis    `json:"y"`
	Percentiles     []int   `json:"percentiles"`
	Source          string  `json:"source"`
}

// Catalog is exactly the 20 AEPap sheets. Additional scientific coverage is not
// silently added (in particular, girls' BMI between 2 and 5 years is absent).
func Catalog() []Definition {
	result := []Definition{}
	add := func(page int, indicator, title, sex, dataset, mode string, amin, amax float64, x, y Axis) {
		version := "WHO 2006"
		if dataset[len(dataset)-2:] == "07" {
			version = "WHO 2007"
		}
		key := dataset + "_girls"
		if sex == "MALE" {
			key = dataset + "_boys"
		}
		result = append(result, Definition{ID: key, PDFNumber: len(result) + 1, PDFPage: page, Indicator: indicator, Title: title, Sex: sex, AgeMinMonths: amin, AgeMaxMonths: amax, Reference: version, Dataset: key, MeasurementMode: mode, X: x, Y: y, Percentiles: []int{3, 15, 50, 85, 97}, Source: sourcePage(dataset)})
	}
	age := func(min, max, step float64) Axis {
		minor := 1.0
		if max == 228 {
			minor = 3 // The school-age PDF grids use quarters of a year.
		}
		return Axis{"AGE", "meses", min, max, step, minor}
	}
	value := func(variable, unit string, min, max, step, minor float64) Axis {
		return Axis{variable, unit, min, max, step, minor}
	}
	add(1, "WEIGHT", "Peso para la edad", "FEMALE", "wfa06", "", 0, 60, age(0, 60, 2), value("WEIGHT", "kg", 1, 25, 2, 1))
	add(2, "WEIGHT", "Peso para la edad", "MALE", "wfa06", "", 0, 60, age(0, 60, 2), value("WEIGHT", "kg", 1, 25, 2, 1))
	add(2, "LENGTH", "Longitud para la edad", "FEMALE", "lhfa06", "RECUMBENT", 0, 24, age(0, 24, 1), value("LENGTH", "cm", 45, 95, 5, 1))
	add(3, "LENGTH", "Longitud para la edad", "MALE", "lhfa06", "RECUMBENT", 0, 24, age(0, 24, 1), value("LENGTH", "cm", 45, 95, 5, 1))
	// Split charts share a scientific dataset but have independent identifiers.
	result[2].ID = "lfa06_girls"
	result[3].ID = "lfa06_boys"
	add(3, "HEIGHT", "Estatura para la edad", "FEMALE", "lhfa06", "STANDING", 24, 60, age(24, 60, 2), value("HEIGHT", "cm", 77, 120, 5, 1))
	add(4, "HEIGHT", "Estatura para la edad", "MALE", "lhfa06", "STANDING", 24, 60, age(24, 60, 2), value("HEIGHT", "cm", 80, 120, 5, 1))
	result[4].ID = "hfa06_girls"
	result[5].ID = "hfa06_boys"
	add(4, "HEAD_CIRCUMFERENCE", "Perímetro cefálico para la edad", "MALE", "hcfa06", "", 0, 60, age(0, 60, 2), value("HEAD_CIRCUMFERENCE", "cm", 32, 54.5, 2, .5))
	add(5, "HEAD_CIRCUMFERENCE", "Perímetro cefálico para la edad", "FEMALE", "hcfa06", "", 0, 60, age(0, 60, 2), value("HEAD_CIRCUMFERENCE", "cm", 31, 53.5, 2, .5))
	add(5, "WEIGHT", "Peso para la edad", "FEMALE", "wfa07", "", 60, 120, age(60, 120, 3), value("WEIGHT", "kg", 13, 47, 5, 1))
	add(6, "WEIGHT", "Peso para la edad", "MALE", "wfa07", "", 60, 120, age(60, 120, 3), value("WEIGHT", "kg", 13, 45, 5, 1))
	add(6, "HEIGHT", "Estatura para la edad", "FEMALE", "hfa07", "STANDING", 60, 228, age(60, 228, 3), value("HEIGHT", "cm", 95, 180, 10, 5))
	add(7, "HEIGHT", "Estatura para la edad", "MALE", "hfa07", "STANDING", 60, 228, age(60, 228, 3), value("HEIGHT", "cm", 100, 195, 10, 5))
	add(7, "WEIGHT_LENGTH", "Peso para la longitud", "FEMALE", "wfl06", "RECUMBENT", 0, 24, value("LENGTH", "cm", 45, 110, 5, 1), value("WEIGHT", "kg", 1, 23, 2, .5))
	add(8, "WEIGHT_LENGTH", "Peso para la longitud", "MALE", "wfl06", "RECUMBENT", 0, 24, value("LENGTH", "cm", 45, 110, 5, 1), value("WEIGHT", "kg", 1, 23, 2, .5))
	add(8, "WEIGHT_HEIGHT", "Peso para la estatura", "FEMALE", "wfh06", "STANDING", 24, 60, value("HEIGHT", "cm", 65, 120, 5, 1), value("WEIGHT", "kg", 5, 29, 2, .5))
	add(9, "WEIGHT_HEIGHT", "Peso para la estatura", "MALE", "wfh06", "STANDING", 24, 60, value("HEIGHT", "cm", 65, 120, 5, 1), value("WEIGHT", "kg", 5, 28, 2, .5))
	add(9, "BMI", "IMC para la edad", "FEMALE", "bfa06", "AGE_APPROPRIATE", 0, 24, age(0, 24, 1), value("BMI", "kg/m²", 9.6, 21.4, 1, .2))
	add(10, "BMI", "IMC para la edad", "MALE", "bfa06", "AGE_APPROPRIATE", 0, 60, age(0, 60, 2), value("BMI", "kg/m²", 9.6, 21.4, 1, .2))
	add(10, "BMI", "IMC para la edad", "FEMALE", "bfa07", "STANDING", 60, 228, age(60, 228, 3), value("BMI", "kg/m²", 12, 29.5, 2, .5))
	add(11, "BMI", "IMC para la edad", "MALE", "bfa07", "STANDING", 60, 228, age(60, 228, 3), value("BMI", "kg/m²", 12, 30, 2, .5))
	return result
}

func sourcePage(dataset string) string {
	base := "https://www.who.int/tools/child-growth-standards/standards/"
	switch dataset {
	case "wfa06":
		return base + "weight-for-age"
	case "lhfa06":
		return base + "length-height-for-age"
	case "hcfa06":
		return base + "head-circumference-for-age"
	case "wfl06", "wfh06":
		return base + "weight-for-length-height"
	case "bfa06":
		return "https://www.who.int/toolkits/child-growth-standards/standards/body-mass-index-for-age-bmi-for-age"
	case "hfa07":
		return "https://www.who.int/tools/growth-reference-data-for-5to19-years/indicators/height-for-age"
	case "bfa07":
		return "https://www.who.int/tools/growth-reference-data-for-5to19-years/indicators/bmi-for-age"
	default:
		return "https://www.who.int/tools/growth-reference-data-for-5to19-years/indicators/weight-for-age-5to10-years"
	}
}

const DaysPerMonth = 30.4375

func (d Definition) Applicable(sex string, days int) bool {
	if d.Sex != sex || days < 0 {
		return false
	}
	months := float64(days) / DaysPerMonth
	if d.Reference == "WHO 2007" {
		return days > 1856 && months >= 61 && months <= d.AgeMaxMonths
	}
	if days > 1856 {
		return false
	}
	if d.AgeMinMonths == 24 {
		return days >= 731
	}
	if d.AgeMaxMonths == 24 {
		return days <= 730
	}
	return true
}
