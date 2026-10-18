package clinical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	common "github.com/bycode/bycode-image-converter-go/pkg/media"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
)

func TestManagedStudyHTTPRecoveryAndRetention(t *testing.T) {
	services, pool := integrityFixture(t, 10, 10)
	service := services[0]
	ctx := t.Context()
	// The real runtime shares the pool between pgx domain transactions and
	// database/sql media transactions. One connection proves no nested acquire.
	poolConfig := service.pool.Config()
	poolConfig.MaxConns = 1
	constrainedPool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(constrainedPool.Close)
	service.pool = constrainedPool
	if _, err := pool.Exec(ctx, `UPDATE specialties SET patient_record_tabs=patient_record_tabs || '["STUDIES"]'::jsonb WHERE id=(SELECT specialty_id FROM doctor_workspaces WHERE user_id='integrity-owner') AND NOT patient_record_tabs @> '["STUDIES"]'::jsonb`); err != nil {
		t.Fatal(err)
	}
	patient := integrityPatient(t, service)
	var patientIdentity string
	if err := pool.QueryRow(ctx, "SELECT media_identity::text FROM patients WHERE id=$1", patient.ID).Scan(&patientIdentity); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	original := encoded.Bytes()
	converterKey := strings.Repeat("c", 64)
	converter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+converterKey || r.Header.Get("X-User-Id") != "" {
			t.Error("converter credential isolation failed")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		input, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		data, err := io.ReadAll(input)
		input.Close()
		if err != nil || !bytes.Equal(data, original) || header.Filename != "original" || r.FormValue("profile") != "doctorchart-study" {
			t.Error("unexpected clinical conversion contract", err)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Image-Profile", "doctorchart-study")
		w.Header().Set("X-Image-Preserved", "true")
		w.Header().Set("X-Image-Width", "3")
		w.Header().Set("X-Image-Height", "2")
		_, _ = w.Write(original)
	}))
	t.Cleanup(converter.Close)
	cfg := common.Config{Environment: "test", Service: "doctorchart", Policy: common.DeletionPolicy{RetainFor: 72 * time.Hour}, Objects: common.ObjectConfig{Provider: "local", Directory: filepath.Join(t.TempDir(), "objects"), Bucket: "private", Prefix: "test/doctorchart/"}, Recovery: common.ObjectConfig{Provider: "local", Directory: filepath.Join(t.TempDir(), "journal"), Bucket: "recovery", Prefix: "test/doctorchart/"}}
	files, err := storage.New(storage.Config{Managed: &cfg, Converter: storage.ConverterConfig{URL: converter.URL, APIKey: converterKey}})
	if err != nil {
		t.Fatal(err)
	}
	db := stdlib.OpenDBFromPool(service.pool)
	t.Cleanup(func() { db.Close() })
	files.SetCatalog(db)
	service.files = files
	mux := http.NewServeMux()
	service.StudyRoutes(mux)
	secret := strings.Repeat("g", 64)
	handler := api.Gateway(secret, api.Dispatch(mux))
	request := func(method, path, owner string, body io.Reader, contentType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, body).WithContext(ctx)
		r.Header.Set("X-Gateway-Secret", secret)
		r.Header.Set("X-User-Id", owner)
		r.Header.Set("X-User-Role", "DOCTOR")
		r.Header.Set("X-User-Plan", "PRO")
		r.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	base := fmt.Sprintf("/api/doctorchart/patients/%d/studies", patient.ID)
	upload := func() study {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, err := form.CreateFormFile("file", "clinical-fixture.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(original); err != nil {
			t.Fatal(err)
		}
		if err = form.Close(); err != nil {
			t.Fatal(err)
		}
		response := request("POST", base, "integrity-owner", &body, form.FormDataContentType())
		if response.Code != 201 {
			t.Fatalf("upload: %d %s", response.Code, response.Body.String())
		}
		var result study
		if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := upload()
	var assetID string
	if err := pool.QueryRow(ctx, "SELECT media_asset_id::text FROM patient_medical_studies WHERE id=$1", first.ID).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	asset, err := (common.Catalog{DB: db}).Get(ctx, assetID)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Status != common.Associated || asset.OwnerID != "integrity-owner" || asset.Visibility != "private" || len(asset.Associations) != 1 || asset.Associations[0].EntityID != patientIdentity || asset.SHA256 != common.Checksum(original) {
		t.Fatalf("incorrect normalized clinical asset: %+v", asset)
	}
	filePath := fmt.Sprintf("%s/%d/file", base, first.ID)
	response := request("GET", filePath, "integrity-owner", nil, "")
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), original) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("private download: %d %s", response.Code, response.Body.String())
	}
	if response := request("GET", filePath, "integrity-other", nil, ""); response.Code != 403 {
		t.Fatalf("other owner accessed clinical file: %d", response.Code)
	}
	response = request("DELETE", fmt.Sprintf("%s/%d", base, first.ID), "integrity-owner", nil, "")
	if response.Code != 204 {
		t.Fatalf("delete: %d %s", response.Code, response.Body.String())
	}
	var queued bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM study_file_deletions WHERE storage_key=$1)", assetID).Scan(&queued); err != nil || !queued {
		t.Fatal("durable retirement intent missing", queued, err)
	}
	objects, err := common.NewObjects(cfg.Objects)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = objects.Head(ctx, asset.ObjectKey); err != nil {
		t.Fatal("retention removed physical clinical bytes", err)
	}
	retired, err := (common.Catalog{DB: db}).Get(ctx, assetID)
	if err != nil || retired.Status != common.PendingDelete || retired.DeleteAfter == nil || retired.DeleteAfter.Before(retired.UpdatedAt.Add(72*time.Hour)) {
		t.Fatal("retirement bypassed recovery window", retired, err)
	}
	// A second upload survives a simulated restoration that loses SQL references.
	second := upload()
	var secondAssetID string
	if err := pool.QueryRow(ctx, "SELECT media_asset_id::text FROM patient_medical_studies WHERE id=$1", second.ID).Scan(&secondAssetID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM patient_medical_studies WHERE id=$1", second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM media_assets WHERE asset_id=$1", secondAssetID); err != nil {
		t.Fatal(err)
	}
	manager, err := common.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	events, err := manager.Journal.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	report, err := common.Reconcile(ctx, nil, objects, events, "test", "doctorchart")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range report.Findings {
		if finding.AssetID == secondAssetID && finding.Classification == "recoverable_candidate" {
			found = true
		}
	}
	if !report.DryRun || !found {
		t.Fatalf("restored reference loss not identified: %+v", report)
	}
	cfg.Policy.RecoveryMode = true
	protected, err := storage.New(storage.Config{Managed: &cfg, Converter: storage.ConverterConfig{URL: converter.URL, APIKey: converterKey}})
	if err != nil {
		t.Fatal(err)
	}
	protected.SetCatalog(db)
	if err = protected.Delete(ctx, assetID); err == nil {
		t.Fatal("recovery mode allowed deletion")
	}
	if _, err = objects.Head(ctx, asset.ObjectKey); err != nil {
		t.Fatal("recovery mode lost clinical bytes", err)
	}
}
