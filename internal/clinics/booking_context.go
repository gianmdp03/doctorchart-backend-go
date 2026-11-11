package clinics

import (
	"context"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"time"
)

// Value snapshots remain comparable and do not follow mutable catalog/profile data.
type BookingSpecialty struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}
type BookingClinic struct {
	Name          string `json:"name"`
	Address       string `json:"address"`
	PublicAddress string `json:"publicAddress"`
	Timezone      string `json:"timezone"`
}
type BookingProvenance struct {
	Kind            string `json:"kind"`
	BlockID         string `json:"blockId"`
	ScheduleVersion string `json:"scheduleVersion"`
	Weekday         int    `json:"weekday"`
	ExceptionID     string `json:"exceptionId"`
	From            string `json:"from"`
	Through         string `json:"through"`
	StartMinute     int    `json:"startMinute"`
	EndMinute       int    `json:"endMinute"`
	SlotMinutes     int    `json:"slotMinutes"`
	Timezone        string `json:"timezone"`
}

func bookingContextConflict() error {
	return api.Failure(409, "BOOKING_CONTEXT_MISMATCH", "El horario debe pertenecer a la especialidad del turno. Para cambiar de especialidad, cancelá y creá un turno nuevo.")
}

func resolveBookingSlot(ctx context.Context, q scheduleReader, clinic Clinic, start, now time.Time) (AvailableSlot, error) {
	agenda, err := readAgenda(ctx, q, clinic.ID)
	if err != nil {
		return AvailableSlot{}, err
	}
	if !agenda.Configured {
		return AvailableSlot{}, slotUnavailable()
	}
	loc, err := time.LoadLocation(clinic.Timezone)
	if err != nil {
		return AvailableSlot{}, err
	}
	local := start.In(loc)
	date := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	slots, err := agenda.slots(ctx, date, loc, now)
	if err != nil {
		return AvailableSlot{}, err
	}
	for _, slot := range slots {
		if slot.Start.Equal(start) && slot.blockID != "" && slot.contextID != "" {
			var ready bool
			if err = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM doctor_specialty_contexts WHERE id=$1 AND onboarding_completed AND definition IS NOT NULL)", slot.contextID).Scan(&ready); err != nil {
				return AvailableSlot{}, err
			}
			if !ready {
				return AvailableSlot{}, slotUnavailable()
			}
			return slot, nil
		}
	}
	return AvailableSlot{}, slotUnavailable()
}
