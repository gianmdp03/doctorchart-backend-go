package clinics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
)

type clinicSchedule struct {
	ClinicID   string `json:"clinicId"`
	Version    string `json:"version"`
	Configured bool   `json:"configured"`
	ScheduleConfig
}
type agendaRecord struct {
	id, zone, name string
	config         ScheduleConfig
}
type scheduleReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type agendaWindow struct {
	start, end         time.Time
	clinic, name, zone string
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
			result = append(result, agendaWindow{start: instant, end: instant.Add(time.Minute), clinic: a.id, name: a.name, zone: a.zone})
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
				loc, err := time.LoadLocation(prior.zone)
				if err != nil {
					return err
				}
				return api.Failure(409, "SCHEDULE_CONFLICT", fmt.Sprintf("Tu atención en %s se superpone con %s el %s de %s a %s (%s). Revisá los bloques o excepciones.", w.name, prior.name, prior.start.In(loc).Format("02/01/2006"), prior.start.In(loc).Format("15:04"), prior.end.In(loc).Format("15:04"), prior.zone))
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
	if err := checkAgendaConflicts(ctx, agendas, date.AddDate(0, 0, -1), 369); err != nil {
		return err
	}
	return validateBookedSchedules(ctx, tx, workspace, now)
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
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(clean)
	}()
	agenda, err := readAgenda(r.Context(), tx, clinic.ID)
	if err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
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
	for _, key := range []string{"expectedVersion", "minNoticeMinutes", "maxAdvanceDays", "weekly", "exceptions"} {
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
	if raw, present := body["slotMinutes"]; present {
		m, e := api.Integer(raw, "slotMinutes", 5)
		if e != nil || m > 480 {
			return api.Invalid("La duración general de compatibilidad debe estar entre 5 y 480 minutos.")
		}
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
	if err = saveAgenda(ctx, tx, current.ID, workspace, &request.ScheduleConfig, previous); err != nil {
		return err
	}
	if err = validateBlockBookings(ctx, tx, current, previous.ScheduleConfig, request.ScheduleConfig, time.Now().UTC()); err != nil {
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
	return s.availabilityFor(w, r, user, nil)
}
func (s *Service) availabilityFor(w http.ResponseWriter, r *http.Request, user string, public *publicAdmission) error {
	clinic, err := s.scheduleClinic(r, user)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	for key, values := range q {
		if (key != "from" && key != "days" && key != "specialtyContextId") || len(values) != 1 {
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
	selectedContext := q.Get("specialtyContextId")
	if selectedContext != "" {
		if _, err = decimal(selectedContext, true); err != nil {
			return err
		}
	}
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
	var staffSnapshot billing.Snapshot
	if staffFrom(ctx) != nil {
		staffSnapshot, err = s.staffRights.Resolve(ctx, user)
		if err != nil {
			return unavailable()
		}
		if err = s.staffRights.Fence(ctx, tx, staffSnapshot); err != nil {
			return unavailable()
		}
	}
	var workspace int64
	if err = tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR SHARE", user).Scan(&workspace); err != nil {
		return err
	}
	readyContexts := map[string]bool{}
	contextRows, e := tx.Query(ctx, "SELECT id::text FROM doctor_specialty_contexts WHERE workspace_id=$1 AND onboarding_completed AND definition IS NOT NULL", workspace)
	if e != nil {
		return e
	}
	for contextRows.Next() {
		var id string
		if e = contextRows.Scan(&id); e != nil {
			contextRows.Close()
			return e
		}
		readyContexts[id] = true
	}
	e = contextRows.Err()
	contextRows.Close()
	if e != nil {
		return e
	}
	if selectedContext != "" && !readyContexts[selectedContext] {
		return api.Failure(404, "SPECIALTY_CONTEXT_NOT_FOUND", "La especialidad no está disponible para atención.")
	}

	if staff := staffFrom(ctx); staff != nil {
		if err = staff.check(ctx, tx, workspace, staffSnapshot, Booking{}, bookingRequest{}); err != nil {
			return err
		}
	}
	clinic, err = scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM clinics WHERE id=$1 AND workspace_id=$2", clinic.ID, workspace))
	if err != nil {
		return err
	}
	if public != nil {
		current, e := s.readPublicDoctor(ctx, tx, public.doctorID)
		if e != nil {
			return e
		}
		if !current.published {
			return publicMissing()
		}
		if !publicContextClinic(current, public.contextID, public.clinicID) {
			return publicMissing()
		}
		selectedContext = public.contextID
		found := false
		for _, c := range current.Clinics {
			if c.ID == public.clinicID {
				found = true
			}
		}
		if !found {
			return publicMissing()
		}
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
			if failure, ok := err.(*api.Error); ok && failure.Code == "SCHEDULE_CONFLICT" && (public != nil || staffFrom(ctx) != nil) {
				return slotUnavailable()
			}
			return err
		}
	}
	loc, err := time.LoadLocation(clinic.Timezone)
	if err != nil {
		return err
	}
	occupied, err := occupiedBookingWindows(ctx, tx, workspace, first.Add(-26*time.Hour), first.AddDate(0, 0, days).Add(50*time.Hour))
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
		filtered := []AvailableSlot{}
		for _, slot := range slots {
			if readyContexts[slot.contextID] && (selectedContext == "" || slot.contextID == selectedContext) {
				filtered = append(filtered, slot)
			}
		}
		slots = filtered
		result = append(result, AvailabilityDay{Date: date.Format("2006-01-02"), Slots: removeOccupiedSlots(slots, occupied)})
	}
	if public != nil {
		api.WriteJSON(w, 200, map[string]any{"timezone": clinic.Timezone, "days": result})
		return nil
	}
	api.WriteJSON(w, 200, map[string]any{"clinicId": clinic.ID, "timezone": clinic.Timezone, "scheduleVersion": agenda.Version, "generatedAt": now, "days": result})
	return nil
}
