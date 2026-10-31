package clinics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type clinicSchedule struct {
	ClinicID   string `json:"clinicId"`
	Version    string `json:"version"`
	Configured bool   `json:"configured"`
	ScheduleConfig
}
type agendaRecord struct {
	id, zone string
	config   ScheduleConfig
}
type scheduleReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readAgenda(ctx context.Context, q scheduleReader, id string) (clinicSchedule, error) {
	result := clinicSchedule{ClinicID: id, Version: "0"}
	var weekly, exceptions []byte
	err := q.QueryRow(ctx, "SELECT version::text,slot_minutes,min_notice_minutes,max_advance_days,weekly,exceptions FROM clinic_schedules WHERE clinic_id=$1", id).Scan(&result.Version, &result.SlotMinutes, &result.MinNoticeMinutes, &result.MaxAdvanceDays, &weekly, &exceptions)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(weekly, &result.Weekly); err != nil {
		return result, err
	}
	if err = json.Unmarshal(exceptions, &result.Exceptions); err != nil {
		return result, err
	}
	result.Configured = true
	return result, result.Validate()
}
func readAgendas(ctx context.Context, q scheduleReader, workspace int64) ([]agendaRecord, error) {
	rows, err := q.Query(ctx, "SELECT c.id::text,c.timezone,s.slot_minutes,s.min_notice_minutes,s.max_advance_days,s.weekly,s.exceptions FROM clinics c JOIN clinic_schedules s ON s.clinic_id=c.id WHERE c.workspace_id=$1 AND c.status<>'ARCHIVED' ORDER BY c.id", workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []agendaRecord{}
	for rows.Next() {
		var a agendaRecord
		var weekly, exceptions []byte
		if err = rows.Scan(&a.id, &a.zone, &a.config.SlotMinutes, &a.config.MinNoticeMinutes, &a.config.MaxAdvanceDays, &weekly, &exceptions); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(weekly, &a.config.Weekly); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(exceptions, &a.config.Exceptions); err != nil {
			return nil, err
		}
		if err = a.config.Validate(); err != nil {
			return nil, fmt.Errorf("invalid stored schedule: %w", err)
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

type agendaWindow struct {
	start, end time.Time
	clinic     string
}

func agendaWindows(ctx context.Context, a agendaRecord, first time.Time, days int) ([]agendaWindow, error) {
	loc, err := time.LoadLocation(a.zone)
	if err != nil {
		return nil, err
	}
	result := []agendaWindow{}
	for n := 0; n < days; n++ {
		date := first.AddDate(0, 0, n)
		ranges := a.config.rangesOn(date)
		if len(ranges) == 0 {
			continue
		}
		bounds := make([][2]int, 0, len(ranges))
		for _, r := range ranges {
			start, _ := wallMinute(r.Start, false)
			end, _ := wallMinute(r.End, true)
			bounds = append(bounds, [2]int{start, end})
		}
		var active *agendaWindow
		err = civilMinutes(ctx, date, loc, func(instant time.Time, minute int) {
			open := false
			for _, bound := range bounds {
				begin, end := bound[0], bound[1]
				if minute >= begin && minute < end {
					open = true
					break
				}
			}
			if !open {
				return
			}
			if active != nil && active.end.Equal(instant) {
				active.end = instant.Add(time.Minute)
				return
			}
			result = append(result, agendaWindow{start: instant, end: instant.Add(time.Minute), clinic: a.id})
			active = &result[len(result)-1]
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
func checkAgendaConflicts(ctx context.Context, agendas []agendaRecord, first time.Time, days int) error {
	if len(agendas) < 2 {
		return nil
	}
	windows := []agendaWindow{}
	for _, a := range agendas {
		part, err := agendaWindows(ctx, a, first, days)
		if err != nil {
			return err
		}
		windows = append(windows, part...)
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].start.Before(windows[j].start) })
	// Keep all intervals still open; adjacent half-open intervals do not conflict.
	active := []agendaWindow{}
	for _, w := range windows {
		next := active[:0]
		for _, prior := range active {
			if !prior.end.After(w.start) {
				continue
			}
			if prior.clinic != w.clinic {
				return api.Failure(409, "SCHEDULE_CONFLICT", fmt.Sprintf("Los consultorios %s y %s tienen horarios incompatibles entre %s y %s. Revisá las franjas o excepciones.", prior.clinic, w.clinic, first.Format("02/01/2006"), first.AddDate(0, 0, days-1).Format("02/01/2006")))
			}
			next = append(next, prior)
		}
		active = append(next, w)
	}
	return nil
}
func (s *Service) validateSchedules(ctx context.Context, tx pgx.Tx, workspace int64, now time.Time) error {
	agendas, err := readAgendas(ctx, tx, workspace)
	if err != nil {
		return err
	}
	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return checkAgendaConflicts(ctx, agendas, date.AddDate(0, 0, -1), 369)
}
func (s *Service) scheduleRoutes(mux *http.ServeMux, wrap func(func(http.ResponseWriter, *http.Request, string) error) http.HandlerFunc) {
	mux.HandleFunc("GET /api/doctorchart/clinics/{id}/schedule", wrap(s.getSchedule))
	mux.HandleFunc("PUT /api/doctorchart/clinics/{id}/schedule", wrap(s.putSchedule))
	mux.HandleFunc("GET /api/doctorchart/clinics/{id}/availability", wrap(s.availability))
}
func (s *Service) scheduleClinic(r *http.Request, user string) (Clinic, error) {
	id, err := decimal(r.PathValue("id"), true)
	if err != nil {
		return Clinic{}, err
	}
	return s.owned(r.Context(), user, id)
}
func (s *Service) getSchedule(w http.ResponseWriter, r *http.Request, user string) error {
	clinic, err := s.scheduleClinic(r, user)
	if err != nil {
		return err
	}
	agenda, err := readAgenda(r.Context(), s.pool, clinic.ID)
	if err != nil {
		return err
	}
	if !agenda.Configured {
		api.WriteJSON(w, 200, map[string]any{"clinicId": clinic.ID, "version": "0", "configured": false, "slotMinutes": nil, "minNoticeMinutes": nil, "maxAdvanceDays": nil, "weekly": []ScheduleDay{}, "exceptions": []ScheduleException{}})
	} else {
		api.WriteJSON(w, 200, agenda)
	}
	return nil
}
func (s *Service) putSchedule(w http.ResponseWriter, r *http.Request, user string) error {
	clinic, err := s.scheduleClinic(r, user)
	if err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	body, err := api.Decode(w, r)
	if err != nil {
		return err
	}
	if err = api.Only(body, "expectedVersion", "slotMinutes", "minNoticeMinutes", "maxAdvanceDays", "weekly", "exceptions"); err != nil {
		return err
	}
	for _, key := range []string{"expectedVersion", "slotMinutes", "minNoticeMinutes", "maxAdvanceDays", "weekly", "exceptions"} {
		if body[key] == nil {
			return field(key, "REQUIRED")
		}
	}
	expected, ok := body["expectedVersion"].(string)
	if !ok {
		return field("expectedVersion", "INVALID_VERSION")
	}
	if _, err = decimal(expected, false); err != nil {
		return field("expectedVersion", "INVALID_VERSION")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var request struct {
		ExpectedVersion string `json:"expectedVersion"`
		ScheduleConfig
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&request); err != nil {
		return api.Invalid("Revisá los tipos y los campos de la agenda.")
	}
	if err = request.Validate(); err != nil {
		return api.Invalid(err.Error())
	}
	snapshot, err := s.rights.Resolve(r.Context(), user)
	if err != nil {
		return unavailable()
	}
	ctx := r.Context()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(clean)
	}()
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='10s'"); err != nil {
		return err
	}
	if err = s.rights.Fence(ctx, tx, snapshot); err != nil {
		return unavailable()
	}
	var workspace int64
	if err = tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR UPDATE", user).Scan(&workspace); err != nil {
		return err
	}
	current, err := scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM clinics WHERE id=$1 AND workspace_id=$2 FOR UPDATE", clinic.ID, workspace))
	if err != nil {
		return err
	}
	if current.Status == "ARCHIVED" {
		return api.Failure(409, "CLINIC_STATE_CONFLICT", "Restaurá el consultorio antes de modificar su agenda.")
	}
	if err = s.rights.Fence(ctx, tx, snapshot); err != nil {
		return unavailable()
	}
	if !snapshot.ClinicManagement {
		return api.Failure(403, "CLINIC_MANAGEMENT_DISABLED", "Tu cobertura no habilita la gestión de consultorios.")
	}
	var count int64
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM clinics WHERE workspace_id=$1 AND status<>'ARCHIVED'", workspace).Scan(&count); err != nil {
		return err
	}
	if snapshot.MaxClinics == nil {
		return unavailable()
	}
	if *snapshot.MaxClinics != -1 && count > *snapshot.MaxClinics {
		return api.Failure(409, "CLINIC_LIMIT_REACHED", "Regularizá el cupo de consultorios antes de modificar agendas.")
	}
	previous, err := readAgenda(ctx, tx, current.ID)
	if err != nil {
		return err
	}
	if previous.Version != expected {
		return api.Failure(409, "VERSION_CONFLICT", "La agenda cambió. Tus datos no se guardaron; recargá la versión vigente.")
	}
	weekly, _ := json.Marshal(request.Weekly)
	exceptions, _ := json.Marshal(request.Exceptions)
	_, err = tx.Exec(ctx, "INSERT INTO clinic_schedules(clinic_id,slot_minutes,min_notice_minutes,max_advance_days,weekly,exceptions) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(clinic_id) DO UPDATE SET slot_minutes=excluded.slot_minutes,min_notice_minutes=excluded.min_notice_minutes,max_advance_days=excluded.max_advance_days,weekly=excluded.weekly,exceptions=excluded.exceptions,version=clinic_schedules.version+1", current.ID, request.SlotMinutes, request.MinNoticeMinutes, request.MaxAdvanceDays, weekly, exceptions)
	if err != nil {
		return err
	}
	if err = s.validateSchedules(ctx, tx, workspace, time.Now().UTC()); err != nil {
		return err
	}
	result, err := readAgenda(ctx, tx, current.ID)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	api.WriteJSON(w, 200, result)
	return nil
}
func (s *Service) availability(w http.ResponseWriter, r *http.Request, user string) error {
	clinic, err := s.scheduleClinic(r, user)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	for key, values := range q {
		if (key != "from" && key != "days") || len(values) != 1 {
			return api.Invalid("Parámetros de disponibilidad inválidos.")
		}
	}
	first, err := civilDate(q.Get("from"))
	if err != nil {
		return field("from", "INVALID_DATE")
	}
	days, err := strconv.Atoi(q.Get("days"))
	if err != nil || days < 1 || days > 7 || first.AddDate(0, 0, days-1).Year() > 9999 {
		return field("days", "INVALID_RANGE")
	}
	ctx := r.Context()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='10s'"); err != nil {
		return err
	}
	var workspace int64
	if err = tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR SHARE", user).Scan(&workspace); err != nil {
		return err
	}
	clinic, err = scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM clinics WHERE id=$1 AND workspace_id=$2", clinic.ID, workspace))
	if err != nil {
		return err
	}
	agenda, err := readAgenda(ctx, tx, clinic.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result := []AvailabilityDay{}
	if clinic.Status == "ACTIVE" && agenda.Configured {
		all, err := readAgendas(ctx, tx, workspace)
		if err != nil {
			return err
		}
		if err = checkAgendaConflicts(r.Context(), all, first.AddDate(0, 0, -2), days+4); err != nil {
			return err
		}
	}
	loc, err := time.LoadLocation(clinic.Timezone)
	if err != nil {
		return err
	}
	for n := 0; n < days; n++ {
		date := first.AddDate(0, 0, n)
		slots := []AvailableSlot{}
		if clinic.Status == "ACTIVE" && agenda.Configured {
			slots, err = agenda.slots(r.Context(), date, loc, now)
			if err != nil {
				return err
			}
		}
		result = append(result, AvailabilityDay{Date: date.Format("2006-01-02"), Slots: slots})
	}
	api.WriteJSON(w, 200, map[string]any{"clinicId": clinic.ID, "timezone": clinic.Timezone, "scheduleVersion": agenda.Version, "generatedAt": now, "days": result})
	return nil
}
