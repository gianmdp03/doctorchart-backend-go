package clinics

import (
	"context"
	"testing"
	"time"
)

func TestScheduleCivilTime(t *testing.T) {
	for _, tc := range []struct {
		name, date, zone string
		ranges           []ScheduleRange
		want             int
	}{
		{"split", "2026-11-02", "UTC", []ScheduleRange{{Start: "09:00", End: "10:00"}, {Start: "14:00", End: "15:00"}}, 4},
		{"spring gap", "2026-03-08", "America/New_York", []ScheduleRange{{Start: "01:00", End: "04:00"}}, 4},
		{"fall repeated", "2026-11-01", "America/New_York", []ScheduleRange{{Start: "01:00", End: "02:00"}}, 4},
		{"skipped civil date", "2011-12-30", "Pacific/Apia", []ScheduleRange{{Start: "09:00", End: "10:00"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			day, err := civilDate(tc.date)
			if err != nil {
				t.Fatal(err)
			}
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			config := ScheduleConfig{SlotMinutes: 30, MaxAdvanceDays: 366, Weekly: []ScheduleDay{{Weekday: (int(day.Weekday())+6)%7 + 1, Ranges: tc.ranges}}}
			if err := config.Validate(); err != nil {
				t.Fatal(err)
			}
			result, err := config.slots(t.Context(), day, loc, day.Add(-24*time.Hour))
			if err != nil || len(result) != tc.want {
				t.Fatalf("slots=%v err=%v want=%d", result, err, tc.want)
			}
			for i := 1; i < len(result); i++ {
				if !result[i].Start.After(result[i-1].Start) {
					t.Fatal("duplicate or unordered instant")
				}
			}
		})
	}
}
func TestScheduleValidationAndExceptions(t *testing.T) {
	day, _ := civilDate("2026-11-02")
	config := ScheduleConfig{SlotMinutes: 30, MaxAdvanceDays: 1, Weekly: []ScheduleDay{{Weekday: 1, Ranges: []ScheduleRange{{Start: "09:00", End: "12:00"}}}}, Exceptions: []ScheduleException{{From: "2026-11-02", Through: "2026-11-03", Label: "Vacaciones", Ranges: []ScheduleRange{}}}}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	result, err := config.slots(t.Context(), day, time.UTC, day)
	if err != nil || len(result) != 0 {
		t.Fatal(result, err)
	}
	config.Exceptions = nil
	config.Weekly[0].Ranges = append(config.Weekly[0].Ranges, ScheduleRange{Start: "11:00", End: "13:00"})
	if config.Validate() == nil {
		t.Fatal("overlap accepted")
	}
	for _, bad := range []string{"2026-02-30", "2026-1-1", "invalid"} {
		if _, err := civilDate(bad); err == nil {
			t.Fatal(bad)
		}
	}
	for _, bad := range []string{"9:00", "25:00", "24:00", "09:60"} {
		if _, err := wallMinute(bad, false); err == nil {
			t.Fatal(bad)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := civilMinutes(ctx, day, time.UTC, func(time.Time, int) {}); err == nil {
		t.Fatal("cancellation ignored")
	}
}
