package clinical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

const calendarID = "argentina-2026-v1"

type ageRule struct {
	From  int `json:"fromMonths"`
	To    int `json:"toMonths"`
	Grace int `json:"graceMonths"`
}
type application struct {
	ID   string   `json:"id"`
	Dose string   `json:"dosis"`
	Rule *ageRule `json:"ageRule"`
}
type vaccine struct {
	ID           string        `json:"id"`
	Name         string        `json:"nombre"`
	Applications []application `json:"aplicaciones"`
}
type calendar struct {
	ID       string    `json:"calendarId"`
	Vaccines []vaccine `json:"vacunas"`
}
type Dose struct {
	ID          string  `json:"id"`
	Calendar    string  `json:"calendarId"`
	VaccineID   string  `json:"vaccineId"`
	VaccineName string  `json:"vaccineName"`
	Label       string  `json:"doseLabel"`
	From        int     `json:"dueFrom"`
	To          int     `json:"dueTo"`
	Age         string  `json:"dueAgeLabel"`
	Status      string  `json:"status"`
	Applied     *string `json:"appliedAt"`
}
type Schedule struct {
	PatientID  int64  `json:"patientId"`
	Version    int64  `json:"patientVersion"`
	CalendarID string `json:"calendarId"`
	AsOf       string `json:"asOfDate"`
	Months     int    `json:"ageInMonths"`
	Age        string `json:"ageLabel"`
	Doses      []Dose `json:"doses"`
}
type vaccination struct {
	ID       int64
	DoseID   string
	Applied  string
	Calendar calendar
}

func vaccinationError(message string) error {
	return api.Failure(400, "INVALID_VACCINATION_REQUEST", message)
}
func doseMissing() error {
	return api.Failure(404, "VACCINATION_DOSE_NOT_FOUND", "No se encontró la dosis del calendario.")
}
func loadCalendar(ctx context.Context, tx pgx.Tx) (calendar, error) {
	var result calendar
	var raw []byte
	err := tx.QueryRow(ctx, "SELECT definition FROM vaccination_calendars WHERE id=$1", calendarID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, api.Failure(503, "VACCINATION_CALENDAR_UNAVAILABLE", "No se pudo cargar el calendario de vacunación.")
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}
func loadVaccinations(ctx context.Context, tx pgx.Tx, patientID int64) ([]vaccination, error) {
	result := []vaccination{}
	rows, err := tx.Query(ctx, `SELECT v.id,v.dose_id,v.applied_at::text,c.definition FROM patient_vaccinations v JOIN vaccination_doses d ON d.id=v.dose_id JOIN vaccination_calendars c ON c.id=d.calendar_id WHERE v.patient_id=$1`, patientID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var row vaccination
		var raw []byte
		if err := rows.Scan(&row.ID, &row.DoseID, &row.Applied, &raw); err != nil {
			return result, err
		}
		if err := json.Unmarshal(raw, &row.Calendar); err != nil {
			return result, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
func monthLabel(months int) string {
	if months < 24 {
		return strconv.Itoa(months) + " meses"
	}
	return strconv.Itoa(months/12) + " años"
}
func projectDose(cal calendar, vaccine vaccine, app application, months int, applied *string) Dose {
	rule := app.Rule
	status := "ACTUAL"
	if applied != nil {
		status = "APLICADA"
	} else if months < rule.From {
		status = "FUTURA"
	} else if months > rule.To+rule.Grace {
		status = "PENDIENTE"
	}
	return Dose{app.ID, cal.ID, vaccine.ID, vaccine.Name, app.Dose, rule.From, rule.To, monthLabel(rule.From), status, applied}
}
func (s *Service) schedule(ctx context.Context, tx pgx.Tx, p Patient, cal calendar) (Schedule, error) {
	todayText := time.Now().In(s.viewZone(ctx)).Format("2006-01-02")
	today, err := time.Parse("2006-01-02", todayText)
	if err != nil {
		return Schedule{}, err
	}
	birth, err := time.Parse("2006-01-02", p.BirthDate)
	if err != nil || birth.After(today) {
		return Schedule{}, vaccinationError("El calendario requiere una fecha de nacimiento válida.")
	}
	months := ageMonths(birth, today)
	records, err := loadVaccinations(ctx, tx, p.ID)
	if err != nil {
		return Schedule{}, err
	}
	applied := map[string]string{}
	for _, record := range records {
		applied[record.DoseID] = record.Applied
	}
	doses := []Dose{}
	for _, vaccine := range cal.Vaccines {
		for _, app := range vaccine.Applications {
			if app.Rule == nil {
				continue
			}
			var date *string
			if value, exists := applied[app.ID]; exists {
				date = &value
			}
			doses = append(doses, projectDose(cal, vaccine, app, *months, date))
		}
	}
	for _, record := range records {
		if record.Calendar.ID == cal.ID {
			continue
		}
		for _, vaccine := range record.Calendar.Vaccines {
			for _, app := range vaccine.Applications {
				if app.ID == record.DoseID && app.Rule != nil {
					date := record.Applied
					doses = append(doses, projectDose(record.Calendar, vaccine, app, *months, &date))
				}
			}
		}
	}
	rank := map[string]int{"APLICADA": 0, "PENDIENTE": 1, "ACTUAL": 2, "FUTURA": 3}
	sort.SliceStable(doses, func(i, j int) bool {
		a, b := doses[i], doses[j]
		if a.Status != b.Status {
			return rank[a.Status] < rank[b.Status]
		}
		if a.Status == "APLICADA" {
			return false
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	return Schedule{p.ID, p.Version, cal.ID, todayText, *months, monthLabel(*months), doses}, nil
}
func (s *Service) Vaccinations(ctx context.Context, userID string, patientID int64) (Schedule, error) {
	var result Schedule
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		p, err := ownedPatient(ctx, tx, userID, patientID)
		if err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, userID, "PATIENT_VACCINATION"); err != nil {
			return err
		}
		cal, err := loadCalendar(ctx, tx)
		if err != nil {
			return err
		}
		result, err = s.schedule(ctx, tx, p, cal)
		return err
	})
	return result, err
}

type SavedVaccination struct {
	Patient  Patient  `json:"patient"`
	Schedule Schedule `json:"schedule"`
}

func (s *Service) SaveVaccination(ctx context.Context, userID string, patientID int64, doseID string, request map[string]any) (SavedVaccination, error) {
	var result SavedVaccination
	if err := api.Only(request, "expectedVersion", "calendarId", "applied", "appliedAt"); err != nil {
		return result, err
	}
	err := s.transaction(ctx, false, func(tx pgx.Tx) error {
		p, err := ownedPatient(ctx, tx, userID, patientID)
		if err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, userID, "PATIENT_VACCINATION"); err != nil {
			return err
		}
		expected, err := api.Version(request, false)
		if err != nil {
			return err
		}
		if p.Version != *expected {
			return api.Failure(409, "VERSION_CONFLICT", "El registro cambió. Actualizá los datos antes de guardar.")
		}
		cal, err := loadCalendar(ctx, tx)
		if err != nil {
			return err
		}
		requested, err := api.Text(request, "calendarId", 80)
		if err != nil {
			return err
		}
		applied, ok := request["applied"].(bool)
		if !ok {
			return vaccinationError("La aplicación requiere un estado válido.")
		}
		var origin calendar
		var raw []byte
		err = tx.QueryRow(ctx, "SELECT c.definition FROM vaccination_doses d JOIN vaccination_calendars c ON c.id=d.calendar_id WHERE d.id=$1", doseID).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return doseMissing()
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &origin); err != nil {
			return err
		}
		records, err := loadVaccinations(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		var existing *vaccination
		for i := range records {
			if records[i].DoseID == doseID {
				existing = &records[i]
				break
			}
		}
		if origin.ID != requested || origin.ID != cal.ID && existing == nil {
			return api.Failure(409, "VACCINATION_CALENDAR_CHANGED", "Actualizá el calendario antes de guardar.")
		}
		validDose := false
		for _, vaccine := range origin.Vaccines {
			for _, app := range vaccine.Applications {
				if app.ID == doseID && app.Rule != nil {
					validDose = true
				}
			}
		}
		if !validDose {
			return doseMissing()
		}
		date, err := api.OptionalText(request, "appliedAt")
		if err != nil {
			return err
		}
		if !applied && date != nil {
			return vaccinationError("Una dosis no aplicada no puede tener fecha de aplicación.")
		}
		if applied {
			dateText := time.Now().In(s.viewZone(ctx)).Format("2006-01-02")
			if existing != nil {
				dateText = existing.Applied
			}
			if date != nil {
				dateText = *date
			}
			parsed, err := time.Parse("2006-01-02", dateText)
			if err != nil {
				return vaccinationError("La fecha de aplicación no es válida.")
			}
			birth, err := time.Parse("2006-01-02", p.BirthDate)
			if err != nil {
				return err
			}
			today, err := time.Parse("2006-01-02", time.Now().In(s.viewZone(ctx)).Format("2006-01-02"))
			if err != nil {
				return err
			}
			if parsed.Before(birth) || parsed.After(today) {
				return vaccinationError("La fecha debe estar entre el nacimiento y hoy.")
			}
			if existing == nil {
				_, err = tx.Exec(ctx, "INSERT INTO patient_vaccinations(patient_id,dose_id,applied_at) VALUES($1,$2,$3::date)", p.ID, doseID, dateText)
			} else {
				_, err = tx.Exec(ctx, "UPDATE patient_vaccinations SET applied_at=$2::date WHERE id=$1", existing.ID, dateText)
			}
			if err != nil {
				return err
			}
		} else if existing != nil {
			if _, err := tx.Exec(ctx, "DELETE FROM patient_vaccinations WHERE id=$1", existing.ID); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, "UPDATE patients SET version=version+1,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 RETURNING version", p.ID, p.Version).Scan(&p.Version); err != nil {
			return err
		}
		if err := s.detail(ctx, tx, &p); err != nil {
			return err
		}
		schedule, err := s.schedule(ctx, tx, p, cal)
		if err != nil {
			return err
		}
		result = SavedVaccination{p, schedule}
		return nil
	})
	return result, err
}
func (s *Service) VaccinationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/doctorchart/patients/{id}/vaccinations", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			api.WriteError(w, api.Invalid("La solicitud contiene datos inválidos."))
			return
		}
		result, err := s.Vaccinations(r.Context(), api.User(r.Context()).UserID, id)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		api.WriteJSON(w, 200, result)
	})
	mux.HandleFunc("PUT /api/doctorchart/patients/{id}/vaccinations/{applicationId}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			api.WriteError(w, api.Invalid("La solicitud contiene datos inválidos."))
			return
		}
		request, err := api.Decode(w, r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		result, err := s.SaveVaccination(r.Context(), api.User(r.Context()).UserID, id, r.PathValue("applicationId"), request)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		api.WriteJSON(w, 200, result)
	})
}
