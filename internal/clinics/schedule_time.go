package clinics

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ScheduleConfig uses local civil times. No generated slot is persisted.
type ScheduleConfig struct {
	SlotMinutes      int                 `json:"slotMinutes"`
	MinNoticeMinutes int                 `json:"minNoticeMinutes"`
	MaxAdvanceDays   int                 `json:"maxAdvanceDays"`
	Weekly           []ScheduleDay       `json:"weekly"`
	Exceptions       []ScheduleException `json:"exceptions"`
}
type ScheduleDay struct {
	Weekday int             `json:"weekday"`
	Ranges  []ScheduleRange `json:"ranges"`
}
type ScheduleRange struct {
	ID          string `json:"id,omitempty"`
	ContextID   string `json:"specialtyContextId,omitempty"`
	SlotMinutes int    `json:"slotMinutes,omitempty"`
	Start       string `json:"start"`
	End         string `json:"end"`
}
type ScheduleException struct {
	ID      string          `json:"id,omitempty"`
	From    string          `json:"from"`
	Through string          `json:"through"`
	Label   string          `json:"label"`
	Ranges  []ScheduleRange `json:"ranges"`
}
type AvailableSlot struct {
	blockID, contextID string
	SpecialtyContextID string    `json:"specialtyContextId,omitempty"`
	Start              time.Time `json:"start"`
	End                time.Time `json:"end"`
}
type AvailabilityDay struct {
	Date  string          `json:"date"`
	Slots []AvailableSlot `json:"slots"`
}

func civilDate(raw string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", raw)
	if err != nil || d.Format("2006-01-02") != raw || d.Year() < 1 {
		return time.Time{}, fmt.Errorf("fecha inválida")
	}
	return d, nil
}
func wallMinute(raw string, end bool) (int, error) {
	if raw == "24:00" && end {
		return 1440, nil
	}
	t, err := time.Parse("15:04", raw)
	if err != nil || t.Format("15:04") != raw {
		return 0, fmt.Errorf("hora inválida")
	}
	return t.Hour()*60 + t.Minute(), nil
}
func validateRanges(ranges []ScheduleRange) error {
	if ranges == nil {
		return fmt.Errorf("las franjas deben ser una lista explícita")
	}
	if len(ranges) > 16 {
		return fmt.Errorf("máximo técnico de 16 franjas por día")
	}
	ordered := append([]ScheduleRange(nil), ranges...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })
	last := -1
	for _, r := range ordered {
		start, err := wallMinute(r.Start, false)
		if err != nil {
			return err
		}
		end, err := wallMinute(r.End, true)
		if err != nil {
			return err
		}
		if start >= end {
			return fmt.Errorf("la franja debe terminar después del inicio")
		}
		if start < last {
			return fmt.Errorf("las franjas se superponen")
		}
		last = end
	}
	return nil
}
func (c ScheduleConfig) Validate() error {
	if (c.SlotMinutes != 0 && (c.SlotMinutes < 5 || c.SlotMinutes > 480)) || c.MinNoticeMinutes < 0 || c.MinNoticeMinutes > 525600 || c.MaxAdvanceDays < 1 || c.MaxAdvanceDays > 366 {
		return fmt.Errorf("duración, anticipación u horizonte fuera de límites técnicos")
	}
	if len(c.Weekly) > 7 || len(c.Exceptions) > 100 {
		return fmt.Errorf("configuración demasiado extensa")
	}
	seen := map[int]bool{}
	for _, day := range c.Weekly {
		if day.Weekday < 1 || day.Weekday > 7 || seen[day.Weekday] {
			return fmt.Errorf("día semanal inválido o repetido")
		}
		seen[day.Weekday] = true
		if err := c.validateBlocks(day.Ranges); err != nil {
			return err
		}
	}
	ordered := append([]ScheduleException(nil), c.Exceptions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].From < ordered[j].From })
	previous := ""
	for _, e := range ordered {
		from, err := civilDate(e.From)
		if err != nil {
			return err
		}
		through, err := civilDate(e.Through)
		if err != nil {
			return err
		}
		if through.Before(from) || len([]rune(strings.TrimSpace(e.Label))) > 160 {
			return fmt.Errorf("excepción inválida")
		}
		if e.From <= previous {
			return fmt.Errorf("las excepciones se superponen")
		}
		previous = e.Through
		if err := c.validateBlocks(e.Ranges); err != nil {
			return err
		}
	}
	return nil
}
func (c ScheduleConfig) rangesOn(date time.Time) []ScheduleRange {
	key := date.Format("2006-01-02")
	for _, e := range c.Exceptions {
		if key >= e.From && key <= e.Through {
			return e.Ranges
		}
	}
	weekday := (int(date.Weekday())+6)%7 + 1
	for _, d := range c.Weekly {
		if d.Weekday == weekday {
			return d.Ranges
		}
	}
	return nil
}

// Civil minute enumeration handles skipped dates, missing times and both
// occurrences of a repeated time without relying on time.Date's ambiguous choice.
func civilMinutes(ctx context.Context, date time.Time, loc *time.Location, visit func(time.Time, int)) error {
	year, month, day := date.Date()
	start := date.Add(-26 * time.Hour)
	end := date.Add(50 * time.Hour)
	for instant := start; instant.Before(end); instant = instant.Add(time.Minute) {
		if instant.Minute() == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		local := instant.In(loc)
		y, m, d := local.Date()
		if y == year && m == month && d == day {
			visit(instant, local.Hour()*60+local.Minute())
		}
	}
	return nil
}
func (c ScheduleConfig) slots(ctx context.Context, date time.Time, loc *time.Location, now time.Time) ([]AvailableSlot, error) {
	slots := []AvailableSlot{}
	ranges := c.rangesOn(date)
	if len(ranges) == 0 {
		return slots, nil
	}
	earliest := now.Add(time.Duration(c.MinNoticeMinutes) * time.Minute)
	latest := now.Add(time.Duration(c.MaxAdvanceDays) * 24 * time.Hour)
	err := civilMinutes(ctx, date, loc, func(instant time.Time, minute int) {
		if instant.Before(earliest) {
			return
		}
		for _, r := range ranges {
			minutes := r.SlotMinutes
			if minutes == 0 {
				minutes = c.SlotMinutes
			}
			if minutes < 5 || minutes > 480 {
				continue
			}
			duration := time.Duration(minutes) * time.Minute
			if instant.Add(duration).After(latest) {
				continue
			}
			begin, _ := wallMinute(r.Start, false)
			end, _ := wallMinute(r.End, true)
			if minute < begin || minute+minutes > end || (minute-begin)%minutes != 0 {
				continue
			}
			// Refuse slots that cross an offset jump, including a backward transition.
			_, startOffset := instant.In(loc).Zone()
			_, endOffset := instant.Add(duration - time.Nanosecond).In(loc).Zone()
			if startOffset != endOffset {
				continue
			}
			slots = append(slots, AvailableSlot{Start: instant.UTC(), End: instant.Add(duration).UTC(), blockID: r.ID, contextID: r.ContextID, SpecialtyContextID: r.ContextID})
			break
		}
	})
	return slots, err
}

func (c ScheduleConfig) validateBlocks(ranges []ScheduleRange) error {
	if err := validateRanges(ranges); err != nil {
		return err
	}
	for _, r := range ranges {
		m := r.SlotMinutes
		if m == 0 {
			m = c.SlotMinutes
		}
		if m < 5 || m > 480 {
			return fmt.Errorf("la duración de cada bloque debe estar entre 5 y 480 minutos")
		}
		if r.ID != "" {
			if _, err := decimal(r.ID, true); err != nil {
				return fmt.Errorf("identificador de bloque inválido")
			}
		}
		if r.ContextID != "" {
			if _, err := decimal(r.ContextID, true); err != nil {
				return fmt.Errorf("contexto de bloque inválido")
			}
		}
	}
	return nil
}
