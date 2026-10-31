package config

import (
	"strings"
	"testing"
)

func TestPublicBookingsRemainExplicitOptIn(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "postgres://fixture:synthetic@127.0.0.1:55468/fixture?sslmode=disable")
	t.Setenv("GATEWAY_SHARED_SECRET", strings.Repeat("a", 32))
	t.Setenv("SERVICES_BILLING_SHARED_SECRET", strings.Repeat("b", 32))
	t.Setenv("SERVICES_BILLING_URL", "http://127.0.0.1:18081")
	t.Setenv("PUBLIC_BOOKINGS_ENABLED", "false")
	c, err := Load()
	if err != nil || c.PublicBookingsEnabled {
		t.Fatal(err)
	}
	t.Setenv("PUBLIC_BOOKINGS_ENABLED", "true")
	t.Setenv("BOOKINGS_ENABLED", "false")
	if _, err = Load(); err == nil {
		t.Fatal("public without M4 accepted")
	}
	t.Setenv("BOOKINGS_ENABLED", "true")
	t.Setenv("SCHEDULES_ENABLED", "true")
	c, err = Load()
	if err != nil || !c.PublicBookingsEnabled {
		t.Fatal(err)
	}
	t.Setenv("PUBLIC_BOOKINGS_ENABLED", "TRUE")
	if _, err = Load(); err == nil {
		t.Fatal("invalid public flag accepted")
	}
}
