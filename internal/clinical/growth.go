package clinical

import (
	"context"
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

//go:embed who/*.csv
var whoFiles embed.FS

type lms struct{ l, m, s float64 }
type whoTables map[string][]lms

func loadWHO() (whoTables, error) {
	tables := whoTables{}
	for _, metric := range []string{"wfa", "lhfa", "hcfa"} {
		for _, sex := range []string{"male", "female"} {
			key := metric + "_" + sex
			file, err := whoFiles.Open("who/" + key + ".csv")
			if err != nil {
				return nil, err
			}
			records, readErr := csv.NewReader(file).ReadAll()
			closeErr := file.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if len(records) < 2 {
				return nil, fmt.Errorf("WHO table %s is empty", key)
			}
			entries := []lms{}
			for _, row := range records[1:] {
				if len(row) < 4 {
					return nil, fmt.Errorf("WHO table %s has an invalid row", key)
				}
				values := make([]float64, 3)
				for i := range values {
					values[i], err = strconv.ParseFloat(row[i+1], 64)
					if err != nil {
						return nil, err
					}
				}
				entries = append(entries, lms{values[0], values[1], values[2]})
			}
			tables[key] = entries
		}
	}
	return tables, nil
}

type whoEvaluation struct {
	Z              float64 `json:"zScore"`
	Percentile     float64 `json:"percentile"`
	Interpretation string  `json:"interpretation"`
}
type growthEvaluations struct {
	Weight *whoEvaluation `json:"weight"`
	Height *whoEvaluation `json:"height"`
	Head   *whoEvaluation `json:"headCircumference"`
}
type growthMeasurement struct {
	Source       string             `json:"source"`
	EncounterID  *int64             `json:"encounterId"`
	Date         string             `json:"date"`
	Months       float64            `json:"ageMonths"`
	Age          string             `json:"ageFormatted"`
	Measurements Measurements       `json:"measurements"`
	Evaluations  *growthEvaluations `json:"evaluations"`
}

func javaRound(value float64) float64 {
	if math.IsNaN(value) {
		return 0
	}
	if value >= float64(math.MaxInt64) {
		return float64(math.MaxInt64)
	}
	if value <= float64(math.MinInt64) {
		return float64(math.MinInt64)
	}
	return math.Floor(value + 0.5)
}
func (tables whoTables) evaluate(metric, sex string, days int64, value *float64) *whoEvaluation {
	if value == nil || *value <= 0 {
		return nil
	}
	gender := "male"
	if sex == "FEMALE" {
		gender = "female"
	}
	table := tables[metric+"_"+gender]
	index := days
	if index < 0 {
		index = 0
	}
	if index >= int64(len(table)) {
		index = int64(len(table) - 1)
	}
	entry := table[index]
	z := 0.0
	if entry.m > 0 && entry.s > 0 {
		if math.Abs(entry.l) < 1e-7 {
			z = math.Log(*value/entry.m) / entry.s
		} else {
			z = (math.Pow(*value/entry.m, entry.l) - 1) / (entry.l * entry.s)
		}
	}
	sign := 1.0
	if z < 0 {
		sign = -1
	}
	a := math.Abs(z) / math.Sqrt(2)
	t := 1 / (1 + 0.3275911*a)
	y := 1 - (((((1.061405429*t-1.453152027)*t+1.421413741)*t-0.284496736)*t + 0.254829592) * t * math.Exp(-a*a))
	percentile := math.Max(0.1, math.Min(99.9, 50*(1+sign*y)))
	label := "Adecuado"
	switch metric {
	case "wfa":
		switch {
		case z < -3:
			label = "Desnutrición severa"
		case z < -2:
			label = "Bajo peso"
		case z > 3:
			label = "Obesidad"
		case z > 2:
			label = "Sobrepeso"
		case z > 1:
			label = "Riesgo de sobrepeso"
		}
	case "lhfa":
		label = "Adecuada"
		switch {
		case z < -3:
			label = "Talla baja severa"
		case z < -2:
			label = "Talla baja"
		case z > 3:
			label = "Talla muy alta"
		case z > 2:
			label = "Talla alta"
		}
	case "hcfa":
		if z < -2 {
			label = "Microcefalia"
		} else if z > 2 {
			label = "Macrocefalia"
		}
	}
	return &whoEvaluation{javaRound(z*100) / 100, javaRound(percentile*10) / 10, label}
}
func (tables whoTables) evaluations(sex string, days int64, m Measurements) *growthEvaluations {
	result := growthEvaluations{tables.evaluate("wfa", sex, days, m.Weight), tables.evaluate("lhfa", sex, days, m.Height), tables.evaluate("hcfa", sex, days, m.Head)}
	if result.Weight == nil && result.Height == nil && result.Head == nil {
		return nil
	}
	return &result
}
func javaPlusMonths(date time.Time, months int) time.Time {
	first := time.Date(date.Year(), date.Month()+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	day := date.Day()
	last := first.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return first.AddDate(0, 0, day-1)
}
func pediatricAge(birth, day time.Time) string {
	if day.Before(birth) {
		return "0 días"
	}
	if day.Equal(birth) {
		return "Recién nacido"
	}
	months := (day.Year()-birth.Year())*12 + int(day.Month()) - int(birth.Month())
	days := day.Day() - birth.Day()
	if months > 0 && days < 0 {
		months--
		days = int(day.Sub(javaPlusMonths(birth, months)).Hours() / 24)
	}
	years, months := months/12, months%12
	monthLabel := fmt.Sprintf("%d meses", months)
	if months == 1 {
		monthLabel = "1 mes"
	}
	dayLabel := fmt.Sprintf("%d días", days)
	if days == 1 {
		dayLabel = "1 día"
	}
	if years > 0 {
		label := fmt.Sprintf("%d años", years)
		if years == 1 {
			label = "1 año"
		}
		if months > 0 {
			label += ", " + monthLabel
		}
		return label
	}
	if months > 0 {
		if days > 0 {
			return monthLabel + ", " + dayLabel
		}
		return monthLabel
	}
	return dayLabel
}
func (s *Service) GrowthMeasurements(ctx context.Context, user string, id int64) ([]growthMeasurement, error) {
	result := []growthMeasurement{}
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		p, err := ownedPatient(ctx, tx, user, id)
		if err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, user, "GROWTH"); err != nil {
			return err
		}
		birth, err := time.Parse("2006-01-02", p.BirthDate)
		if err != nil {
			return err
		}
		sex := ""
		if p.Gender != nil {
			sex = *p.Gender
		}
		if len(p.Personal) > 0 && string(p.Personal) != "null" {
			var personal struct {
				Birth *Measurements `json:"birthMeasurements"`
			}
			if err := json.Unmarshal(p.Personal, &personal); err != nil {
				return err
			}
			if personal.Birth != nil && (personal.Birth.Weight != nil || personal.Birth.Height != nil || personal.Birth.Head != nil) {
				result = append(result, growthMeasurement{Source: "BIRTH", Date: p.BirthDate, Age: "Al nacer", Measurements: *personal.Birth, Evaluations: s.growth.evaluations(sex, 0, *personal.Birth)})
			}
		}
		rows, err := tx.Query(ctx, `SELECT id,encounter_date,weight_kg,height_cm,head_circumference_cm FROM clinical_encounters WHERE patient_id=$1 AND encounter_date<$2 AND (encounter_date AT TIME ZONE 'America/Argentina/Buenos_Aires')::date >= $3::date AND (weight_kg IS NOT NULL OR height_cm IS NOT NULL OR head_circumference_cm IS NOT NULL) ORDER BY encounter_date ASC,id ASC`, id, time.Now().UTC(), p.BirthDate)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var encounterID int64
			var instant time.Time
			var m Measurements
			if err := rows.Scan(&encounterID, &instant, &m.Weight, &m.Height, &m.Head); err != nil {
				return err
			}
			date := instant.In(s.zone).Format("2006-01-02")
			day, err := time.Parse("2006-01-02", date)
			if err != nil {
				return err
			}
			days := int64(day.Sub(birth).Hours() / 24)
			result = append(result, growthMeasurement{Source: "ENCOUNTER", EncounterID: &encounterID, Date: date, Months: javaRound(float64(days)/30.4375*10) / 10, Age: pediatricAge(birth, day), Measurements: m, Evaluations: s.growth.evaluations(sex, days, m)})
		}
		return rows.Err()
	})
	return result, err
}
func (s *Service) GrowthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/doctorchart/patients/{id}/measurements", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		var result []growthMeasurement
		if err == nil {
			result, err = s.GrowthMeasurements(r.Context(), api.User(r.Context()).UserID, id)
		}
		clinicalReply(w, result, err, 200)
	})
}
