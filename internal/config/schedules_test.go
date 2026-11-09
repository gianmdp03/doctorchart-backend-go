package config

import (
	"os"
	"strings"
	"testing"
)

func TestSchedulesOptInIsExplicitAndValidated(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "postgres://fixture:synthetic@127.0.0.1:55468/fixture?sslmode=disable")
	t.Setenv("GATEWAY_SHARED_SECRET", strings.Repeat("a", 32))
	t.Setenv("SERVICES_BILLING_SHARED_SECRET", strings.Repeat("b", 32))
	t.Setenv("SERVICES_BILLING_URL", "http://127.0.0.1:18081")
	t.Setenv("SCHEDULES_ENABLED", "false")
	if err := os.Unsetenv("SCHEDULES_ENABLED"); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil || c.SchedulesEnabled {
		t.Fatal("M3 must default off", c.SchedulesEnabled, err)
	}
	for _, value := range []string{"false", "true"} {
		t.Setenv("SCHEDULES_ENABLED", value)
		c, err = Load()
		if err != nil || c.SchedulesEnabled != (value == "true") {
			t.Fatal(value, c.SchedulesEnabled, err)
		}
	}
	for _, value := range []string{"", "1", "TRUE", "maybe"} {
		t.Setenv("SCHEDULES_ENABLED", value)
		if _, err = Load(); err == nil {
			t.Fatal("invalid flag accepted", value)
		}
	}
}
