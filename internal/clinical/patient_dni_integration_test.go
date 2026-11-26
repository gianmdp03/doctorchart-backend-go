package clinical

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

func TestPatientDNIConcurrentCreateAndScopedLookup(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	owner := api.Identity{UserID: "integrity-owner", Plan: "BASIC"}
	other := api.Identity{UserID: "integrity-other", Plan: "BASIC"}
	results := integrityConcurrent(t, func(ctx context.Context, i int) error {
		request := integrityPatientRequest()
		if i == 0 {
			request["values"].(map[string]any)["sys_dni"] = " 12345678 "
		}
		_, err := services[i].CreatePatient(ctx, owner, request)
		return err
	})
	integrityOneRejected(t, results, 409, "PATIENT_DNI_ALREADY_EXISTS")

	ctx := t.Context()
	setup, err := services[0].workspaces.Setup(ctx, owner.UserID)
	if err != nil {
		t.Fatal(err)
	}
	otherSetup, err := services[0].workspaces.Setup(ctx, other.UserID)
	if err != nil {
		t.Fatal(err)
	}
	config := pool.Config()
	config.ConnConfig.User = db.AppRole
	config.ConnConfig.Password = os.Getenv("DOCTORCHART_TEST_APP_PASSWORD")
	runtimePool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimePool.Close()
	service, err := New(runtimePool, workspace.New(runtimePool, services[0].forms), services[0].forms, services[0].rules, services[0].files)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	service.Routes(mux)
	handler := api.Gateway("projection-fixture", mux)
	const lookup = "/patients/check-dni"
	dni := map[string]any{"nationalId": "12345678"}
	projectionFixtureCall(t, handler, "POST", lookup, owner.UserID, setup.ContextID, map[string]any{"nationalId": "12345"}, 400)
	projectionFixtureCall(t, handler, "POST", lookup, other.UserID, setup.ContextID, dni, 404)
	check := func(actor string, scope int64) *int64 {
		t.Helper()
		response := projectionFixtureCall(t, handler, "POST", lookup, actor, scope, dni, 200)
		return projectionFixtureDecode[struct {
			PatientID *int64
		}](t, response.Body.Bytes()).PatientID
	}
	if id := check(other.UserID, otherSetup.ContextID); id != nil {
		t.Fatalf("other doctor saw a patient: %d", *id)
	}
	id := check(owner.UserID, setup.ContextID)
	if id == nil {
		t.Fatal("lookup did not find the committed patient")
	}

	request := integrityPatientRequest()
	request["values"].(map[string]any)["sys_dni"] = "87654321"
	patient, err := services[0].CreatePatient(ctx, owner, request)
	if err != nil {
		t.Fatal(err)
	}
	update := map[string]any{"expectedVersion": json.Number("0"), "values": integrityPatientRequest()["values"]}
	_, err = services[0].UpdatePatient(ctx, owner.UserID, patient.ID, update)
	integrityError(t, err, 409, "PATIENT_DNI_ALREADY_EXISTS")
	stored, err := services[0].GetPatient(ctx, owner.UserID, patient.ID)
	if err != nil || stored.Version != patient.Version || stored.NationalID == nil || *stored.NationalID != "87654321" {
		t.Fatalf("duplicate edit changed the stored patient: %#v, %v", stored, err)
	}

	if _, err := services[0].CreatePatient(ctx, other, integrityPatientRequest()); err != nil {
		t.Fatalf("independent doctor could not register the DNI: %v", err)
	}
	if err := services[0].annulPatient(ctx, owner.UserID, *id, annulment{reason: "Synthetic annulment"}); err != nil {
		t.Fatal(err)
	}
	if id := check(owner.UserID, setup.ContextID); id != nil {
		t.Fatalf("lookup returned an annulled patient: %d", *id)
	}
	if _, err := services[0].CreatePatient(ctx, owner, integrityPatientRequest()); err != nil {
		t.Fatalf("an annulled record blocked reuse of the DNI: %v", err)
	}
}
