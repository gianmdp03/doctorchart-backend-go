package clinics

import (
	"testing"
	"time"
)

func TestScheduleSpecialHoursReplaceWeeklyAndBoundFullSlots(t *testing.T) {
	day, _ := civilDate("2026-11-02")
	config := ScheduleConfig{SlotMinutes: 45, MinNoticeMinutes: 60, MaxAdvanceDays: 1, Weekly: []ScheduleDay{{Weekday: 1, Ranges: []ScheduleRange{{Start: "09:00", End: "12:00"}}}}, Exceptions: []ScheduleException{{From: "2026-11-02", Through: "2026-11-02", Label: "Horario especial", Ranges: []ScheduleRange{{Start: "14:00", End: "15:30"}}}}}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	slots, err := config.slots(t.Context(), day, time.UTC, day.Add(13*time.Hour))
	if err != nil || len(slots) != 2 || slots[0].Start.Hour() != 14 || slots[1].End.Hour() != 15 || slots[1].End.Minute() != 30 {
		t.Fatal(slots, err)
	}
	config.Exceptions = append(config.Exceptions, ScheduleException{From: "2026-11-02", Through: "2026-11-03", Ranges: []ScheduleRange{}})
	if config.Validate() == nil {
		t.Fatal("overlapping exceptions admitted")
	}
	config.Exceptions = nil
	config.Weekly = []ScheduleDay{{Weekday: 1, Ranges: []ScheduleRange{{Start: "23:00", End: "24:00"}}}}
	if err = config.Validate(); err != nil {
		t.Fatal(err)
	}
	slots, err = config.slots(t.Context(), day, time.UTC, day.Add(23*time.Hour))
	if err != nil || len(slots) != 0 {
		t.Fatal("minimum notice ignored", slots, err)
	}
}
