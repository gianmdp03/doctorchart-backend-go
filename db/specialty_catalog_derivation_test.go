package db

import "testing"

// Derive the new pin independently, using only published SQL in an empty owned
// fixture, never a development or restored database. Prior pins are validated.
func TestSpecialtyContextCatalogDerivation(t *testing.T) {
	pool := fixture(t)
	if _, err := pool.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	pins := fixtureCatalogPins(t, pool)
	if digest(readString(t, pool, catalogSQL, []string{})) != pins.baseline {
		t.Fatal("baseline drift")
	}
	steps := []struct{ sql, pin string }{
		{clinicsMigration, pins.clinics}, {entitlementMigration, pins.entitlements},
		{timezoneMigration, pins.timezones}, {scheduleMigration, pins.schedules},
		{bookingMigration, pins.bookings}, {attendeeMigration, pins.attendee},
		{publicBookingMigration, pins.publicBookings}, {secretaryMigration, pins.secretaries},
		{notificationMigration, pins.notifications},
	}
	for _, step := range steps {
		if _, err := pool.Exec(step.sql); err != nil {
			t.Fatal(err)
		}
		if digest(readString(t, pool, catalogSQL, []string{})) != step.pin {
			t.Fatal("published migration drift")
		}
	}
	if _, err := pool.Exec(specialtyContextMigration); err != nil {
		t.Fatal(err)
	}
	actual := digest(readString(t, pool, catalogSQL, []string{}))
	t.Logf("specialty contexts pin: %s", actual)
	if actual != pins.specialtyContexts {
		t.Fatal("context migration drift")
	}
}
