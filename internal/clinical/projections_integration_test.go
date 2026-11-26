package clinical

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"unicode/utf16"

	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

// These cases deliberately represent stored forms, including definitions which
// current publication validation rejects. They are never published as templates.
func projectionFixtureForm(fields ...map[string]any) map[string]any {
	items := make([]any, len(fields))
	for i, field := range fields {
		items[i] = field
	}
	return map[string]any{"categories": []any{map[string]any{
		"key": "synthetic_projection", "title": "Synthetic historical projection", "enabled": true, "fields": items,
	}}}
}

func projectionFixtureField(key string) map[string]any {
	return map[string]any{"key": key, "label": key, "type": "TEXTAREA", "required": false}
}

func projectionFixtureHandler(s *Service) http.Handler {
	mux := http.NewServeMux()
	s.Routes(mux)
	s.EncounterRoutes(mux)
	s.PrescriptionRoutes(mux)
	s.PrintRoutes(mux)
	return api.Gateway("projection-fixture", api.Dispatch(mux))
}

func projectionFixtureCall(t *testing.T, handler http.Handler, method, path, actor string, scope int64, body any, status int) *httptest.ResponseRecorder {
	t.Helper()
	var input []byte
	var err error
	if body != nil {
		input, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	u, err := url.Parse("/api/doctorchart" + path)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("specialtyContextId", fmt.Sprint(scope))
	u.RawQuery = query.Encode()
	r := httptest.NewRequest(method, u.String(), bytes.NewReader(input))
	r.Header.Set("X-Gateway-Secret", "projection-fixture")
	r.Header.Set("X-User-Id", actor)
	r.Header.Set("X-User-Role", "COMPANY")
	r.Header.Set("X-User-Plan", "BASIC")
	r.Header.Set("X-User-Plan-Expires-At", "2099-01-01T00:00:00Z")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status=%d expected=%d body=%s", method, path, w.Code, status, w.Body)
	}
	return w
}

func projectionFixtureDecode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func projectionFixtureFollowUp(t *testing.T, got *string, want string) {
	t.Helper()
	if want == "" {
		if got != nil && *got != "" {
			t.Fatalf("empty or hidden follow-up reappeared: %q", *got)
		}
		return
	}
	if got == nil || *got != want {
		t.Fatalf("follow-up=%v expected=%q", got, want)
	}
}

func TestHistoricalProjectionHTTPReadsKeepStoredRows(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	s, ctx := services[0], t.Context()
	// A newer account configuration must not reinterpret a historical snapshot.
	current := projectionFixtureForm(projectionFixtureField("followUpInstructions"))
	currentJSON, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE doctor_specialty_contexts SET definition=jsonb_set(definition,'{encounterForm}',$1::jsonb),version=1
		WHERE workspace_id=(SELECT id FROM doctor_workspaces WHERE user_id='integrity-owner')`, currentJSON); err != nil {
		t.Fatal(err)
	}
	legacy, canonical := projectionFixtureField("followUp"), projectionFixtureField("followUpInstructions")
	hidden := projectionFixtureField("followUpInstructions")
	hidden["visibleWhen"] = map[string]any{"fieldKey": "show", "equals": true}
	hiddenLegacy := projectionFixtureField("followUp")
	hiddenLegacy["visibleWhen"] = map[string]any{"fieldKey": "show", "equals": true}
	show := map[string]any{"key": "show", "label": "Show", "type": "BOOLEAN", "required": false}
	for i, test := range []struct {
		name   string
		form   map[string]any
		values map[string]any
		want   string
	}{
		{"historical", projectionFixtureForm(legacy), map[string]any{"followUp": "SP04F_HISTORICAL", "unknownSaved": "Synthetic unknown"}, "SP04F_HISTORICAL"},
		{"canonical", projectionFixtureForm(canonical), map[string]any{"followUpInstructions": "SP04F_CANONICAL"}, "SP04F_CANONICAL"},
		{"both", projectionFixtureForm(legacy, canonical), map[string]any{"followUp": "SP04F_OLD", "followUpInstructions": "SP04F_CANONICAL"}, "SP04F_CANONICAL"},
		{"canonical_empty", projectionFixtureForm(legacy, canonical), map[string]any{"followUp": "SP04F_OLD", "followUpInstructions": ""}, ""},
		{"canonical_hidden", projectionFixtureForm(show, legacy, hidden), map[string]any{"show": false, "followUp": "SP04F_OLD", "followUpInstructions": "SP04F_HIDDEN"}, ""},
		{"historical_hidden", projectionFixtureForm(show, hiddenLegacy), map[string]any{"show": false, "followUp": "SP04F_HIDDEN"}, ""},
		{"canonical_absent_from_snapshot", projectionFixtureForm(legacy), map[string]any{"followUp": "SP04F_HISTORICAL", "followUpInstructions": "Synthetic unknown canonical answer"}, "SP04F_HISTORICAL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := integrityPatientRequest()
			request["workspaceVersion"] = json.Number("1")
			request["values"].(map[string]any)["sys_dni"] = fmt.Sprintf("%08d", 12345678+i)
			p, err := s.CreatePatient(ctx, api.Identity{UserID: "integrity-owner", Plan: "BASIC"}, request)
			if err != nil {
				t.Fatal(err)
			}
			form, err := json.Marshal(test.form)
			if err != nil {
				t.Fatal(err)
			}
			values, err := json.Marshal(test.values)
			if err != nil {
				t.Fatal(err)
			}
			var id int64
			if err := pool.QueryRow(ctx, `INSERT INTO clinical_encounters
				(patient_id,specialty_id,encounter_date,consultation_type,follow_up_instructions,follow_up_date,
				form_definition_snapshot,form_values,formula_context,formula_results,workspace_version,version,updated_at,diagnostic_orders)
				VALUES($1,(SELECT specialty_id FROM doctor_specialty_contexts WHERE id=$2),'2026-01-02T12:00:00Z','FOLLOW_UP','SP04F_STALE_COLUMN','2026-12-23',
				$3,$4,'{"zone":"America/Argentina/Buenos_Aires"}','{}',0,0,'2026-01-02T12:00:00Z','["Synthetic study"]') RETURNING id`,
				p.ID, p.ContextID, form, values).Scan(&id); err != nil {
				t.Fatal(err)
			}
			var before []byte
			if err := pool.QueryRow(ctx, "SELECT to_jsonb(e) FROM clinical_encounters e WHERE id=$1", id).Scan(&before); err != nil {
				t.Fatal(err)
			}
			handler := projectionFixtureHandler(s)
			read := func(path string) *httptest.ResponseRecorder {
				t.Helper()
				return projectionFixtureCall(t, handler, "GET", path, "integrity-owner", p.ContextID, nil, 200)
			}
			detail := projectionFixtureDecode[Encounter](t, read(fmt.Sprintf("/encounters/%d", id)).Body.Bytes())
			projectionFixtureFollowUp(t, detail.FollowUp, test.want)
			if detail.FollowUpDate == nil || *detail.FollowUpDate != "2026-12-23" || detail.Date != "2026-01-02T12:00:00Z" {
				t.Fatal("projection changed the encounter or follow-up dates")
			}
			list := projectionFixtureDecode[[]EncounterSummary](t, read(fmt.Sprintf("/encounters/patient/%d", p.ID)).Body.Bytes())
			if len(list) != 1 {
				t.Fatal("historical consultation disappeared from its history")
			}
			projectionFixtureFollowUp(t, list[0].FollowUp, test.want)
			summary := projectionFixtureDecode[ClinicalSummary](t, read(fmt.Sprintf("/patients/%d/clinical-summary", p.ID)).Body.Bytes())
			if summary.LastEncounter == nil || summary.LastEncounter.ID != id {
				t.Fatal("summary lost its last historical consultation")
			}
			projectionFixtureFollowUp(t, summary.LastEncounter.FollowUp, test.want)
			printable := projectionFixtureDecode[printDocument](t, read(fmt.Sprintf("/patients/%d/encounters/%d/print", p.ID, id)).Body.Bytes())
			projectionFixtureFollowUp(t, printable.Instructions, test.want)
			var after []byte
			var revisions int
			if err := pool.QueryRow(ctx, "SELECT to_jsonb(e),(SELECT count(*) FROM clinical_revisions WHERE entity='ENCOUNTER' AND entity_id=e.id) FROM clinical_encounters e WHERE id=$1", id).Scan(&after, &revisions); err != nil ||
				!bytes.Equal(before, after) || revisions != 0 {
				t.Fatal("reading historical compatibility modified stored data or fabricated a revision", err)
			}
		})
	}
}

// Search the decoded PDF page stream for UTF-16 text, rather than assuming that
// an HTTP JSON projection proves the text was actually emitted in the PDF.
func projectionFixturePDFContains(t *testing.T, data []byte, text string) bool {
	t.Helper()
	var content bytes.Buffer
	if err := pdfapi.ExtractContent(t.Context(), bytes.NewReader(data), nil, func(reader io.Reader, _ int) error {
		_, err := io.Copy(&content, reader)
		return err
	}, model.NewDefaultConfiguration()); err != nil {
		t.Fatal(err)
	}
	var encoded []byte
	for _, unit := range utf16.Encode([]rune(text)) {
		encoded = binary.BigEndian.AppendUint16(encoded, unit)
	}
	return bytes.Contains(content.Bytes(), encoded)
}

func TestHistoricalProjectionRevisionDocumentsExportAndIsolation(t *testing.T) {
	services, pool := integrityFixture(t, -1, -1)
	s, ctx := services[0], t.Context()
	p := integrityPatient(t, s)
	form := projectionFixtureForm(projectionFixtureField("chiefComplaint"), projectionFixtureField("diagnosis"),
		projectionFixtureField("treatment"), projectionFixtureField("followUp"))
	formJSON, err := json.Marshal(form)
	if err != nil {
		t.Fatal(err)
	}
	const originalValues = `{"chiefComplaint":"Synthetic historical visit","diagnosis":"Synthetic historical diagnosis","treatment":"Synthetic treatment","followUp":"SP04F_HISTORICAL","unknownSaved":"SP04F_UNKNOWN_ORIGINAL"}`
	var encounterID int64
	if err := pool.QueryRow(ctx, `INSERT INTO clinical_encounters
		(patient_id,specialty_id,encounter_date,consultation_type,chief_complaint,primary_diagnosis_description,follow_up_instructions,
		follow_up_date,form_definition_snapshot,form_values,formula_context,formula_results,workspace_version,version,updated_at,diagnostic_orders)
		VALUES($1,(SELECT specialty_id FROM doctor_specialty_contexts WHERE id=$2),'2026-01-02T12:00:00Z','FOLLOW_UP','Synthetic stale complaint','Synthetic stale diagnosis',NULL,
		'2026-12-23',$3,$4,'{"zone":"America/Argentina/Buenos_Aires"}','{}',0,0,'2026-01-02T12:00:00Z','["Synthetic study"]') RETURNING id`,
		p.ID, p.ContextID, formJSON, originalValues).Scan(&encounterID); err != nil {
		t.Fatal(err)
	}
	handler := projectionFixtureHandler(s)
	owner := func(method, path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		return projectionFixtureCall(t, handler, method, path, "integrity-owner", p.ContextID, body, status)
	}
	patientPath := fmt.Sprintf("/patients/%d", p.ID)
	encounterPath := fmt.Sprintf("/encounters/%d", encounterID)
	printPath := fmt.Sprintf("%s/encounters/%d", patientPath, encounterID)
	// Preserve a real PDF produced with the old missing-follow-up interpretation.
	// Inserting a synthetic prior emission avoids pretending that today's issuer
	// can still reproduce that old interpretation from current projections.
	doc, err := s.printable(api.WithSpecialtyContext(ctx, p.ContextID), "integrity-owner", p.ID, encounterID, true)
	if err != nil {
		t.Fatal(err)
	}
	doc.Instructions = nil
	doc.Code, doc.emitted = "DC-00000000000000000000000000000000", true
	originalPDF, err := s.printPDF(doc, url.Values{"type": {"STUDY_ORDERS"}})
	if err != nil {
		t.Fatal(err)
	}
	originalHash := fmt.Sprintf("%x", sha256.Sum256(originalPDF))
	var historicalIssue int64
	if err := pool.QueryRow(ctx, `INSERT INTO clinical_document_issues
		(code,owner_user_id,specialty_context_id,patient_id,source,source_id,source_version,document_type,issuer_specialty,issued_at,pdf,sha256)
		VALUES($1,'integrity-owner',$2,$3,'ENCOUNTER',$4,0,'STUDY_ORDERS',$5,'2026-01-02T12:00:00Z',$6,$7) RETURNING id`,
		doc.Code, p.ContextID, p.ID, encounterID, *doc.Author.Specialty, originalPDF, originalHash).Scan(&historicalIssue); err != nil {
		t.Fatal(err)
	}
	originalPath := fmt.Sprintf("%s/documents/%d/pdf", patientPath, historicalIssue)
	if projectionFixturePDFContains(t, originalPDF, "SP04F_HISTORICAL") {
		t.Fatal("prior PDF fixture unexpectedly contains the corrected projection")
	}
	printable := projectionFixtureDecode[printDocument](t, owner("GET", printPath+"/print", nil, 200).Body.Bytes())
	projectionFixtureFollowUp(t, printable.Instructions, "SP04F_HISTORICAL")
	issue := func(want string, version int64) documentIssue {
		t.Helper()
		issued := projectionFixtureDecode[documentIssue](t, owner("POST", printPath+"/documents", map[string]any{}, 201).Body.Bytes())
		pdf := owner("GET", fmt.Sprintf("%s/documents/%d/pdf", patientPath, issued.ID), nil, 200).Body.Bytes()
		if issued.Issuer != "integrity-owner" || issued.SourceVersion != version || issued.SourceID != encounterID ||
			fmt.Sprintf("%x", sha256.Sum256(pdf)) != issued.SHA256 || !projectionFixturePDFContains(t, pdf, want) {
			t.Fatal("new emission did not conserve its corrected content and authoritative evidence")
		}
		return issued
	}
	issue("SP04F_HISTORICAL", 0)
	corrected := owner("PUT", encounterPath, map[string]any{
		"expectedVersion": 0, "consultationType": "FOLLOW_UP", "reason": "Synthetic verified correction",
		"values": map[string]any{"chiefComplaint": "Synthetic corrected visit", "diagnosis": "Synthetic corrected diagnosis",
			"treatment": "Synthetic corrected treatment", "followUp": "SP04F_CORRECTED"},
	}, 200)
	current := projectionFixtureDecode[Encounter](t, corrected.Body.Bytes())
	projectionFixtureFollowUp(t, current.FollowUp, "SP04F_CORRECTED")
	if current.Version != 1 || current.Date != "2026-01-02T12:00:00Z" || current.FollowUpDate == nil || *current.FollowUpDate != "2026-12-23" {
		t.Fatal("correction changed dates or failed to advance the clinical version")
	}
	readCurrent := projectionFixtureDecode[Encounter](t, owner("GET", encounterPath, nil, 200).Body.Bytes())
	projectionFixtureFollowUp(t, readCurrent.FollowUp, "SP04F_CORRECTED")
	if !reflect.DeepEqual(projectionFixtureDecode[map[string]any](t, readCurrent.Snapshot), form) {
		t.Fatal("correction rewrote the saved snapshot")
	}
	if projectionFixtureDecode[map[string]any](t, readCurrent.Values)["unknownSaved"] != "SP04F_UNKNOWN_ORIGINAL" {
		t.Fatal("correction erased an unknown historical answer")
	}
	revisions := projectionFixtureDecode[struct{ Items []revisionItem }](t, owner("GET", patientPath+"/revisions", nil, 200).Body.Bytes())
	if len(revisions.Items) != 1 || revisions.Items[0].Operation != "CORRECT" || revisions.Items[0].Actor != "integrity-owner" ||
		revisions.Items[0].PreviousVersion != 0 || revisions.Items[0].NewVersion != 1 ||
		revisions.Items[0].Derived["encounter.followUpInstructions"] != "SP04F_HISTORICAL" {
		t.Fatal("correction lost its actor, previous version or historical interpretation")
	}
	previous := projectionFixtureDecode[map[string]any](t, revisions.Items[0].Previous)
	wantOriginal := projectionFixtureDecode[map[string]any](t, []byte(originalValues))
	if !reflect.DeepEqual(previous["form_values"], wantOriginal) || previous["follow_up_instructions"] != nil {
		t.Fatal("revision rewrote the original answers or obsolete persisted projection")
	}
	issue("SP04F_CORRECTED", 1)
	if got := owner("GET", originalPath, nil, 200).Body.Bytes(); !bytes.Equal(got, originalPDF) {
		t.Fatal("new read rules or correction regenerated a prior PDF")
	}
	// Prescription-only instructions remain independent of encounter follow-up.
	rx := projectionFixtureDecode[Prescription](t, owner("POST", patientPath+"/prescriptions", map[string]any{
		"type": "STUDY_ORDERS", "generalInstructions": "SP04F_RX_ONLY", "prescriptions": []any{}, "diagnosticOrders": []any{"Synthetic separate order"},
	}, 201).Body.Bytes())
	rxDoc := projectionFixtureDecode[printDocument](t, owner("GET", fmt.Sprintf("%s/prescriptions/%d/print", patientPath, rx.ID), nil, 200).Body.Bytes())
	projectionFixtureFollowUp(t, rxDoc.Instructions, "SP04F_RX_ONLY")
	summary := projectionFixtureDecode[ClinicalSummary](t, owner("GET", patientPath+"/clinical-summary", nil, 200).Body.Bytes())
	if summary.LastEncounter == nil {
		t.Fatal("corrected consultation disappeared")
	}
	projectionFixtureFollowUp(t, summary.LastEncounter.FollowUp, "SP04F_CORRECTED")
	owner("DELETE", encounterPath, map[string]any{"expectedVersion": 1, "reason": "Synthetic duplicate consultation"}, 204)
	summary = projectionFixtureDecode[ClinicalSummary](t, owner("GET", patientPath+"/clinical-summary", nil, 200).Body.Bytes())
	if summary.LastEncounter != nil || len(summary.Diagnoses) != 0 {
		t.Fatal("annulled consultation or diagnosis remained current in summary")
	}
	if list := projectionFixtureDecode[[]EncounterSummary](t, owner("GET", fmt.Sprintf("/encounters/patient/%d", p.ID), nil, 200).Body.Bytes()); len(list) != 0 {
		t.Fatal("annulled consultation remained in the current history")
	}
	owner("GET", printPath+"/print", nil, 404)
	if got := owner("GET", originalPath, nil, 200).Body.Bytes(); !bytes.Equal(got, originalPDF) {
		t.Fatal("annulling the source changed its prior issued document")
	}

	archive := owner("POST", patientPath+"/export", nil, 200)
	z, err := zip.NewReader(bytes.NewReader(archive.Body.Bytes()), int64(archive.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, entry := range z.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name], err = io.ReadAll(io.LimitReader(reader, maxChartArchiveBytes))
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	manifest := projectionFixtureDecode[struct{ Files []chartEntry }](t, files["MANIFIESTO.json"])
	if len(manifest.Files)+1 != len(files) {
		t.Fatal("export manifest omitted content")
	}
	for _, entry := range manifest.Files {
		data, ok := files[entry.Path]
		if !ok || int64(len(data)) != entry.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != entry.SHA256 {
			t.Fatal("export file did not match its manifest", entry.Path)
		}
	}
	row := projectionFixtureDecode[map[string]any](t, bytes.TrimSpace(files["datos/consultas.ndjson"]))
	if row["annulled_at"] == nil || row["derivedProjections"].(map[string]any)["encounter.followUpInstructions"] != "SP04F_CORRECTED" ||
		row["form_values"].(map[string]any)["followUp"] != "SP04F_CORRECTED" {
		t.Fatal("contextual export confused derived, original or annulled clinical data")
	}
	decoder := json.NewDecoder(bytes.NewReader(files["datos/revisiones.ndjson"]))
	var foundOriginal, foundAnnulment bool
	for decoder.More() {
		var revision map[string]any
		if err := decoder.Decode(&revision); err != nil {
			t.Fatal(err)
		}
		before := revision["previous_content"].(map[string]any)
		if revision["operation"] == "CORRECT" {
			foundOriginal = reflect.DeepEqual(before["form_values"], wantOriginal) &&
				revision["derivedProjections"].(map[string]any)["encounter.followUpInstructions"] == "SP04F_HISTORICAL"
		}
		if revision["operation"] == "ANNUL" {
			foundAnnulment = before["form_values"].(map[string]any)["followUp"] == "SP04F_CORRECTED"
		}
	}
	if !foundOriginal || !foundAnnulment || !bytes.Equal(files[fmt.Sprintf("emisiones/documento-%d.pdf", historicalIssue)], originalPDF) {
		t.Fatal("export lost original unknown values, the annulment or the exact prior PDF")
	}

	var otherScope, dermatology int64
	if err := pool.QueryRow(ctx, `SELECT c.id FROM doctor_specialty_contexts c JOIN doctor_workspaces w ON w.id=c.workspace_id WHERE w.user_id='integrity-other'`).Scan(&otherScope); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT id FROM specialties WHERE code='dermatology'").Scan(&dermatology); err != nil {
		t.Fatal(err)
	}
	otherContext, err := s.workspaces.AddContext(ctx, "integrity-owner", map[string]any{"specialtyId": json.Number(fmt.Sprint(dermatology))})
	if err != nil {
		t.Fatal(err)
	}
	otherContext, err = s.workspaces.Edit(ctx, "integrity-owner", "finish", map[string]any{"specialtyContextId": json.Number(fmt.Sprint(otherContext.ContextID)), "expectedVersion": json.Number(fmt.Sprint(otherContext.Version))})
	if err != nil {
		t.Fatal(err)
	}
	for _, denied := range []struct {
		actor string
		scope int64
	}{{"integrity-other", otherScope}, {"integrity-owner", otherContext.ContextID}} {
		for _, path := range []string{patientPath + "/clinical-summary", patientPath + "/revisions", originalPath} {
			projectionFixtureCall(t, handler, "GET", path, denied.actor, denied.scope, nil, 404)
		}
		projectionFixtureCall(t, handler, "POST", patientPath+"/export", denied.actor, denied.scope, nil, 404)
	}
	var operations int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT operation) FROM clinical_access_events
		WHERE patient_id=$1 AND operation IN ('CLINICAL_SUMMARY','PATIENT_ENCOUNTERS','ENCOUNTER_PRINT_DATA',
		'PRESCRIPTION_PRINT_DATA','CLINICAL_REVISIONS','DOCUMENT_ISSUE','DOCUMENT_REPRINT','CHART_EXPORT_PREPARE','CHART_EXPORT_DOWNLOAD')
		AND outcome IN ('AUTHORIZED','ARTIFACT_READY')`, p.ID).Scan(&operations); err != nil || operations != 9 {
		t.Fatal("projection read/document/export audit incomplete", operations, err)
	}
	var rejected int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM clinical_access_events WHERE patient_id=$1 AND outcome='REJECTED'`, p.ID).Scan(&rejected); err != nil || rejected < 8 {
		t.Fatal("cross-account/context denials did not retain their audit events", rejected, err)
	}
	var evidenceHash string
	if err := pool.QueryRow(ctx, "SELECT sha256 FROM clinical_document_issues WHERE id=$1", historicalIssue).Scan(&evidenceHash); err != nil || evidenceHash != originalHash {
		t.Fatal("prior issuance evidence changed", err)
	}
}
