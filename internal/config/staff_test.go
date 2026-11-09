package config

import (
	"strings"
	"testing"
)

func TestStaffConfigurationContract(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "postgres://fixture:synthetic@127.0.0.1:55468/fixture?sslmode=disable")
	t.Setenv("GATEWAY_SHARED_SECRET", strings.Repeat("a", 32))
	t.Setenv("SERVICES_BILLING_SHARED_SECRET", strings.Repeat("b", 32))
	t.Setenv("SERVICES_BILLING_URL", "http://127.0.0.1:18081")
	t.Setenv("SCHEDULES_ENABLED", "true")
	t.Setenv("BOOKINGS_ENABLED", "true")
	t.Setenv("STAFF_ENABLED", "true")
	t.Setenv("STAFF_ORIGIN", "https://doctorchart.test")
	if c, err := Load(); err != nil || !c.StaffEnabled {
		t.Fatal("valid staff configuration rejected", err)
	}
	for _, origin := range []string{"http://doctorchart.test", "https://doctorchart.test/", "https://doctorchart.test/path", "https://doctorchart.test?x=1"} {
		t.Run(origin, func(t *testing.T) {
			t.Setenv("STAFF_ENABLED", "true")
			t.Setenv("STAFF_ORIGIN", origin)
			if _, err := Load(); err == nil {
				t.Fatal("unsafe origin accepted")
			}
		})
	}
}
