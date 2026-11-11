package db

import "testing"

func TestPatientContextCatalogDerivation(t *testing.T) {
	pool := fixture(t)
	if _, err := pool.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	pins := fixtureCatalogPins(t, pool)
	steps := []struct{ sql, pin string }{
		{clinicsMigration, pins.clinics}, {entitlementMigration, pins.entitlements}, {timezoneMigration, pins.timezones},
		{scheduleMigration, pins.schedules}, {bookingMigration, pins.bookings}, {attendeeMigration, pins.attendee},
		{publicBookingMigration, pins.publicBookings}, {secretaryMigration, pins.secretaries},
		{notificationMigration, pins.notifications}, {specialtyContextMigration, pins.specialtyContexts},
	}
	if digest(readString(t, pool, catalogSQL, []string{})) != pins.baseline {
		t.Fatal("baseline drift")
	}
	for _, step := range steps {
		if _, err := pool.Exec(step.sql); err != nil {
			t.Fatal(err)
		}
		if digest(readString(t, pool, catalogSQL, []string{})) != step.pin {
			t.Fatal("published migration drift")
		}
	}
	if _, err := pool.Exec(patientContextMigration); err != nil {
		t.Fatal(err)
	}
	actual := digest(readString(t, pool, catalogSQL, []string{}))
	t.Logf("patient contexts pin: %s", actual)
	if actual != pins.patientContexts {
		t.Fatal("patient context catalog mismatch")
	}
}
