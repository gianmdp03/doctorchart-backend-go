package clinical

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
)

func TestStudyHTTPRejectsOversizedUploadsBeforePersistence(t *testing.T) {
	// No database or converter is supplied: rejection must precede either operation.
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(storage.Config{Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{files: files}
	mux := http.NewServeMux()
	service.StudyRoutes(mux)
	key := strings.Repeat("g", 64)
	handler := api.Gateway(key, api.Dispatch(mux))
	for _, test := range []struct {
		name    string
		size    int
		chunked bool
	}{{"file boundary", storage.MaxBytes + 1, false}, {"chunked body boundary", storage.MaxBytes + (64 << 10) + 1, true}} {
		t.Run(test.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", "study.png")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(make([]byte, test.size)); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/api/doctorchart/patients/1/studies", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.Header.Set("X-Gateway-Secret", key)
			req.Header.Set("X-User-Id", "fixture-owner")
			req.Header.Set("X-User-Role", "DOCTOR")
			req.Header.Set("X-User-Plan", "PRO")
			req.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
			if test.chunked {
				req.ContentLength = -1
			}
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			var failure api.Error
			if err := json.Unmarshal(res.Body.Bytes(), &failure); err != nil {
				t.Fatal(err)
			}
			if res.Code != 413 || failure.Code != "STUDY_FILE_TOO_LARGE" || !strings.Contains(failure.Message, "10 MB") {
				t.Fatalf("upload limit not enforced: %d %s", res.Code, res.Body.String())
			}
		})
	}
}

type rejectedUploadBody struct{ reads int }

func (r *rejectedUploadBody) Read([]byte) (int, error) { r.reads++; return 0, io.EOF }

func TestStudyAdmissionRejectsBeforeReadingMultipart(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	files, err := storage.New(storage.Config{Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 21; i++ {
		upload, err := files.BeginUpload(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer upload.Close()
	}
	service := &Service{files: files}
	mux := http.NewServeMux()
	service.StudyRoutes(mux)
	key := strings.Repeat("g", 64)
	handler := api.Gateway(key, api.Dispatch(mux))
	body := &rejectedUploadBody{}
	req := httptest.NewRequest("POST", "/api/doctorchart/patients/1/studies", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=fixture")
	req.Header.Set("X-Gateway-Secret", key)
	req.Header.Set("X-User-Id", "fixture-owner")
	req.Header.Set("X-User-Role", "DOCTOR")
	req.Header.Set("X-User-Plan", "PRO")
	req.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	var failure api.Error
	if err := json.Unmarshal(res.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if res.Code != 503 || failure.Code != "STUDY_VALIDATION_BUSY" || body.reads != 0 {
		t.Fatal("full admission read/spooled another upload", res.Code, body.reads)
	}
}
