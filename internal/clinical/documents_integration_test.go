package clinical

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/workspace"
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
		r.Header.Set("X-Clinical-Author-Name", base64.RawURLEncoding.EncodeToString([]byte("Lucía María Benítez")))
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
	if first.ID == second.ID || first.Code == second.Code {
		t.Fatal("new issue re-used earlier evidence")
	}
	path := fmt.Sprintf("/documents/%d/pdf", first.ID)
	original := owner("GET", path, "", 200).Body.Bytes()
	if !bytes.HasPrefix(original, []byte("%PDF")) || fmt.Sprintf("%x", sha256.Sum256(original)) != first.SHA256 {
		t.Fatal("PDF did not match issue evidence")
	}
	if !projectionFixturePDFContains(t, original, "Lucía María Benítez") || projectionFixturePDFContains(t, original, first.Code) || projectionFixturePDFContains(t, original, first.Issuer) || projectionFixturePDFContains(t, original, "FORGED") {
		t.Fatal("stored PDF displays unexpected author or internal identifiers")
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

func TestSignaturesLifecycleAndSignedDocumentIssuance(t *testing.T) {
	services, _ := integrityFixture(t, -1, -1)
	s := services[0]
	p := integrityPatient(t, s)
	mux := http.NewServeMux()
	s.workspaces.Routes(mux)
	s.Routes(mux)
	s.PrescriptionRoutes(mux)
	s.PrintRoutes(mux)
	handler := api.Gateway("sig-fixture", api.Dispatch(mux))

	call := func(method, path, body string, headers map[string]string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Gateway-Secret", "sig-fixture")
		r.Header.Set("X-User-Id", "integrity-owner")
		r.Header.Set("X-Clinical-Author-Name", base64.RawURLEncoding.EncodeToString([]byte("Dra. Paula Gómez")))
		r.Header.Set("X-User-Role", "COMPANY")
		r.Header.Set("X-User-Plan", "BASIC")
		r.Header.Set("X-User-Plan-Expires-At", "2099-01-01T00:00:00Z")
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s returned %d: %s", method, path, w.Code, w.Body)
		}
		return w
	}

	makePNG := func(rCol uint8) string {
		img := image.NewNRGBA(image.Rect(0, 0, 120, 48))
		for x := 10; x < 110; x++ {
			img.SetNRGBA(x, 24, color.NRGBA{R: rCol, G: 20, B: 40, A: 255})
		}
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	}

	// 1. Create two signatures: first becomes favorite automatically.
	w1 := call("POST", "/api/doctorchart/workspace/signatures", fmt.Sprintf(`{"name":"Dra. Paula","stampText":"Dra. Paula Gómez — MN 11111","imageBase64":%q}`, makePNG(10)), nil, 201)
	var sig1 workspace.SignatureSummary
	if err := json.Unmarshal(w1.Body.Bytes(), &sig1); err != nil || !sig1.IsFavorite {
		t.Fatalf("expected first signature to be favorite: %v (%s)", err, w1.Body)
	}
	w2 := call("POST", "/api/doctorchart/workspace/signatures", fmt.Sprintf(`{"name":"Dra. Julieta","stampText":"Dra. Julieta Pérez — MN 54321","imageBase64":%q}`, makePNG(60)), nil, 201)
	var sig2 workspace.SignatureSummary
	if err := json.Unmarshal(w2.Body.Bytes(), &sig2); err != nil || sig2.IsFavorite {
		t.Fatalf("expected second signature not to be favorite: %v (%s)", err, w2.Body)
	}

	// 2. Check image endpoint + ETag 304.
	imgRes := call("GET", fmt.Sprintf("/api/doctorchart/workspace/signatures/%d/image", sig2.ID), "", nil, 200)
	etag := imgRes.Header().Get("ETag")
	if etag == "" || imgRes.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected image headers: etag=%q ct=%q", etag, imgRes.Header().Get("Content-Type"))
	}
	call("GET", fmt.Sprintf("/api/doctorchart/workspace/signatures/%d/image", sig2.ID), "", map[string]string{"If-None-Match": etag}, 304)

	// 3. Create prescription and issue with default, explicit second signature, and none.
	rxW := call("POST", fmt.Sprintf("/api/doctorchart/patients/%d/prescriptions?specialtyContextId=%d", p.ID, p.ContextID),
		`{"type":"STUDY_ORDERS","prescriptions":[],"diagnosticOrders":["Ecografía abdominal"]}`, nil, 201)
	var rx Prescription
	if err := json.Unmarshal(rxW.Body.Bytes(), &rx); err != nil {
		t.Fatal(err)
	}
	issuePath := fmt.Sprintf("/api/doctorchart/patients/%d/prescriptions/%d/documents?specialtyContextId=%d", p.ID, rx.ID, p.ContextID)

	wDefault := call("POST", issuePath, `{"type":"STUDY_ORDERS"}`, nil, 201)
	var docDefault documentIssue
	if err := json.Unmarshal(wDefault.Body.Bytes(), &docDefault); err != nil {
		t.Fatal(err)
	}
	wExplicit := call("POST", issuePath, fmt.Sprintf(`{"type":"STUDY_ORDERS","signatureId":%d}`, sig2.ID), nil, 201)
	var docExplicit documentIssue
	if err := json.Unmarshal(wExplicit.Body.Bytes(), &docExplicit); err != nil {
		t.Fatal(err)
	}
	wNone := call("POST", issuePath, `{"type":"STUDY_ORDERS","signatureId":null}`, nil, 201)
	var docNone documentIssue
	if err := json.Unmarshal(wNone.Body.Bytes(), &docNone); err != nil {
		t.Fatal(err)
	}

	pdfDefault := call("GET", fmt.Sprintf("/api/doctorchart/patients/%d/documents/%d/pdf?specialtyContextId=%d", p.ID, docDefault.ID, p.ContextID), "", nil, 200).Body.Bytes()
	pdfExplicit := call("GET", fmt.Sprintf("/api/doctorchart/patients/%d/documents/%d/pdf?specialtyContextId=%d", p.ID, docExplicit.ID, p.ContextID), "", nil, 200).Body.Bytes()
	pdfNone := call("GET", fmt.Sprintf("/api/doctorchart/patients/%d/documents/%d/pdf?specialtyContextId=%d", p.ID, docNone.ID, p.ContextID), "", nil, 200).Body.Bytes()

	if !projectionFixturePDFContains(t, pdfDefault, "Dra. Paula Gómez — MN 11111") || projectionFixturePDFContains(t, pdfDefault, "Emitido sin firma digital") {
		t.Fatal("default issued PDF did not use favorite signature cleanly")
	}
	if !projectionFixturePDFContains(t, pdfExplicit, "Dra. Julieta Pérez — MN 54321") || projectionFixturePDFContains(t, pdfExplicit, "Emitido sin firma digital") {
		t.Fatal("explicit issued PDF did not use selected signature cleanly")
	}
	if !projectionFixturePDFContains(t, pdfNone, "Emitido sin firma digital") {
		t.Fatal("unsigned issued PDF missing unsigned footer")
	}

	// 4. Delete favorite signature #1 -> signature #2 is promoted, and historical PDF remains intact.
	call("DELETE", fmt.Sprintf("/api/doctorchart/workspace/signatures/%d", sig1.ID), "", nil, 204)
	listRes := call("GET", "/api/doctorchart/workspace/signatures", "", nil, 200)
	var sigList struct {
		Items []workspace.SignatureSummary `json:"items"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &sigList); err != nil || len(sigList.Items) != 1 || !sigList.Items[0].IsFavorite || sigList.Items[0].ID != sig2.ID {
		t.Fatalf("expected signature #2 to be promoted to favorite: %s", listRes.Body)
	}
	reprint := call("GET", fmt.Sprintf("/api/doctorchart/patients/%d/documents/%d/pdf?specialtyContextId=%d", p.ID, docDefault.ID, p.ContextID), "", nil, 200).Body.Bytes()
	if !bytes.Equal(reprint, pdfDefault) {
		t.Fatal("deleting signature altered previously issued immutable PDF")
	}
}
