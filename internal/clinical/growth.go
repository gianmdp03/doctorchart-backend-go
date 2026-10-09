package clinical

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/growth"
)

type growthEvaluations struct {
	Weight *growth.Evaluation `json:"weight"`
	Height *growth.Evaluation `json:"height"`
	Head   *growth.Evaluation `json:"headCircumference"`
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
type patientGrowth struct {
	PatientID    int64                 `json:"patientId"`
	BirthDate    string                `json:"birthDate"`
	AsOfDate     string                `json:"asOfDate"`
	AgeDays      int                   `json:"ageDays"`
	AgeFormatted string                `json:"ageFormatted"`
	ReferenceSex string                `json:"referenceSex"`
	SexSource    string                `json:"sexSource"`
	Status       string                `json:"status"`
	Observations []growth.Observation  `json:"observations"`
	Charts       []growth.PatientChart `json:"charts"`
}
type growthOptions struct {
	sex   string
	modes map[string]string
}

func growthReferenceSex(p Patient, confirmation string) (string, string, error) {
	if confirmation != "" {
		return confirmation, "VIEW_CONFIRMATION", nil
	}
	// The system's canonical sys_sex field is labelled Sexo and is validated as
	// SELECT MALE/FEMALE/OTHER/UNKNOWN. Arbitrary gender projections are insufficient.
	var values map[string]any
	if len(p.Values) > 0 && string(p.Values) != "null" {
		if err := json.Unmarshal(p.Values, &values); err != nil {
			return "", "", err
		}
	}
	sex, _ := values["sys_sex"].(string)
	if p.Gender != nil && *p.Gender == sex && (sex == "MALE" || sex == "FEMALE") {
		return sex, "SYSTEM_SEX", nil
	}
	return "", "", nil
}

func parseGrowthOptions(r *http.Request) (growthOptions, error) {
	options := growthOptions{sex: r.URL.Query().Get("referenceSex"), modes: map[string]string{}}
	invalid := func() error {
		return &api.Error{Status: 400, Code: "INVALID_GROWTH_REFERENCE", Message: "Confirmá una referencia y posiciones de medición válidas."}
	}
	if options.sex != "" && options.sex != "MALE" && options.sex != "FEMALE" {
		return options, invalid()
	}
	raw := r.URL.Query().Get("measurementModes")
	if raw != "" {
		if len(raw) > 16384 || json.Unmarshal([]byte(raw), &options.modes) != nil || len(options.modes) > 500 {
			return options, invalid()
		}
		for key, mode := range options.modes {
			if mode != "RECUMBENT" && mode != "STANDING" {
				return options, invalid()
			}
			if key != "BIRTH" {
				id, err := strconv.ParseInt(key, 10, 64)
				if err != nil || id <= 0 {
					return options, invalid()
				}
			}
		}
	}
	return options, nil
}

func (s *Service) PatientGrowth(ctx context.Context, user string, id int64, options growthOptions) (patientGrowth, error) {
	result := patientGrowth{PatientID: id, Observations: []growth.Observation{}, Charts: []growth.PatientChart{}, Status: "READY"}
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		p, err := ownedPatient(ctx, tx, user, id)
		if err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, user, "GROWTH"); err != nil {
			return err
		}
		now := time.Now()
		result.BirthDate = p.BirthDate
		result.AsOfDate = now.In(s.zone).Format("2006-01-02")
		birth, err := time.Parse("2006-01-02", p.BirthDate)
		if err != nil || p.BirthDate > result.AsOfDate {
			result.Status = "INVALID_BIRTH_DATE"
			return nil
		}
		today, err := time.Parse("2006-01-02", result.AsOfDate)
		if err != nil {
			return err
		}
		result.AgeDays = int(today.Sub(birth).Hours() / 24)
		result.AgeFormatted = pediatricAge(birth, today)
		result.ReferenceSex, result.SexSource, err = growthReferenceSex(p, options.sex)
		if err != nil {
			return err
		}
		if result.ReferenceSex == "" {
			result.Status = "REFERENCE_SEX_REQUIRED"
		}
		appendObservation := func(source string, encounterID *int64, date string, m Measurements, mode string) error {
			day, err := time.Parse("2006-01-02", date)
			if err != nil {
				return err
			}
			if day.Before(birth) || date > result.AsOfDate {
				return nil
			}
			key := "BIRTH"
			if encounterID != nil {
				key = strconv.FormatInt(*encounterID, 10)
			}
			if confirmed, ok := options.modes[key]; ok {
				mode = confirmed
			}
			if mode != "RECUMBENT" && mode != "STANDING" {
				mode = "UNKNOWN"
			}
			result.Observations = append(result.Observations, growth.Observation{Source: source, EncounterID: encounterID, Date: date, AgeDays: int(day.Sub(birth).Hours() / 24), AgeFormatted: pediatricAge(birth, day), Weight: m.Weight, Height: m.Height, Head: m.Head, Mode: mode})
			return nil
		}
		if len(p.Personal) > 0 && string(p.Personal) != "null" {
			var personal struct {
				Birth *Measurements `json:"birthMeasurements"`
			}
			if err := json.Unmarshal(p.Personal, &personal); err != nil {
				return err
			}
			if personal.Birth != nil && (personal.Birth.Weight != nil || personal.Birth.Height != nil || personal.Birth.Head != nil) {
				if err := appendObservation("BIRTH", nil, p.BirthDate, *personal.Birth, ""); err != nil {
					return err
				}
			}
		}
		rows, err := tx.Query(ctx, `SELECT id,encounter_date,weight_kg,height_cm,head_circumference_cm,form_values FROM clinical_encounters WHERE patient_id=$1 AND encounter_date<=$2 AND (encounter_date AT TIME ZONE 'America/Argentina/Buenos_Aires')::date >= $3::date AND (weight_kg IS NOT NULL OR height_cm IS NOT NULL OR head_circumference_cm IS NOT NULL) ORDER BY encounter_date ASC,id ASC`, id, now.UTC(), p.BirthDate)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var encounterID int64
			var instant time.Time
			var m Measurements
			var raw json.RawMessage
			if err := rows.Scan(&encounterID, &instant, &m.Weight, &m.Height, &m.Head, &raw); err != nil {
				return err
			}
			var values map[string]any
			if len(raw) > 0 && string(raw) != "null" {
				if err := json.Unmarshal(raw, &values); err != nil {
					return err
				}
			}
			// A wizard SELECT field named heightMeasurementMode records the position
			// in the existing form_values storage, without changing old snapshots/schema.
			mode, _ := values["heightMeasurementMode"].(string)
			if err := appendObservation("ENCOUNTER", &encounterID, instant.In(s.zone).Format("2006-01-02"), m, mode); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		result.Charts = s.growth.PatientCharts(result.ReferenceSex, result.AgeDays, result.Observations)
		return nil
	})
	return result, err
}

// Preserve the historical measurements route for other clients. Scientific
// evaluations now use the same domain service; neither age nor sex is clamped.
func (s *Service) GrowthMeasurements(ctx context.Context, user string, id int64) ([]growthMeasurement, error) {
	response, err := s.PatientGrowth(ctx, user, id, growthOptions{})
	if err != nil {
		return nil, err
	}
	result := []growthMeasurement{}
	for _, o := range response.Observations {
		evaluations := &growthEvaluations{}
		for _, d := range growth.Catalog() {
			if !d.Applicable(response.ReferenceSex, o.AgeDays) {
				continue
			}
			point := s.growth.Point(d, o)
			if point == nil {
				continue
			}
			switch d.Indicator {
			case "WEIGHT":
				evaluations.Weight = point.Evaluation
			case "HEIGHT", "LENGTH":
				evaluations.Height = point.Evaluation
			case "HEAD_CIRCUMFERENCE":
				evaluations.Head = point.Evaluation
			}
		}
		result = append(result, growthMeasurement{Source: o.Source, EncounterID: o.EncounterID, Date: o.Date, Months: float64(o.AgeDays) / growth.DaysPerMonth, Age: o.AgeFormatted, Measurements: Measurements{Weight: o.Weight, Height: o.Height, Head: o.Head}, Evaluations: evaluations})
	}
	return result, nil
}

func (s *Service) GrowthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/doctorchart/patients/{id}/growth", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		var result patientGrowth
		var options growthOptions
		if err == nil {
			options, err = parseGrowthOptions(r)
		}
		if err == nil {
			result, err = s.PatientGrowth(r.Context(), api.User(r.Context()).UserID, id, options)
		}
		clinicalReply(w, result, err, 200)
	})
	mux.HandleFunc("GET /api/doctorchart/patients/{id}/measurements", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		var result []growthMeasurement
		if err == nil {
			result, err = s.GrowthMeasurements(r.Context(), api.User(r.Context()).UserID, id)
		}
		clinicalReply(w, result, err, 200)
	})
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
