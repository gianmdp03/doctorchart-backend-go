package clinics

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

const civilLayout = "2006-01-02"

type agendaWindow struct {
	ContextID string    `json:"specialtyContextId,omitempty"`
	ClinicID  string    `json:"clinicId"`
	Name      string    `json:"name"`
	Timezone  string    `json:"timezone"`
	Modality  string    `json:"modality"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
}

func localDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Intersect each civil range with every constant-offset period. This preserves
// both occurrences in a fall-back hour and omits nonexistent spring hours.
func agendaWindows(ctx context.Context, a agendaRecord, first time.Time, days int) ([]agendaWindow, error) {
	loc, err := time.LoadLocation(a.zone)
	if err != nil {
		return nil, err
	}
	result := []agendaWindow{}
	for n := range days {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		date := first.AddDate(0, 0, n)
		ranges := a.config.rangesOn(date)
		if len(ranges) == 0 {
			continue
		}
		limit := date.Add(50 * time.Hour)
		for period := date.Add(-26 * time.Hour); period.Before(limit); {
			local := period.In(loc)
			_, offset := local.Zone()
			_, transition := local.ZoneBounds()
			endPeriod := limit
			if transition.After(period) && transition.Before(limit) {
				endPeriod = transition.UTC()
			}
			midnight := date.Add(-time.Duration(offset) * time.Second)
			for _, r := range ranges {
				startMinute, _ := wallMinute(r.Start, false)
				endMinute, _ := wallMinute(r.End, true)
				start := maxTime(period, midnight.Add(time.Duration(startMinute)*time.Minute))
				end := minTime(endPeriod, midnight.Add(time.Duration(endMinute)*time.Minute))
				if start.Before(end) {
					result = append(result, agendaWindow{
						ClinicID: a.id, Name: a.name, Timezone: a.zone, Modality: a.modality,
						ContextID: r.ContextID, Start: start, End: end,
					})
				}
			}
			period = endPeriod
		}
	}
	return result, nil
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func scheduleConflict(requested, existing agendaWindow, booking bool) error {
	start := maxTime(requested.Start, existing.Start)
	end := minTime(requested.End, existing.End)
	zone, _ := time.LoadLocation(requested.Timezone)
	otherZone, _ := time.LoadLocation(existing.Timezone)
	day := localDate(start.In(zone))
	code, subject := "SCHEDULE_CONFLICT", "la agenda de"
	if booking {
		code, subject = "BOOKING_SCHEDULE_CONFLICT", "un turno vigente en"
	}
	failure := api.Failure(409, code, fmt.Sprintf(
		"La franja de %s se superpone con %s %s el %s de %s a %s (%s). Superposición: %s–%s (%s).",
		requested.Name, subject, existing.Name, existing.Start.In(otherZone).Format("02/01/2006"),
		existing.Start.In(otherZone).Format("15:04"), existing.End.In(otherZone).Format("15:04"), existing.Timezone,
		start.In(zone).Format("15:04"), end.In(zone).Format("15:04"), requested.Timezone))
	failure.Details = struct {
		Requested    agendaWindow `json:"requested"`
		Existing     agendaWindow `json:"existing"`
		Date         string       `json:"date"`
		Weekday      int          `json:"weekday"`
		OverlapStart time.Time    `json:"overlapStart"`
		OverlapEnd   time.Time    `json:"overlapEnd"`
	}{requested, existing, day.Format(civilLayout), (int(day.Weekday())+6)%7 + 1, start, end}
	return failure
}

func localScheduleConflict(clinic Clinic, conflict *localRangeConflict) error {
	existing, requested := conflict.existing, conflict.requested
	start, end := max(existing.Start, requested.Start), min(existing.End, requested.End)
	when := ""
	if conflict.weekday != 0 {
		weekdays := [...]string{"lunes", "martes", "miércoles", "jueves", "viernes", "sábado", "domingo"}
		when = "los " + weekdays[conflict.weekday-1]
	} else {
		from, _ := civilDate(conflict.from)
		through, _ := civilDate(conflict.through)
		when = "el " + from.Format("02/01/2006")
		if !from.Equal(through) {
			when = "del " + from.Format("02/01/2006") + " al " + through.Format("02/01/2006")
		}
	}
	failure := api.Invalid(fmt.Sprintf("En %s, la franja de %s a %s se superpone con la de %s a %s %s. Superposición: %s–%s (%s).",
		clinic.Name, requested.Start, requested.End, existing.Start, existing.End, when, start, end, clinic.Timezone))
	failure.Details = struct {
		ClinicID     string        `json:"clinicId"`
		Name         string        `json:"name"`
		Modality     string        `json:"modality"`
		Timezone     string        `json:"timezone"`
		Weekday      int           `json:"weekday,omitempty"`
		From         string        `json:"from,omitempty"`
		Through      string        `json:"through,omitempty"`
		Requested    ScheduleRange `json:"requestedLocal"`
		Existing     ScheduleRange `json:"existingLocal"`
		OverlapStart string        `json:"overlapStartLocal"`
		OverlapEnd   string        `json:"overlapEndLocal"`
	}{clinic.ID, clinic.Name, clinic.Modality, clinic.Timezone, conflict.weekday, conflict.from, conflict.through, requested, existing, start, end}
	return failure
}

func checkAgendaConflicts(ctx context.Context, agendas []agendaRecord, first time.Time, days int) error {
	return checkAgendaWindows(ctx, agendas, first, days, "", time.Time{})
}

func checkAgendaWindows(ctx context.Context, agendas []agendaRecord, first time.Time, days int, changed string, since time.Time) error {
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
	sort.SliceStable(windows, func(i, j int) bool { return windows[i].Start.Before(windows[j].Start) })
	active := []agendaWindow{}
	for _, w := range windows {
		next := active[:0]
		for _, prior := range active {
			if !prior.End.After(w.Start) {
				continue
			}
			if prior.ClinicID != w.ClinicID && minTime(prior.End, w.End).After(since) {
				if changed == "" || changed == w.ClinicID {
					return scheduleConflict(w, prior, false)
				}
				if changed == prior.ClinicID {
					return scheduleConflict(prior, w, false)
				}
			}
			next = append(next, prior)
		}
		active = append(next, w)
	}
	return nil
}

// Between exception boundaries and UTC offset transitions the schedules repeat
// every seven days. Sample a full week plus civil/UTC date margins at each
// boundary. Future IANA rules repeat over the 400-year Gregorian cycle.
func validateRecurringAgendas(ctx context.Context, agendas []agendaRecord, changed string, now time.Time) error {
	if len(agendas) < 2 {
		return nil
	}
	today := localDate(now)
	checkpoints := []time.Time{today}
	last := today
	zones := map[string]bool{}
	included := false
	for _, a := range agendas {
		included = included || a.id == changed
		zones[a.zone] = true
		for _, e := range a.config.Exceptions {
			from, _ := civilDate(e.From)
			through, _ := civilDate(e.Through)
			for _, boundary := range []time.Time{from, through.AddDate(0, 0, 1)} {
				if !boundary.Before(today) {
					checkpoints = append(checkpoints, boundary)
					last = maxTime(last, boundary)
				}
			}
		}
	}
	if !included {
		return nil
	}
	limit := minTime(last.AddDate(401, 0, 0), time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
	for zone := range zones {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			return err
		}
		for instant := today.AddDate(0, 0, -2); instant.Before(limit); {
			if err := ctx.Err(); err != nil {
				return err
			}
			_, transition := instant.In(loc).ZoneBounds()
			if transition.IsZero() || !transition.Before(limit) {
				break
			}
			if !transition.After(instant) {
				// The extrapolated year boundary may be returned at or behind
				// the cursor. Continue on the following day.
				instant = instant.Add(24 * time.Hour)
				continue
			}
			before := transition.Add(-time.Second).In(loc)
			after := transition.In(loc)
			_, offsetBefore := before.Zone()
			_, offsetAfter := after.Zone()
			if offsetBefore != offsetAfter {
				checkpoints = append(checkpoints, localDate(transition.UTC()))
			}
			instant = transition
		}
	}
	sort.Slice(checkpoints, func(i, j int) bool { return checkpoints[i].Before(checkpoints[j]) })
	var checkedUntil time.Time
	for _, point := range checkpoints {
		first := maxTime(point.AddDate(0, 0, -2), checkedUntil.AddDate(0, 0, -2))
		until := minTime(point.AddDate(0, 0, 10), limit)
		if !until.After(checkedUntil) {
			continue
		}
		if err := checkAgendaWindows(ctx, agendas, first, int(until.Sub(first)/(24*time.Hour)), changed, now); err != nil {
			return err
		}
		checkedUntil = until
	}
	return nil
}

func (s *Service) validateSchedules(ctx context.Context, tx pgx.Tx, workspace int64, changed string, now time.Time) error {
	agendas, err := readAgendas(ctx, tx, workspace)
	if err != nil {
		return err
	}
	if err := validateRecurringAgendas(ctx, agendas, changed, now); err != nil {
		return err
	}
	if err := validateArchivedReservations(ctx, tx, workspace, agendas, changed, now); err != nil {
		return err
	}
	return validateBookedSchedules(ctx, tx, workspace, now)
}

func validateDraftAgendas(ctx context.Context, q scheduleReader, workspace int64, draft agendaRecord, now time.Time) error {
	agendas, err := readAgendas(ctx, q, workspace)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(agendas, func(agenda agendaRecord) bool { return agenda.id == draft.id })
	if index < 0 {
		agendas = append(agendas, draft)
	} else {
		agendas[index] = draft
	}
	if err := validateRecurringAgendas(ctx, agendas, draft.id, now); err != nil {
		return err
	}
	return validateArchivedReservations(ctx, q, workspace, agendas, draft.id, now)
}

func archivedReservationWindows(ctx context.Context, q scheduleReader, workspace int64, from, to time.Time) ([]agendaWindow, error) {
	rows, err := q.Query(ctx, `SELECT c.id::text,COALESCE(b.clinic_snapshot->>'name',c.name),
		COALESCE(b.clinic_snapshot->>'timezone',c.timezone),c.modality,b.specialty_context_id::text,b.start_at,b.end_at
		FROM bookings b JOIN clinics c ON c.id=b.clinic_id AND c.workspace_id=b.workspace_id
		WHERE b.workspace_id=$1 AND c.status='ARCHIVED' AND b.status<>'CANCELLED'
		  AND b.end_at>$2 AND b.start_at<$3 ORDER BY b.start_at`, workspace, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []agendaWindow{}
	for rows.Next() {
		var w agendaWindow
		if err := rows.Scan(&w.ClinicID, &w.Name, &w.Timezone, &w.Modality, &w.ContextID, &w.Start, &w.End); err != nil {
			return nil, err
		}
		w.Start, w.End = w.Start.UTC(), w.End.UTC()
		result = append(result, w)
	}
	return result, rows.Err()
}

func validateArchivedReservations(ctx context.Context, q scheduleReader, workspace int64, agendas []agendaRecord, changed string, now time.Time) error {
	booked, err := archivedReservationWindows(ctx, q, workspace, now, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	for _, b := range booked {
		for _, a := range agendas {
			if a.id != changed && b.ClinicID != changed {
				continue
			}
			windows, err := agendaWindows(ctx, a, localDate(b.Start).AddDate(0, 0, -2), 5)
			if err != nil {
				return err
			}
			for _, w := range windows {
				if w.Start.Before(b.End) && b.Start.Before(w.End) {
					return scheduleConflict(w, b, true)
				}
			}
		}
	}
	return nil
}

// occupiedSchedule is owner-only and returns intervals without patient data.
func (s *Service) occupiedSchedule(w http.ResponseWriter, r *http.Request, user string) error {
	clinic, err := s.scheduleClinic(r, user)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	var selectedContextID int64
	if len(q["from"]) != 1 {
		return api.Invalid("Indicá la fecha de inicio de la semana.")
	}
	for key, values := range q {
		if key == "from" {
			continue
		}
		if key != "specialtyContextId" || len(values) != 1 || clinic.Modality != teleconsultation {
			return api.Invalid("Parámetros de horarios ocupados inválidos.")
		}
		selectedContextID, err = decimal(values[0], true)
		if err != nil {
			return err
		}
	}
	selectedContext := q.Get("specialtyContextId")
	date, err := civilDate(q.Get("from"))
	if err != nil || date.Year() >= 9999 {
		return field("from", "INVALID_DATE")
	}
	ctx := r.Context()
	workspace, err := s.workspace(ctx, user)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(clinic.Timezone)
	if err != nil {
		return err
	}
	from := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 7)
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if selectedContext != "" {
		var owned bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM doctor_specialty_contexts WHERE workspace_id=$1 AND id=$2)", workspace, selectedContextID).Scan(&owned); err != nil {
			return err
		}
		if !owned {
			return api.Failure(404, "SPECIALTY_CONTEXT_NOT_FOUND", "La especialidad no está disponible para atención.")
		}
	}
	agendas, err := readAgendas(ctx, tx, workspace)
	if err != nil {
		return err
	}
	items, err := archivedReservationWindows(ctx, tx, workspace, from, to)
	if err != nil {
		return err
	}
	for _, a := range agendas {
		if a.id == clinic.ID && selectedContext == "" {
			continue
		}
		part, err := agendaWindows(ctx, a, date.AddDate(0, 0, -2), 11)
		if err != nil {
			return err
		}
		for _, interval := range part {
			if a.id == clinic.ID && interval.ContextID == selectedContext {
				continue
			}
			if interval.Start.Before(to) && from.Before(interval.End) {
				interval.Start = maxTime(interval.Start, from).UTC()
				interval.End = minTime(interval.End, to).UTC()
				items = append(items, interval)
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Start.Before(items[j].Start) })
	api.WriteJSON(w, 200, map[string]any{"timezone": clinic.Timezone, "items": items})
	return nil
}
