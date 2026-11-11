package db

import "testing"

func TestScheduleBlocksCatalogDerivation(t *testing.T) {
	pool := fixture(t)
	if _, err := pool.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	pins := fixtureCatalogPins(t, pool)
	steps := []struct{ sql, pin string }{
		{clinicsMigration, pins.clinics}, {entitlementMigration, pins.entitlements}, {timezoneMigration, pins.timezones},
		{scheduleMigration, pins.schedules}, {bookingMigration, pins.bookings}, {attendeeMigration, pins.attendee},
		{publicBookingMigration, pins.publicBookings}, {secretaryMigration, pins.secretaries},
		{notificationMigration, pins.notifications}, {specialtyContextMigration, pins.specialtyContexts}, {patientContextMigration, pins.patientContexts},
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
	if _, err := pool.Exec(scheduleBlockMigration); err != nil {
		t.Fatal(err)
	}
	actual := digest(readString(t, pool, catalogSQL, []string{}))
	t.Logf("schedule blocks pin: %s", actual)
	if actual != pins.scheduleBlocks {
		t.Fatal("schedule blocks catalog mismatch")
	}
}
