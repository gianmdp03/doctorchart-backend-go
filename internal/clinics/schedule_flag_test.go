package clinics

import "testing"

func TestScheduleFeatureFlagDefaultsToOff(t *testing.T) {
	f := newFixture(t)
	f.limit.Store(-1)
	assertCode(t, f.request("POST", "/clinics", owner, createBody), 201, "")
	assertCode(t, f.request("GET", "/clinics/1/schedule", owner, ""), 404, "")
}
