package clinical

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/bootstrap"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
)

// Runs only against the new, empty database created by verify_doctorchart.ps1.
func TestLiveStudyHTTPConverterAndPersistence(t *testing.T) {
	raw, address, key := os.Getenv("DOCTORCHART_CONVERTER_TEST_DATABASE_URL"), os.Getenv("DOCTORCHART_CONVERTER_TEST_URL"), os.Getenv("DOCTORCHART_CONVERTER_TEST_API_KEY")
	if raw == "" || address == "" || key == "" {
		t.Skip("disposable PostgreSQL and converter fixtures required")
	}
	db, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	ip := net.ParseIP(db.Hostname())
	if db.Path != "/doctorchart_converter_test" || (db.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		t.Fatal("test requires loopback database doctorchart_converter_test")
	}
	target, err := url.Parse(strings.TrimRight(address, "/") + "/v1/convert")
	if err != nil {
		t.Fatal(err)
	}
	ip = net.ParseIP(target.Hostname())
	if target.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Fatal("converter must be a local fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid fixture connection")
	}
	poolConfig.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("invalid fixture connection")
	}
	defer pool.Close()
	var tables int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname='public'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("HTTP fixture database must be empty; no existing data may be modified")
	}
	if err := bootstrap.EnsureSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var specialtyID, patientID int64
	if err := pool.QueryRow(ctx, `INSERT INTO specialties(active,code,created_at,display_name,navigation,patient_record_actions,patient_record_tabs,updated_at,version) VALUES(true,'CONVERTER_TEST',now(),'Fixture','[]','["PRINT_DOCUMENT"]','["SUMMARY","ENCOUNTERS","STUDIES","PRESCRIPTIONS"]',now(),0) RETURNING id`).Scan(&specialtyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO doctor_workspaces(created_at,current_step_key,definition,onboarding_completed,updated_at,user_id,version,specialty_id) VALUES(now(),'done','{"modulesConfig":{}}',true,now(),'fixture-owner',0,$1)`, specialtyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO patients(birth_date,form_definition_snapshot,form_values,formula_context,formula_results,full_name,updated_at,user_id,version,workspace_version) VALUES('2000-01-01','{}','{}','{}','{}','Synthetic Patient',now(),'fixture-owner',0,0) RETURNING id`).Scan(&patientID); err != nil {
		t.Fatal(err)
	}
	countStudies := func() int {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM patient_medical_studies").Scan(&count); err != nil {
			t.Error(err)
		}
		return count
	}
	var converterCalls, putCalls, deleteCalls atomic.Int32
	var converterFail, storageFail atomic.Bool
	var burst, revokeAfterStorage atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		converterCalls.Add(1)
		if !burst.Load() && pool.Stat().AcquiredConns() != 0 {
			t.Error("database connection held while waiting for conversion")
		}
		if r.Header.Get("Authorization") != "Bearer "+key || r.Header.Get("X-Gateway-Secret") != "" || r.Header.Get("X-User-Id") != "" {
			t.Error("backend exposed user credentials")
		}
		if converterFail.Load() {
			w.WriteHeader(503)
			return
		}
		outgoing := r.Clone(r.Context())
		outgoing.URL = target
		outgoing.RequestURI = ""
		response, err := http.DefaultTransport.RoundTrip(outgoing)
		if err != nil {
			t.Error(err)
			w.WriteHeader(502)
			return
		}
		defer response.Body.Close()
		for name, values := range response.Header {
			w.Header()[name] = values
		}
		w.WriteHeader(response.StatusCode)
		io.Copy(w, response.Body)
	}))
	defer proxy.Close()
	var mu sync.Mutex
	objects := map[string][]byte{}
	s3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "PUT":
			putCalls.Add(1)
			if storageFail.Load() {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(403)
				io.WriteString(w, "<Error><Code>AccessDenied</Code></Error>")
				return
			}
			if !burst.Load() && countStudies() != 0 && countStudies() != 1 {
				t.Error("metadata persisted before confirmed storage")
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			objects[r.URL.Path] = data
			if revokeAfterStorage.Swap(false) {
				if _, err := pool.Exec(ctx, "UPDATE patients SET user_id='revoked-owner' WHERE id=$1", patientID); err != nil {
					t.Error(err)
				}
			}
			w.Header().Set("ETag", `"fixture"`)
		case "DELETE":
			deleteCalls.Add(1)
			delete(objects, r.URL.Path)
			w.WriteHeader(204)
		default:
			w.WriteHeader(405)
		}
	}))
	defer s3.Close()
	files, err := storage.New(storage.Config{Type: "s3", Endpoint: s3.URL, Bucket: "fixture", AccessKey: "fixture", SecretKey: "fixture", Converter: storage.ConverterConfig{URL: proxy.URL, APIKey: key, AllowStudyTransformation: true}})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{pool: pool, workspaces: workspace.New(pool, nil), files: files, zone: time.UTC}
	mux := http.NewServeMux()
	service.StudyRoutes(mux)
	service.PrescriptionRoutes(mux)
	service.PrintRoutes(mux)
	gatewayKey := strings.Repeat("g", 64)
	handler := api.Gateway(gatewayKey, api.Dispatch(mux))
	img := image.NewNRGBA(image.Rect(0, 0, 2400, 1000))
	seed := uint32(7)
	for i := 0; i < len(img.Pix); i += 4 {
		seed = seed*1664525 + 1013904223
		copy(img.Pix[i:i+4], []byte{uint8(seed), uint8(seed >> 8), uint8(seed >> 16), 255})
	}
	var original bytes.Buffer
	if err := png.Encode(&original, img); err != nil {
		t.Fatal(err)
	}
	upload := func(user, role, description string, authenticated bool) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "study.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(original.Bytes()); err != nil {
			t.Fatal(err)
		}
		// An Angular-provided profile must not override DoctorChart's selection.
		if err := writer.WriteField("profile", "avatar"); err != nil {
			t.Fatal(err)
		}
		if err := writer.WriteField("description", description); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", fmt.Sprintf("/api/doctorchart/patients/%d/studies", patientID), &body).WithContext(ctx)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if authenticated {
			req.Header.Set("X-Gateway-Secret", gatewayKey)
			req.Header.Set("X-User-Id", user)
			req.Header.Set("X-User-Role", role)
			req.Header.Set("X-User-Plan", "PRO")
			req.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	for _, test := range []struct {
		user, role    string
		authenticated bool
		status        int
	}{{"fixture-owner", "DOCTOR", false, 401}, {"other-owner", "DOCTOR", true, 403}, {"fixture-owner", "VIEWER", true, 403}} {
		response := upload(test.user, test.role, "", test.authenticated)
		if response.Code != test.status || converterCalls.Load() != 0 || putCalls.Load() != 0 || countStudies() != 0 {
			t.Fatalf("authorization bypass: %d %s", response.Code, response.Body.String())
		}
	}
	storageFail.Store(true)
	response := upload("fixture-owner", "DOCTOR", "storage failure", true)
	if response.Code != 503 || countStudies() != 0 {
		t.Fatalf("storage failure wrote metadata: %d %s", response.Code, response.Body.String())
	}
	storageFail.Store(false)
	before := putCalls.Load()
	converterFail.Store(true)
	response = upload("fixture-owner", "DOCTOR", "conversion failure", true)
	if response.Code != 503 || countStudies() != 0 || putCalls.Load() != before {
		t.Fatalf("conversion failure persisted: %d %s", response.Code, response.Body.String())
	}
	converterFail.Store(false)
	response = upload("fixture-owner", "DOCTOR", "successful conversion", true)
	if response.Code != 201 || countStudies() != 1 {
		t.Fatalf("HTTP upload failed: %d %s", response.Code, response.Body.String())
	}
	var result study
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	var storedKey, name, mimeType string
	var size int64
	if err := pool.QueryRow(ctx, "SELECT stored_file_name,original_file_name,content_type,file_size FROM patient_medical_studies WHERE id=$1", result.ID).Scan(&storedKey, &name, &mimeType, &size); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	stored := objects["/fixture/"+storedKey]
	mu.Unlock()
	if name != "study.webp" || mimeType != "image/webp" || size != int64(len(stored)) || result.Size != size || size >= int64(original.Len()) {
		t.Fatal("database metadata does not match stored conversion")
	}
	// Force a database failure after storage and verify the existing rollback cleanup.
	if _, err := pool.Exec(ctx, "ALTER TABLE patient_medical_studies ADD CONSTRAINT converter_test_rejection CHECK (description IS DISTINCT FROM 'force-rollback')"); err != nil {
		t.Fatal(err)
	}
	response = upload("fixture-owner", "DOCTOR", "force-rollback", true)
	mu.Lock()
	objectCount := len(objects)
	mu.Unlock()
	if response.Code != 409 || countStudies() != 1 || objectCount != 1 || deleteCalls.Load() != 1 {
		t.Fatalf("rollback left metadata/object: %d %s", response.Code, response.Body.String())
	}
	// Recheck authorization after queueing/storage and clean up if it changed.
	revokeAfterStorage.Store(true)
	response = upload("fixture-owner", "DOCTOR", "permission changed", true)
	mu.Lock()
	objectCount = len(objects)
	mu.Unlock()
	if response.Code != 403 || countStudies() != 1 || objectCount != 1 || deleteCalls.Load() != 2 {
		t.Fatalf("changed ownership was accepted or left an object: %d %s", response.Code, response.Body.String())
	}
	if _, err := pool.Exec(ctx, "UPDATE patients SET user_id='fixture-owner' WHERE id=$1", patientID); err != nil {
		t.Fatal(err)
	}
	// Twenty concurrent uploads must complete even with only two DB connections.
	// Small images test the complete protocol; the separate load probe uses large inputs.
	original.Reset()
	if err := png.Encode(&original, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	burst.Store(true)
	var uploads sync.WaitGroup
	statuses := make(chan int, 20)
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		uploads.Add(1)
		go func() {
			defer uploads.Done()
			<-start
			statuses <- upload("fixture-owner", "DOCTOR", "queued burst", true).Code
		}()
	}
	close(start)
	uploads.Wait()
	close(statuses)
	for status := range statuses {
		if status != 201 {
			t.Fatalf("burst rejected an upload: %d", status)
		}
	}
	mu.Lock()
	objectCount = len(objects)
	mu.Unlock()
	if countStudies() != 21 || objectCount != 21 {
		t.Fatalf("burst persistence mismatch: rows=%d objects=%d", countStudies(), objectCount)
	}
	// These reads/writes use real baseline tables, including document children
	// and PDF queries; mocked SQL cannot establish their schema compatibility.
	documentRequest := func(method, path, user, role, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Gateway-Secret", gatewayKey)
		r.Header.Set("X-User-Id", user)
		r.Header.Set("X-User-Role", role)
		r.Header.Set("X-User-Plan", "PRO")
		r.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	documentPath := fmt.Sprintf("/api/doctorchart/patients/%d/prescriptions", patientID)
	prescriptionBody := `{"type":"PRESCRIPTION","diagnosisDescription":"Synthetic diagnosis","prescriptions":[{"medicationName":"Synthetic medicine","dosage":"5 mg","administrationRoute":"oral","frequency":"daily","durationDays":5}],"diagnosticOrders":[]}`
	for _, actor := range []struct{ user, role string }{{"other-owner", "DOCTOR"}, {"fixture-owner", "VIEWER"}} {
		if w := documentRequest("POST", documentPath, actor.user, actor.role, prescriptionBody); w.Code != 403 {
			t.Fatal("document creation bypassed authorization", w.Code)
		}
	}
	w := documentRequest("POST", documentPath, "fixture-owner", "DOCTOR", prescriptionBody)
	var prescription Prescription
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &prescription) != nil || len(prescription.Medications) != 1 {
		t.Fatal("baseline prescription creation failed", w.Code, w.Body.String())
	}
	recordPath := fmt.Sprintf("%s/%d", documentPath, prescription.ID)
	w = documentRequest("GET", recordPath+"/print", "fixture-owner", "DOCTOR", "")
	var printed printDocument
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &printed) != nil || printed.Patient.Name != "Synthetic Patient" || printed.ID != prescription.ID {
		t.Fatal("baseline printable document read failed", w.Code, w.Body.String())
	}
	w = documentRequest("GET", recordPath+"/pdf", "fixture-owner", "DOCTOR", "")
	if w.Code != 200 || !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF")) || w.Header().Get("Content-Type") != "application/pdf" || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("baseline private PDF generation failed", w.Code)
	}
	if w := documentRequest("GET", recordPath+"/pdf", "other-owner", "DOCTOR", ""); w.Code != 403 {
		t.Fatal("foreign owner read a PDF", w.Code)
	}
	ordersBody := `{"type":"STUDY_ORDERS","prescriptions":[],"diagnosticOrders":["Synthetic laboratory order"]}`
	w = documentRequest("POST", documentPath, "fixture-owner", "DOCTOR", ordersBody)
	var order Prescription
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &order) != nil || len(order.Orders) != 1 {
		t.Fatal("baseline study order creation failed", w.Code, w.Body.String())
	}
	if w := documentRequest("POST", documentPath, "fixture-owner", "DOCTOR", strings.Replace(prescriptionBody, `"durationDays":5`, `"durationDays":0`, 1)); w.Code != 400 {
		t.Fatal("invalid document accepted", w.Code)
	}
	var records, medications, orders int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM patient_prescription_records),(SELECT count(*) FROM prescription_medications),(SELECT count(*) FROM prescription_diagnostic_orders)`).Scan(&records, &medications, &orders); err != nil || records != 2 || medications != 1 || orders != 1 {
		t.Fatal("document children or rejected writes mismatch", records, medications, orders, err)
	}
	if w := documentRequest("DELETE", recordPath, "fixture-owner", "DOCTOR", ""); w.Code != 204 {
		t.Fatal("baseline document deletion failed", w.Code)
	}
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM patient_prescription_records),(SELECT count(*) FROM prescription_medications),(SELECT count(*) FROM prescription_diagnostic_orders)`).Scan(&records, &medications, &orders); err != nil || records != 1 || medications != 0 || orders != 1 {
		t.Fatal("document deletion left child rows or removed other documents", records, medications, orders, err)
	}
}
