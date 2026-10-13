package clinical

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
)

func TestQuotaRecoveryDoesNotHoldDatabaseLocks(t *testing.T) {
	for _, operation := range []string{"patient", "encounter"} {
		t.Run(operation, func(t *testing.T) {
			services, pool := integrityFixture(t, 5, 5)
			service := services[0]
			patient := integrityPatient(t, service)
			entered := make(chan struct{})
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-r.Context().Done()
			}))
			defer provider.Close()
			service.rules = billing.New(provider.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				identity := api.Identity{UserID: "integrity-owner", Plan: "BASIC"}
				if operation == "patient" {
					_, err := service.CreatePatient(ctx, identity, integrityPatientRequest())
					result <- err
				} else {
					_, err := service.CreateEncounter(ctx, identity, integrityEncounterRequest(patient.ID, "FIRST_VISIT"))
					result <- err
				}
			}()
			select {
			case <-entered:
			case err := <-result:
				t.Fatalf("quota operation skipped recovery: %v", err)
			case <-ctx.Done():
				t.Fatal("quota operation never reached Billing")
			}

			// This independently acquired transaction must be able to lock both
			// quota rows while the network request remains blocked.
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(ctx, "SELECT id FROM doctor_workspaces WHERE user_id='integrity-owner' FOR UPDATE NOWAIT"); err != nil {
				t.Fatalf("Billing recovery holds the workspace quota lock: %v", err)
			}
			if _, err := tx.Exec(ctx, "SELECT id FROM patients WHERE id=$1 FOR UPDATE NOWAIT", patient.ID); err != nil {
				t.Fatalf("Billing recovery holds the patient quota lock: %v", err)
			}
			if acquired := service.pool.Stat().AcquiredConns(); acquired != 0 {
				t.Fatalf("Billing recovery retains %d PostgreSQL connections", acquired)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			cancel()
			select {
			case err := <-result:
				integrityError(t, err, 503, "BILLING_UNAVAILABLE")
			case <-time.After(time.Second):
				t.Fatal("cancelled recovery did not release the operation")
			}
			var patients, encounters int
			if err := pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM patients),(SELECT count(*) FROM clinical_encounters)").Scan(&patients, &encounters); err != nil || patients != 1 || encounters != 0 {
				t.Fatalf("cancelled recovery persisted data: patients=%d encounters=%d err=%v", patients, encounters, err)
			}
		})
	}
}

func TestQuotaAvailabilityOfflinePolicies(t *testing.T) {
	for _, policy := range []struct {
		name     string
		identity api.Identity
		status   int
		code     string
		calls    int32
	}{
		{"paid_unavailable", api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, 503, "BILLING_UNAVAILABLE", 2},
		{"none", api.Identity{UserID: "integrity-owner", Plan: "NONE"}, 403, "FORBIDDEN", 0},
		{"free_trial", api.Identity{UserID: "integrity-owner", Plan: "FREE_TRIAL"}, 0, "", 0},
		{"admin_casefold", api.Identity{UserID: "integrity-owner", Plan: "NONE", Roles: []string{"role_super_admin"}}, 0, "", 0},
	} {
		t.Run(policy.name, func(t *testing.T) {
			services, pool := integrityFixture(t, 5, 5)
			service := services[0]
			patient := integrityPatient(t, service)
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer provider.Close()
			service.rules = billing.New(provider.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, patientErr := service.CreatePatient(ctx, policy.identity, integrityPatientRequest())
			_, encounterErr := service.CreateEncounter(ctx, policy.identity, integrityEncounterRequest(patient.ID, "FIRST_VISIT"))
			if policy.status != 0 {
				integrityError(t, patientErr, policy.status, policy.code)
				integrityError(t, encounterErr, policy.status, policy.code)
			} else if patientErr != nil || encounterErr != nil {
				t.Fatalf("approved offline policy failed: patient=%v encounter=%v", patientErr, encounterErr)
			}
			if calls.Load() != policy.calls {
				t.Fatalf("unexpected Billing HTTP requests: %d, want %d", calls.Load(), policy.calls)
			}
			var patients, encounters int
			if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM patients),(SELECT count(*) FROM clinical_encounters)").Scan(&patients, &encounters); err != nil {
				t.Fatal(err)
			}
			expectedPatients, expectedEncounters := 1, 0
			if policy.status == 0 {
				expectedPatients, expectedEncounters = 2, 1
			}
			if patients != expectedPatients || encounters != expectedEncounters {
				t.Fatalf("persisted patients=%d encounters=%d, want %d/%d", patients, encounters, expectedPatients, expectedEncounters)
			}
			if policy.name == "free_trial" && (service.rules.Int("FREE_TRIAL", "max_patients", -1) != 20 || service.rules.Int("FREE_TRIAL", "max_records_per_patient", -1) != 50) {
				t.Fatal("approved static FREE_TRIAL quotas changed")
			}
		})
	}
}
