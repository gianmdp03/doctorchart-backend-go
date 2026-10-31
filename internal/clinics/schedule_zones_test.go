package clinics

import (
	"testing"
	"time"
)

func TestScheduleConflictAcrossInternationalDateLine(t *testing.T) {
	date, _ := civilDate("2026-11-02")
	config := func(day int, start, end string) ScheduleConfig {
		return ScheduleConfig{SlotMinutes: 30, MaxAdvanceDays: 30, Weekly: []ScheduleDay{{Weekday: day, Ranges: []ScheduleRange{{Start: start, End: end}}}}}
	}
	// Monday 00:00 at +14 is Saturday 23:00 at -11: local dates differ by two.
	agendas := []agendaRecord{{id: "1", zone: "Pacific/Kiritimati", config: config(1, "00:00", "01:00")}, {id: "2", zone: "Pacific/Pago_Pago", config: config(6, "23:00", "24:00")}}
	if err := checkAgendaConflicts(t.Context(), agendas, date.AddDate(0, 0, -2), 5); err == nil {
		t.Fatal("date-line overlap accepted")
	}
	agendas[1].config = config(7, "01:00", "02:00")
	if err := checkAgendaConflicts(t.Context(), agendas, date.AddDate(0, 0, -2), 5); err != nil {
		t.Fatal(err)
	}
	now := date.Add(9 * time.Hour)
	cfg := config(1, "09:00", "12:00")
	cfg.MinNoticeMinutes = 60
	cfg.MaxAdvanceDays = 1
	slots, err := cfg.slots(t.Context(), date, time.UTC, now)
	if err != nil || len(slots) != 4 || slots[0].Start.Before(now.Add(time.Hour)) {
		t.Fatal("notice window ignored", slots, err)
	}
}
