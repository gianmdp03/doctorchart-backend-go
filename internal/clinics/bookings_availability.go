package clinics

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

// scheduleSlotEnd is the shared M3 slot calculator, not a second booking clock.
// Existing reservations ignore a newly increased notice/horizon but must retain
// their exact civil slot, duration and exception compatibility.
func scheduleSlotEnd(ctx context.Context, q scheduleReader, clinic Clinic, start, now time.Time, existing bool) (time.Time, error) {
	agenda, err := readAgenda(ctx, q, clinic.ID)
	if err != nil {
		return time.Time{}, err
	}
	if !agenda.Configured {
		return time.Time{}, slotUnavailable()
	}
	loc, err := time.LoadLocation(clinic.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	local := start.In(loc)
	date := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	config := agenda.ScheduleConfig
	if existing {
		config.MinNoticeMinutes = 0
		now = start.Add(-time.Minute)
	}
	slots, err := config.slots(ctx, date, loc, now)
	if err != nil {
		return time.Time{}, err
	}
	for _, slot := range slots {
		if slot.Start.Equal(start) {
			return slot.End, nil
		}
	}
	return time.Time{}, slotUnavailable()
}
func validateBookedSchedules(ctx context.Context, q scheduleReader, workspace int64, now time.Time) error {
	rows, err := q.Query(ctx, "SELECT "+bookingColumns+" FROM bookings b WHERE workspace_id=$1 AND end_at>$2 AND status IN ('CONFIRMED','CHECKED_IN') ORDER BY start_at,id", workspace, now)
	if err != nil {
		return err
	}
	items := []Booking{}
	for rows.Next() {
		b, e := scanBooking(rows)
		if e != nil {
			rows.Close()
			return e
		}
		items = append(items, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, b := range items {
		clinic, err := scan(q.QueryRow(ctx, "SELECT "+columns+" FROM clinics WHERE id=$1 AND workspace_id=$2", b.ClinicID, workspace))
		if err != nil {
			return err
		}
		end, err := scheduleSlotEnd(ctx, q, clinic, b.Start, now, true)
		if err != nil {
			// Validation failures are conflicts, infrastructure failures retain their type.
			if _, ok := err.(*api.Error); !ok {
				return err
			}
			return api.Failure(409, "BOOKING_SCHEDULE_CONFLICT", "El cambio dejaría una reserva confirmada fuera de su horario. Reprogramala o cancelala primero.")
		}
		if !end.Equal(b.End) {
			return api.Failure(409, "BOOKING_SCHEDULE_CONFLICT", "La duración nueva no coincide con una reserva existente. Reprogramala o cancelala primero.")
		}
	}
	return nil
}

type occupiedWindow struct{ start, end time.Time }

func occupiedBookingWindows(ctx context.Context, q scheduleReader, workspace int64, from, to time.Time) ([]occupiedWindow, error) {
	rows, err := q.Query(ctx, "SELECT start_at,end_at FROM bookings WHERE workspace_id=$1 AND status<>'CANCELLED' AND start_at<$3 AND end_at>$2 ORDER BY start_at", workspace, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	windows := []occupiedWindow{}
	for rows.Next() {
		var w occupiedWindow
		if err = rows.Scan(&w.start, &w.end); err != nil {
			return nil, err
		}
		windows = append(windows, w)
	}
	return windows, rows.Err()
}
func removeOccupiedSlots(slots []AvailableSlot, occupied []occupiedWindow) []AvailableSlot {
	available := make([]AvailableSlot, 0, len(slots))
	next := 0
	for _, slot := range slots {
		for next < len(occupied) && !occupied[next].end.After(slot.Start) {
			next++
		}
		if next < len(occupied) && occupied[next].start.Before(slot.End) && occupied[next].end.After(slot.Start) {
			continue
		}
		available = append(available, slot)
	}
	return available
}

// Keep the reader interface satisfied by real PostgreSQL transactions and pools.
var _ scheduleReader = (pgx.Tx)(nil)
