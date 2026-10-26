package clinical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/bootstrap"
)

func TestStudyNamePersistenceAndAccess(t *testing.T) {
	services, pool := integrityFixture(t, 10, 10)
	service := services[0]
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `UPDATE specialties SET patient_record_tabs=patient_record_tabs || '["STUDIES"]'::jsonb WHERE id=(SELECT specialty_id FROM doctor_workspaces WHERE user_id='integrity-owner') AND NOT patient_record_tabs @> '["STUDIES"]'::jsonb`); err != nil {
		t.Fatal(err)
	}
	patient := integrityPatient(t, service)
	var content bytes.Buffer
	if err := gif.Encode(&content, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	const filename = "IMG_1234.gif"
	const title = `Tomografía "control" / cerebral: anual`
	const gatewayKey = "study-name-fixture"
	mux := http.NewServeMux()
	service.StudyRoutes(mux)
	handler := api.Gateway(gatewayKey, api.Dispatch(mux))
	request := func(method, path, user, contentType string, body io.Reader) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, body)
		r.Header.Set("X-Gateway-Secret", gatewayKey)
		r.Header.Set("X-User-Id", user)
		r.Header.Set("X-User-Role", "DOCTOR")
		r.Header.Set("X-User-Plan", "BASIC")
		r.Header.Set("X-User-Plan-Expires-At", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	base := fmt.Sprintf("/api/doctorchart/patients/%d/studies", patient.ID)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("name", "  "+title+"  "); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("description", "Con contraste\nControl anual"); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	response := request("POST", base, "integrity-owner", form.FormDataContentType(), &body)
	if response.Code != 201 {
		t.Fatalf("upload: %d %s", response.Code, response.Body.String())
	}
	var saved study
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Name != title || saved.OriginalFileName != filename {
		t.Fatalf("independent name/filename: %+v", saved)
	}
	var name, original string
	if err := pool.QueryRow(ctx, "SELECT study_name,original_file_name FROM patient_medical_studies WHERE id=$1", saved.ID).Scan(&name, &original); err != nil {
		t.Fatal(err)
	}
	if name != title || original != filename {
		t.Fatal("database lost name or attachment filename")
	}
	// Re-run real startup validation and read from a new HTTP handler instance.
	if err := bootstrap.EnsureSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	otherMux := http.NewServeMux()
	services[1].StudyRoutes(otherMux)
	otherHandler := api.Gateway(gatewayKey, api.Dispatch(otherMux))
	firstHandler := handler
	handler = otherHandler
	response = request("GET", base, "integrity-owner", "", nil)
	var listed []study
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &listed) != nil || len(listed) != 1 || listed[0].Name != title || listed[0].OriginalFileName != filename {
		t.Fatalf("list after restart: %d %s", response.Code, response.Body.String())
	}
	handler = firstHandler
	detail := fmt.Sprintf("%s/%d", base, saved.ID)
	response = request("GET", detail, "integrity-owner", "", nil)
	var read study
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &read) != nil || read.Name != title || read.OriginalFileName != filename {
		t.Fatalf("detail: %d %s", response.Code, response.Body.String())
	}
	response = request("GET", detail+"/file", "integrity-owner", "", nil)
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), content.Bytes()) || !strings.Contains(response.Header().Get("Content-Disposition"), filename) || strings.Contains(response.Header().Get("Content-Disposition"), "control") {
		t.Fatalf("attachment changed: %d %s", response.Code, response.Header())
	}
	for _, path := range []string{base, detail, detail + "/file"} {
		if denied := request("GET", path, "integrity-other", "", nil); denied.Code != 403 {
			t.Fatalf("cross-owner access: %d %s", denied.Code, denied.Body.String())
		}
	}
	for _, test := range []struct {
		names []string
		code  string
	}{
		{nil, "STUDY_NAME_REQUIRED"},
		{[]string{"   "}, "STUDY_NAME_REQUIRED"},
		{[]string{strings.Repeat("a", 241)}, "STUDY_NAME_TOO_LONG"},
		{[]string{"Control\n"}, "INVALID_STUDY_NAME"},
		{[]string{"Uno", "Dos"}, "INVALID_STUDY_NAME"},
	} {
		var invalidBody bytes.Buffer
		invalidForm := multipart.NewWriter(&invalidBody)
		for _, name := range test.names {
			if err := invalidForm.WriteField("name", name); err != nil {
				t.Fatal(err)
			}
		}
		part, err := invalidForm.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content.Bytes()); err != nil {
			t.Fatal(err)
		}
		if err := invalidForm.Close(); err != nil {
			t.Fatal(err)
		}
		response := request("POST", base, "integrity-owner", invalidForm.FormDataContentType(), &invalidBody)
		var failure api.Error
		if response.Code != 400 || json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != test.code {
			t.Fatalf("invalid name admitted: %d %s", response.Code, response.Body.String())
		}
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM patient_medical_studies").Scan(&count); err != nil || count != 1 {
		t.Fatalf("rejected upload persisted: %d %v", count, err)
	}
}
