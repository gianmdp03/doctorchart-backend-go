package db_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
	"github.com/syntah/doctorchart-backend-go/internal/clinical"
	"github.com/syntah/doctorchart-backend-go/internal/clinics"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

func TestBaseline16CopyApplicationAndArchivedCalculations(t *testing.T) {
	source, target := db.BaselineCopyFixture(t)
	ctx := t.Context()
	raw, err := os.ReadFile("../internal/bootstrap/seed.json")
	if err != nil {
		t.Fatal(err)
	}
	var seed struct {
		Wizards []struct {
			Published json.RawMessage `json:"published_definition"`
		} `json:"wizard_defaults"`
	}
	if err := json.Unmarshal(raw, &seed); err != nil || len(seed.Wizards) == 0 {
		t.Fatalf("read wizard fixture: %v", err)
	}
	definition := seed.Wizards[0].Published
	values := `{"chiefComplaint":"Legacy visit","weightKg":20,"heightCm":100}`
	if _, err := source.Exec(ctx, `UPDATE doctor_specialty_contexts SET definition=$1,source_wizard_version=1 WHERE id=1`, definition); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, `UPDATE clinical_encounters SET form_definition_snapshot=$1::jsonb->'encounterForm',
		form_values=$2::jsonb, chief_complaint='Legacy visit' WHERE id=1`, definition, values); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, `UPDATE specialties SET display_name='Renamed after booking',
		navigation='["CALCULATORS"]', patient_record_tabs='["STUDIES","PRESCRIPTIONS"]',
		patient_record_actions='["EDIT_PATIENT","PRINT_DOCUMENT","NEW_ENCOUNTER"]' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// The storage copy keeps the immutable key referenced by both databases.
	var file bytes.Buffer
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "", 12)
	pdf.Cell(0, 10, "Synthetic baseline 16 study")
	if err := pdf.Output(&file); err != nil {
		t.Fatal(err)
	}
	const key = "00000000-0000-4000-8000-000000000003"
	if _, err := source.Exec(ctx, "UPDATE patient_medical_studies SET stored_file_name=$1, media_asset_id=NULL, file_size=$2 WHERE id=1", key, file.Len()); err != nil {
		t.Fatal(err)
	}
	originFile := filepath.Join(t.TempDir(), key)
	if err := os.WriteFile(originFile, file.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CopyBaseline16(ctx, source, target); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := os.Chmod(destination, 0700); err != nil {
		t.Fatal(err)
	}
	copiedFile, err := os.ReadFile(originFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, key), copiedFile, 0600); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(storage.Config{Type: "local", Directory: destination})
	if err != nil {
		t.Fatal(err)
	}
	config := target.Config()
	config.ConnConfig.User = db.AppRole
	config.ConnConfig.Password = os.Getenv("DOCTORCHART_TEST_APP_PASSWORD")
	app, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, err := db.CheckRuntime(ctx, app, db.AppRole); err != nil {
		t.Fatal(err)
	}
	forms, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	service, err := clinical.New(app, workspace.New(app, forms), forms, billing.New(""), files)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	service.Routes(mux)
	service.EncounterRoutes(mux)
	service.StudyRoutes(mux)
	service.PrintRoutes(mux)
	service.CalculatorRoutes(mux)
	clinics.NewWithBookings(app, nil, true, true).Routes(mux)
	handler := api.Gateway("copy-fixture", api.Dispatch(mux))
	call := func(method, path, owner, contextID, body string, expected int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/doctorchart"+path, strings.NewReader(body))
		if contextID != "" {
			query := req.URL.Query()
			query.Set("specialtyContextId", contextID)
			req.URL.RawQuery = query.Encode()
		}
		req.Header.Set("X-Gateway-Secret", "copy-fixture")
		if owner != "" {
			req.Header.Set("X-User-Id", owner)
			req.Header.Set("X-User-Role", "SUPER_ADMIN")
		}
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != expected {
			t.Fatalf("%s %s: received %d, want %d: %s", method, path, response.Code, expected, response.Body)
		}
		return response
	}
	const owner = "m1-owner"
	const currentFile = "/patients/1/studies/1/file"
	const archivedFile = "/patients/1/revisions/studies/1/file"
	const history = "/patients/1/revisions"
	const calculations = history + "/calculations"
	call("GET", "/patients/1", owner, "1", "", 200)
	call("GET", "/patients/1/prescriptions/1/print", owner, "1", "", 200)
	if active := call("GET", currentFile, owner, "1", "", 200); !bytes.Equal(active.Body.Bytes(), file.Bytes()) {
		t.Fatal("migrated study returned different bytes")
	}
	booking := call("GET", "/bookings/1", owner, "", "", 200)
	if !strings.Contains(booking.Body.String(), `"patientId":"1"`) || !strings.Contains(booking.Body.String(), `"name":"Fixture"`) {
		t.Fatal("booking link or historical specialty snapshot changed")
	}
	call("GET", "/bookings/1", "m1-other", "", "", 404)
	call("GET", "/clinics/1/schedule", owner, "", "", 200)

	before := call("GET", "/encounters/1", owner, "1", "", 200)
	if !strings.Contains(before.Body.String(), "Legacy visit") {
		t.Fatal("previous clinical snapshot is unreadable")
	}
	if archived := call("GET", calculations, owner, "1", "", 200); !strings.Contains(archived.Body.String(), `"items":[]`) {
		t.Fatal("active calculation leaked into archive")
	}
	call("PUT", "/encounters/1", owner, "1", `{"expectedVersion":2,"consultationType":"FIRST_VISIT",
		"values":{"chiefComplaint":"Corrected visit","weightKg":20,"heightCm":100},"reason":"Synthetic correction"}`, 200)
	if revisions := call("GET", history, owner, "1", "", 200); !strings.Contains(revisions.Body.String(), `"firstRecorded":true`) ||
		!strings.Contains(revisions.Body.String(), "Legacy visit") {
		t.Fatal("first revision did not identify and preserve the previous state")
	}
	call("DELETE", "/encounters/1", owner, "1", `{"expectedVersion":3,"reason":"Synthetic annulment"}`, 204)
	call("GET", "/encounters/1", owner, "1", "", 404)
	archived := call("GET", calculations+"?encounterId=1", owner, "1", "", 200)
	if !strings.Contains(archived.Body.String(), "Original calculation") {
		t.Fatal("calculation disappeared after encounter annulment")
	}
	call("GET", calculations, owner, "3", "", 404)
	call("GET", calculations, "m1-other", "2", "", 404)
	call("GET", calculations, "", "1", "", 401)

	if _, err := target.Exec(ctx, `UPDATE specialties SET navigation='["PATIENTS"]' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("GET", calculations, owner, "1", "", 403)
	if _, err := target.Exec(ctx, `UPDATE specialties SET navigation='["CALCULATORS"]' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("DELETE", "/patients/1", owner, "1", `{"expectedVersion":4,"reason":"Synthetic withdrawal"}`, 204)
	call("GET", currentFile, owner, "1", "", 404)
	if previous := call("GET", archivedFile, owner, "1", "", 200); !bytes.Equal(previous.Body.Bytes(), file.Bytes()) {
		t.Fatal("archived migrated study returned different bytes")
	}
	call("GET", archivedFile, owner, "3", "", 404)
	call("GET", "/bookings/1", owner, "", "", 200)
	if !strings.Contains(call("GET", calculations, owner, "1", "", 200).Body.String(), "Original calculation") {
		t.Fatal("calculation disappeared after patient annulment")
	}
}
