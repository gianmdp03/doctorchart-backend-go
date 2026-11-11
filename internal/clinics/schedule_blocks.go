package clinics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func minuteText(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }
func readAgenda(ctx context.Context, q scheduleReader, id string) (clinicSchedule, error) {
	result := clinicSchedule{ClinicID: id, Version: "0", ScheduleConfig: ScheduleConfig{Weekly: []ScheduleDay{}, Exceptions: []ScheduleException{}}}
	err := q.QueryRow(ctx, "SELECT version::text,min_notice_minutes,max_advance_days FROM clinic_schedules WHERE clinic_id=$1", id).Scan(&result.Version, &result.MinNoticeMinutes, &result.MaxAdvanceDays)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	rows, err := q.Query(ctx, "SELECT id::text,from_date::text,through_date::text,label FROM schedule_exceptions WHERE clinic_id=$1 ORDER BY from_date,id", id)
	if err != nil {
		return result, err
	}
	exceptionIndex := map[string]int{}
	for rows.Next() {
		var e ScheduleException
		e.Ranges = []ScheduleRange{}
		if err = rows.Scan(&e.ID, &e.From, &e.Through, &e.Label); err != nil {
			rows.Close()
			return result, err
		}
		exceptionIndex[e.ID] = len(result.Exceptions)
		result.Exceptions = append(result.Exceptions, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = q.Query(ctx, "SELECT id::text,specialty_context_id::text,weekday,exception_id::text,start_minute,end_minute,slot_minutes FROM schedule_blocks WHERE clinic_id=$1 ORDER BY weekday NULLS LAST,exception_id,start_minute,id", id)
	if err != nil {
		return result, err
	}
	dayIndex := map[int]int{}
	uniform := -1
	for rows.Next() {
		var r ScheduleRange
		var day *int
		var exception *string
		var start, end int
		if err = rows.Scan(&r.ID, &r.ContextID, &day, &exception, &start, &end, &r.SlotMinutes); err != nil {
			rows.Close()
			return result, err
		}
		r.Start, r.End = minuteText(start), minuteText(end)
		if uniform == -1 {
			uniform = r.SlotMinutes
		} else if uniform != r.SlotMinutes {
			uniform = 0
		}
		if exception != nil {
			i, ok := exceptionIndex[*exception]
			if !ok {
				rows.Close()
				return result, fmt.Errorf("orphan schedule exception")
			}
			result.Exceptions[i].Ranges = append(result.Exceptions[i].Ranges, r)
		} else {
			if day == nil {
				rows.Close()
				return result, fmt.Errorf("schedule block has no day")
			}
			i, ok := dayIndex[*day]
			if !ok {
				i = len(result.Weekly)
				dayIndex[*day] = i
				result.Weekly = append(result.Weekly, ScheduleDay{Weekday: *day, Ranges: []ScheduleRange{}})
			}
			result.Weekly[i].Ranges = append(result.Weekly[i].Ranges, r)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	// A derived compatibility projection, never persisted or used over a block.
	if uniform > 0 {
		result.SlotMinutes = uniform
	}
	result.Configured = true
	return result, result.Validate()
}
func readAgendas(ctx context.Context, q scheduleReader, workspace int64) ([]agendaRecord, error) {
	rows, err := q.Query(ctx, "SELECT c.id::text,c.timezone,c.name FROM clinics c JOIN clinic_schedules s ON s.clinic_id=c.id WHERE c.workspace_id=$1 AND c.status<>'ARCHIVED' ORDER BY c.id", workspace)
	if err != nil {
		return nil, err
	}
	result := []agendaRecord{}
	for rows.Next() {
		var a agendaRecord
		if err = rows.Scan(&a.id, &a.zone, &a.name); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range result {
		agenda, e := readAgenda(ctx, q, result[i].id)
		if e != nil {
			return nil, e
		}
		result[i].config = agenda.ScheduleConfig
	}
	return result, nil
}
func blockMissing() error {
	return api.Failure(404, "SCHEDULE_BLOCK_NOT_FOUND", "El bloque o la excepción no pertenece a esta agenda. Recargá los horarios.")
}

func saveAgenda(ctx context.Context, tx pgx.Tx, clinic string, workspace int64, next *ScheduleConfig, previous clinicSchedule) error {
	// Workspace/clinic locks are held by putSchedule. No client context is authority.
	rows, err := tx.Query(ctx, "SELECT id::text FROM doctor_specialty_contexts WHERE workspace_id=$1 AND onboarding_completed AND definition IS NOT NULL", workspace)
	if err != nil {
		return err
	}
	contexts := map[string]bool{}
	only := ""
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		contexts[id] = true
		only = id
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	oldBlocks := map[string]ScheduleRange{}
	oldExceptions := map[string]bool{}
	for _, day := range previous.Weekly {
		for _, r := range day.Ranges {
			oldBlocks[r.ID] = r
		}
	}
	for _, e := range previous.Exceptions {
		oldExceptions[e.ID] = true
		for _, r := range e.Ranges {
			oldBlocks[r.ID] = r
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO clinic_schedules(clinic_id,workspace_id,min_notice_minutes,max_advance_days) VALUES($1,$2,$3,$4)
 ON CONFLICT(clinic_id) DO UPDATE SET min_notice_minutes=excluded.min_notice_minutes,max_advance_days=excluded.max_advance_days,version=clinic_schedules.version+1`, clinic, workspace, next.MinNoticeMinutes, next.MaxAdvanceDays); err != nil {
		return err
	}
	keptBlocks, keptExceptions := []int64{}, []int64{}
	seenBlocks, seenExceptions := map[string]bool{}, map[string]bool{}
	saveBlock := func(r *ScheduleRange, day *int, exception *int64, legacy []ScheduleRange) error {
		if r.SlotMinutes == 0 {
			r.SlotMinutes = next.SlotMinutes
		}
		if r.ContextID == "" && next.SlotMinutes != 0 && len(contexts) == 1 {
			var total int
			if err = tx.QueryRow(ctx, "SELECT count(*) FROM doctor_specialty_contexts WHERE workspace_id=$1", workspace).Scan(&total); err != nil {
				return err
			}
			if total == 1 {
				r.ContextID = only
			}
		}
		if !contexts[r.ContextID] {
			return api.Failure(404, "SPECIALTY_CONTEXT_NOT_FOUND", "La especialidad del bloque no pertenece a tu espacio de trabajo.")
		}
		// Old clients omit identities: only an unchanged range can reuse its ID.
		if r.ID == "" && next.SlotMinutes != 0 {
			for _, prior := range legacy {
				if prior.Start == r.Start && prior.End == r.End && !seenBlocks[prior.ID] {
					r.ID = prior.ID
					break
				}
			}
		}
		if r.ID != "" {
			if _, ok := oldBlocks[r.ID]; !ok || seenBlocks[r.ID] {
				return blockMissing()
			}
		}
		start, _ := wallMinute(r.Start, false)
		end, _ := wallMinute(r.End, true)
		if r.ID == "" {
			err = tx.QueryRow(ctx, `INSERT INTO schedule_blocks(clinic_id,workspace_id,specialty_context_id,weekday,exception_id,start_minute,end_minute,slot_minutes) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`, clinic, workspace, r.ContextID, day, exception, start, end, r.SlotMinutes).Scan(&r.ID)
		} else {
			_, err = tx.Exec(ctx, `UPDATE schedule_blocks SET specialty_context_id=$3,weekday=$4,exception_id=$5,start_minute=$6,end_minute=$7,slot_minutes=$8 WHERE id=$1 AND clinic_id=$2`, r.ID, clinic, r.ContextID, day, exception, start, end, r.SlotMinutes)
		}
		if err != nil {
			return err
		}
		id, e := decimal(r.ID, true)
		if e != nil {
			return e
		}
		keptBlocks = append(keptBlocks, id)
		seenBlocks[r.ID] = true
		return nil
	}
	for i := range next.Weekly {
		day := &next.Weekly[i]
		var legacy []ScheduleRange
		for _, d := range previous.Weekly {
			if d.Weekday == day.Weekday {
				legacy = d.Ranges
			}
		}
		for j := range day.Ranges {
			if err = saveBlock(&day.Ranges[j], &day.Weekday, nil, legacy); err != nil {
				return err
			}
		}
	}
	for i := range next.Exceptions {
		e := &next.Exceptions[i]
		var legacy []ScheduleRange
		if e.ID == "" && next.SlotMinutes != 0 {
			for _, prior := range previous.Exceptions {
				if prior.From == e.From && prior.Through == e.Through {
					e.ID = prior.ID
					break
				}
			}
		}
		if e.ID != "" {
			if !oldExceptions[e.ID] || seenExceptions[e.ID] {
				return blockMissing()
			}
			for _, prior := range previous.Exceptions {
				if prior.ID == e.ID {
					legacy = prior.Ranges
				}
			}
			_, err = tx.Exec(ctx, "UPDATE schedule_exceptions SET from_date=$3::date,through_date=$4::date,label=$5 WHERE id=$1 AND clinic_id=$2", e.ID, clinic, e.From, e.Through, e.Label)
		} else {
			err = tx.QueryRow(ctx, "INSERT INTO schedule_exceptions(clinic_id,workspace_id,from_date,through_date,label) VALUES($1,$2,$3::date,$4::date,$5) RETURNING id::text", clinic, workspace, e.From, e.Through, e.Label).Scan(&e.ID)
		}
		if err != nil {
			return err
		}
		eid, er := decimal(e.ID, true)
		if er != nil {
			return er
		}
		keptExceptions = append(keptExceptions, eid)
		seenExceptions[e.ID] = true
		for j := range e.Ranges {
			if err = saveBlock(&e.Ranges[j], nil, &eid, legacy); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(ctx, "DELETE FROM schedule_blocks WHERE clinic_id=$1 AND NOT(id=ANY($2::bigint[]))", clinic, keptBlocks); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "DELETE FROM schedule_exceptions WHERE clinic_id=$1 AND NOT(id=ANY($2::bigint[]))", clinic, keptExceptions)
	return err
}

func slotAt(ctx context.Context, c ScheduleConfig, clinic Clinic, start time.Time) (AvailableSlot, bool, error) {
	loc, err := time.LoadLocation(clinic.Timezone)
	if err != nil {
		return AvailableSlot{}, false, err
	}
	local := start.In(loc)
	date := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	c.MinNoticeMinutes = 0
	c.MaxAdvanceDays = 366
	slots, err := c.slots(ctx, date, loc, start.Add(-time.Minute))
	if err != nil {
		return AvailableSlot{}, false, err
	}
	for _, slot := range slots {
		if slot.Start.Equal(start) {
			return slot, true, nil
		}
	}
	return AvailableSlot{}, false, nil
}
func validateBlockBookings(ctx context.Context, tx pgx.Tx, clinic Clinic, before, after ScheduleConfig, now time.Time) error {
	rows, err := tx.Query(ctx, "SELECT "+bookingColumns+" FROM bookings b WHERE clinic_id=$1 AND end_at>$2 AND status IN ('CONFIRMED','CHECKED_IN') ORDER BY start_at", clinic.ID, now)
	if err != nil {
		return err
	}
	bookings := []Booking{}
	for rows.Next() {
		b, e := scanBooking(rows)
		if e != nil {
			rows.Close()
			return e
		}
		bookings = append(bookings, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, booking := range bookings {
		start := booking.Start
		old, ok, err := slotAt(ctx, before, clinic, start)
		if err != nil {
			return err
		}
		if !ok {
			return api.Failure(409, "BOOKING_SCHEDULE_CONFLICT", "Una reserva futura no coincide con los bloques actuales. Reprogramala o cancelala antes de cambiar los horarios.")
		}
		next, found, err := slotAt(ctx, after, clinic, start)
		if err != nil {
			return err
		}
		if !found || old.blockID != next.blockID || old.contextID != next.contextID || !old.End.Equal(next.End) {
			return api.Failure(409, "BOOKING_SCHEDULE_CONFLICT", "El cambio altera un bloque con reservas futuras. Conservá su horario y duración o reprogramá o cancelá las reservas primero.")
		}
		if next.contextID != booking.ContextID || !next.End.Equal(booking.End) || (booking.Provenance.Kind == "SCHEDULE_BLOCK" && next.blockID != booking.Provenance.BlockID) {
			return api.Failure(409, "BOOKING_SCHEDULE_CONFLICT", "El cambio contradice el bloque de origen de una reserva futura. Reprogramala o cancelala primero.")
		}
	}
	return nil
}
