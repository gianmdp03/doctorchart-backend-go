package config

import (
	"strings"
	"testing"
)

func TestBookingsOptInRequiresSchedules(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "postgres://fixture:synthetic@127.0.0.1:55468/fixture?sslmode=disable")
	t.Setenv("GATEWAY_SHARED_SECRET", strings.Repeat("a", 32))
	t.Setenv("SERVICES_BILLING_SHARED_SECRET", strings.Repeat("b", 32))
	t.Setenv("SERVICES_BILLING_URL", "http://127.0.0.1:18081")
	t.Setenv("SCHEDULES_ENABLED", "true")
	t.Setenv("BOOKINGS_ENABLED", "false")
	c, err := Load()
	if err != nil || c.BookingsEnabled {
		t.Fatal(c.BookingsEnabled, err)
	}
	t.Setenv("BOOKINGS_ENABLED", "true")
	c, err = Load()
	if err != nil || !c.BookingsEnabled {
		t.Fatal(c.BookingsEnabled, err)
	}
	t.Setenv("SCHEDULES_ENABLED", "false")
	if _, err = Load(); err == nil {
		t.Fatal("bookings without scheduling accepted")
	}
	t.Setenv("SCHEDULES_ENABLED", "true")
	for _, bad := range []string{"", "yes", "1", "TRUE"} {
		t.Setenv("BOOKINGS_ENABLED", bad)
		if _, err = Load(); err == nil {
			t.Fatal("invalid bookings flag accepted", bad)
		}
	}
}
