package clinics

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
	"time"
)

func TestBookingsPostgresConcurrentCrossClinicExclusion(t *testing.T) {
	f, c, start := bookingFixture(t)
	second := scheduleClinicFixture(t, f)
	results := make(chan error, 2)
	for _, id := range []string{c.ID, second.ID} {
		go func(id string) {
			_, err := f.pool.Exec(t.Context(), "INSERT INTO bookings(workspace_id,clinic_id,start_at,end_at,contact_name) SELECT workspace_id,id,$2,$3,'Direct concurrent writer' FROM clinics WHERE id=$1", id, start, start.Add(30*time.Minute))
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
