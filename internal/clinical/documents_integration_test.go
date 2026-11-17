package clinical

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func TestDocumentIssueAndContextualExport(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	s, ctx := services[0], t.Context()
	p := integrityPatient(t, s)
	mux := http.NewServeMux()
	s.Routes(mux)
	s.PrescriptionRoutes(mux)
	s.PrintRoutes(mux)
	handler := api.Gateway("document-fixture", api.Dispatch(mux))
	call := func(method, suffix, body, actor, role string, contextID int64, want int) *httptest.ResponseRecorder {
		t.Helper()
		separator := "?"
		if strings.Contains(suffix, "?") {
			separator = "&"
		}
		r := httptest.NewRequest(method, fmt.Sprintf("/api/doctorchart/patients/%d%s%sspecialtyContextId=%d", p.ID, suffix, separator, contextID), strings.NewReader(body))
		r.Header.Set("X-Gateway-Secret", "document-fixture")
		r.Header.Set("X-User-Id", actor)
		r.Header.Set("X-User-Role", role)
		r.Header.Set("X-User-Plan", "BASIC")
		r.Header.Set("X-User-Plan-Expires-At", "2099-01-01T00:00:00Z")
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s returned %d: %s", method, suffix, w.Code, w.Body)
		}
		return w
	}
	owner := func(method, suffix, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		return call(method, suffix, body, "integrity-owner", "COMPANY", p.ContextID, want)
	}
	rx := owner("POST", "/prescriptions", `{"type":"STUDY_ORDERS","prescriptions":[],"diagnosticOrders":["Synthetic study order"]}`, 201)
	var record Prescription
	if err := json.Unmarshal(rx.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	route := fmt.Sprintf("/prescriptions/%d/documents", record.ID)
	owner("POST", route, `{"doctorName":"FORGED","clinicName":"FORGED","specialty":"FORGED"}`, 400)
	create := func() documentIssue {
		w := owner("POST", route+"?doctorName=FORGED&clinicName=FORGED&specialty=FORGED", `{"type":"STUDY_ORDERS"}`, 201)
		var issued documentIssue
		if err := json.Unmarshal(w.Body.Bytes(), &issued); err != nil ||
			issued.Issuer != "integrity-owner" || strings.Contains(w.Body.String(), "FORGED") || issued.SourceVersion != 0 {
			t.Fatal("incorrect issue evidence", err)
		}
		return issued
	}
	first, second := create(), create()
	if first.ID == second.ID || first.Code == second.Code || first.SHA256 == second.SHA256 {
		t.Fatal("new issue re-used earlier evidence")
	}
	path := fmt.Sprintf("/documents/%d/pdf", first.ID)
	original := owner("GET", path, "", 200).Body.Bytes()
	if !bytes.HasPrefix(original, []byte("%PDF")) || fmt.Sprintf("%x", sha256.Sum256(original)) != first.SHA256 {
		t.Fatal("PDF did not match issue evidence")
	}
	owner("POST", fmt.Sprintf("/documents/%d/annul", first.ID), `{"reason":"Synthetic annulment"}`, 204)
	if data := owner("GET", path, "", 200).Body.Bytes(); !bytes.Equal(data, original) {
		t.Fatal("annulment or reprint changed issued bytes")
	}
	call("GET", path, "", "integrity-other", "COMPANY", p.ContextID, 404)
	call("POST", "/export", "", "fixture-admin", "SUPER_ADMIN", p.ContextID, 404)

	stored, err := s.files.Save(ctx, "synthetic.pdf", bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO patient_medical_studies
		(patient_id,study_name,original_file_name,stored_file_name,content_type,file_size,uploaded_at,annulled_at)
		VALUES($1,'Synthetic archive','../caller-path.pdf',$2,$3,$4,now(),now())`, p.ID, stored.Key, stored.ContentType, stored.Size); err != nil {
		t.Fatal(err)
	}
	result := owner("POST", "/export", "", 200)
	z, err := zip.NewReader(bytes.NewReader(result.Body.Bytes()), int64(result.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, file := range z.File {
		if strings.Contains(file.Name, "..") {
			t.Fatal("client filename escaped into archive path")
		}
		in, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name], err = io.ReadAll(io.LimitReader(in, maxChartArchiveBytes))
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	var manifest struct {
		Files []chartEntry `json:"files"`
		Scope string       `json:"scope"`
	}
	if err := json.Unmarshal(files["MANIFIESTO.json"], &manifest); err != nil || manifest.Scope != "PATIENT_CONTEXT" || len(manifest.Files)+1 != len(files) {
		t.Fatal("invalid manifest", err)
	}
	for _, entry := range manifest.Files {
		data, ok := files[entry.Path]
		if !ok || int64(len(data)) != entry.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != entry.SHA256 {
			t.Fatal("manifest digest mismatch", entry.Path)
		}
	}
	if !bytes.Equal(files[fmt.Sprintf("emisiones/documento-%d.pdf", first.ID)], original) ||
		!bytes.Contains(files["datos/emisiones.ndjson"], []byte("Synthetic annulment")) ||
		!bytes.Contains(files["datos/estudios.ndjson"], []byte("annulled_at")) ||
		!bytes.Equal(files["adjuntos/estudio-1.pdf"], original) || !bytes.Contains(files["LECTURA.html"], []byte("Synthetic study order")) {
		t.Fatal("export lost historical evidence")
	}
	if err := s.files.Delete(ctx, stored.Key); err != nil {
		t.Fatal(err)
	}
	missing := owner("POST", "/export", "", 409)
	if !strings.Contains(missing.Body.String(), "CHART_EXPORT_INCOMPLETE") || missing.Header().Get("Content-Digest") != "" {
		t.Fatal("partial export exposed to the requester")
	}
	var auditEvents int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT operation) FROM clinical_access_events
		WHERE patient_id=$1 AND operation IN ('DOCUMENT_ISSUE','DOCUMENT_REPRINT','DOCUMENT_ANNUL','CHART_EXPORT_PREPARE','CHART_EXPORT_DOWNLOAD')
		AND outcome IN ('AUTHORIZED','ARTIFACT_READY')`, p.ID).Scan(&auditEvents); err != nil || auditEvents != 5 {
		t.Fatal("document lifecycle audit is incomplete", auditEvents, err)
	}
	var protected bool
	if err := pool.QueryRow(ctx, `SELECT bool_and(
		NOT has_table_privilege('doctorchart_app',name,'UPDATE,DELETE,TRUNCATE')
		AND NOT has_any_column_privilege('doctorchart_app',name,'UPDATE')
		AND NOT has_table_privilege('doctorchart_media',name,'SELECT,INSERT,UPDATE,DELETE'))
		FROM unnest(ARRAY['clinical_document_issues','clinical_document_annulments','clinical_export_evidence']) name`).
		Scan(&protected); err != nil || !protected {
		t.Fatal("document or export evidence has unsafe ordinary privileges", err)
	}
	var issuesBefore int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM clinical_document_issues").Scan(&issuesBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE clinical_access_events ADD CONSTRAINT document_fixture_audit_failure CHECK(false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	owner("POST", route, "{}", 503)
	var issuesAfter int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM clinical_document_issues").Scan(&issuesAfter); err != nil || issuesBefore != issuesAfter {
		t.Fatal("document issue survived an audit rollback", issuesAfter, err)
	}
}
