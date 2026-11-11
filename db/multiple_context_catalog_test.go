package db

import "testing"

func TestMultipleContextCatalogDerivation(t *testing.T) {
	pool := fixture(t)
	if _, err := pool.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	pins := fixtureCatalogPins(t, pool)
	steps := []struct{ sql, pin string }{
		{clinicsMigration, pins.clinics}, {entitlementMigration, pins.entitlements}, {timezoneMigration, pins.timezones},
		{scheduleMigration, pins.schedules}, {bookingMigration, pins.bookings}, {attendeeMigration, pins.attendee},
		{publicBookingMigration, pins.publicBookings}, {secretaryMigration, pins.secretaries}, {notificationMigration, pins.notifications},
		{specialtyContextMigration, pins.specialtyContexts}, {patientContextMigration, pins.patientContexts}, {scheduleBlockMigration, pins.scheduleBlocks}, {bookingContextMigration, pins.bookingContexts},
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
	if _, err := pool.Exec(multipleContextMigration); err != nil {
		t.Fatal(err)
	}
	actual := digest(readString(t, pool, catalogSQL, []string{}))
	t.Logf("multiple contexts pin: %s", actual)
	if actual != pins.multipleContexts {
		t.Fatal("booking context catalog mismatch")
	}
}
