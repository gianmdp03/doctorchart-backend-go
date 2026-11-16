package clinics

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
	"time"
)

func TestBookingsPostgresConcurrentCrossClinicExclusion(t *testing.T) {
	for _, modality := range []string{inPerson, teleconsultation} {
		t.Run(modality, func(t *testing.T) {
			testPostgresConcurrentExclusion(t, modality)
		})
	}
}

func testPostgresConcurrentExclusion(t *testing.T, modality string) {
	t.Helper()
	f, c, start := bookingFixture(t)
	var second Clinic
	if modality == teleconsultation {
		second = virtualClinic(t, f)
	} else {
		second = scheduleClinicFixture(t, f)
	}
	copyScheduleDirect(t, f, c.ID, second.ID, 0)
	results := make(chan error, 2)
	for _, id := range []string{c.ID, second.ID} {
		go func(id string) {
			err := insertBookingDirect(t.Context(), f, id, id, nil, start, start.Add(30*time.Minute))
			results <- err
		}(id)
	}
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
			continue
		}
		var failure *pgconn.PgError
		if errors.As(err, &failure) && failure.Code == "23P01" {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("database admitted overlapping reservations", success, conflict)
	}
}
