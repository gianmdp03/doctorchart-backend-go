package dataexport

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	media "github.com/bycode/bycode-image-converter-go/pkg/media"
	"github.com/go-pdf/fpdf"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/bootstrap"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
	zip "go.foxforensics.eu/go-zip/zip"
)

func exportFixture(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("DOCTORCHART_EXPORT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set DOCTORCHART_EXPORT_TEST_DATABASE_URL to an isolated local PostgreSQL cluster initialized for doctorchart_export_fixture")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.User != "doctorchart_export_fixture" || config.ConnConfig.Host != "127.0.0.1" || config.ConnConfig.Port != 55485 {
		t.Fatal("export test requires its dedicated loopback cluster")
	}
	config.ConnConfig.RuntimeParams["timezone"] = "UTC"
	admin, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if _, err := admin.Exec(t.Context(), `DO $$ BEGIN
		IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='doctorchart_app') THEN CREATE ROLE doctorchart_app LOGIN; END IF;
		IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='doctorchart_media') THEN CREATE ROLE doctorchart_media LOGIN; END IF;
	END $$;`); err != nil {
		t.Fatal(err)
	}
	validator, err := wizard.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.Run(t.Context(), admin, validator); err != nil {
		t.Fatal(err)
	}
	appConfig := config.Copy()
	appConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE doctorchart_app")
		return err
	}
	app, err := pgxpool.NewWithConfig(t.Context(), appConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return admin, app
}

func TestPracticeExportRoundTrip(t *testing.T) {
	db, app := exportFixture(t)
	ctx := t.Context()
	uid := hex.EncodeToString(randBytes(5))
	owner, other := "export-doctor-"+uid, "export-other-"+uid
	profileMarker := "Profesional sintético"
	patientName, otherName := "Paciente <sintético>", "Otro paciente SECRET-AISLADO"
	syntheticStudy := fixturePDF(t, "Estudio sintetico")
	syntheticPDF := fixturePDF(t, "Emision sintetica")

	makeContext := func(owner string, n int) int64 {
		t.Helper()
		var id int64
		err := db.QueryRow(ctx, `INSERT INTO doctor_specialty_contexts
			(workspace_id,specialty_id,definition,draft_definition,created_at,updated_at,current_step_key,version,onboarding_completed)
			SELECT w.id,s.id,'{"marker":"wizard-actual"}', '{"marker":"wizard-borrador"}',now(),now(),'COMPLETE',3,true
			FROM doctor_workspaces w CROSS JOIN (SELECT id FROM specialties ORDER BY id OFFSET $2 LIMIT 1) s
			WHERE w.user_id=$1 RETURNING id`, owner, n).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	for _, person := range []string{owner, other} {
		if _, err := db.Exec(ctx, "INSERT INTO doctor_workspaces (user_id,created_at,updated_at,version) VALUES ($1,now(),now(),0)", person); err != nil {
			t.Fatal(err)
		}
	}
	first, second, foreign := makeContext(owner, 0), makeContext(owner, 1), makeContext(other, 0)
	makePatient := func(owner, name string, id int64) int64 {
		t.Helper()
		var patient int64
		err := db.QueryRow(ctx, `INSERT INTO patients (birth_date, full_name, national_id, updated_at, user_id, workspace_id,
			specialty_context_id, version, workspace_version, form_definition_snapshot, form_values, formula_context, formula_results)
			SELECT '2020-01-01', $2, '12345678', now(), w.user_id, w.id, $3, 0, 0, '{"marker":"ficha"}', '{}', '{}', '{}'
			FROM doctor_workspaces w WHERE w.user_id=$1 RETURNING id`, owner, name, id).Scan(&patient)
		if err != nil {
			t.Fatal(err)
		}
		return patient
	}
	patient := makePatient(owner, patientName, first)
	secondPatient := makePatient(owner, "Mismo DNI en otro contexto", second)
	otherPatient := makePatient(other, otherName, foreign)

	var encounter, document, study int64
	if err := db.QueryRow(ctx, `INSERT INTO clinical_encounters
		(consultation_type, encounter_date, form_definition_snapshot, form_values, formula_context, formula_results, updated_at,
		version, workspace_version, patient_id, specialty_id)
		SELECT 'FOLLOW_UP',now(),'{"marker":"wizard-historico","categories":[{"fields":[{"key":"field","label":"Campo histórico"}]}]}','{"field":9007199254740993}','{}','{}',now(),0,1,$1,c.specialty_id
		FROM doctor_specialty_contexts c WHERE c.id=$2 RETURNING id`, patient, first).Scan(&encounter); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO clinical_document_issues
		(code,owner_user_id,specialty_context_id,patient_id,source,source_id,source_version,document_type,issuer_specialty,issued_at,pdf,sha256)
		VALUES($1,$2,$3,$4,'ENCOUNTER',$5,0,'PRESCRIPTION','Prueba',now(),$6,encode(sha256($6),'hex')) RETURNING id`,
		"DC-"+hex.EncodeToString(randBytes(16)), owner, first, patient, encounter, syntheticPDF).Scan(&document); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	managed := media.Config{
		Environment: "test", Service: "doctorchart", Policy: media.DeletionPolicy{RetainFor: 72 * time.Hour},
		Objects:  media.ObjectConfig{Provider: "local", Directory: filepath.Join(t.TempDir(), "objects"), Bucket: "private", Prefix: "test/doctorchart/"},
		Recovery: media.ObjectConfig{Provider: "local", Directory: filepath.Join(t.TempDir(), "journal"), Bucket: "recovery", Prefix: "test/doctorchart/"},
	}
	files, err := storage.New(storage.Config{Type: "local", Directory: dir, Managed: &managed})
	if err != nil {
		t.Fatal(err)
	}
	catalog := stdlib.OpenDBFromPool(db)
	t.Cleanup(func() { _ = catalog.Close() })
	files.SetCatalog(catalog)
	makeStudy := func(patient int64, data []byte) (int64, string) {
		t.Helper()
		id := hex.EncodeToString(randBytes(16))
		key := fmt.Sprintf("%s-%s-4%s-8%s-%s", id[:8], id[8:12], id[13:16], id[17:20], id[20:32])
		if err := os.WriteFile(filepath.Join(dir, key), data, 0600); err != nil {
			t.Fatal(err)
		}
		var study int64
		err := db.QueryRow(ctx, `INSERT INTO patient_medical_studies (patient_id,study_name,original_file_name,stored_file_name,content_type,file_size,uploaded_at)
			VALUES($1,'Estudio de prueba','DNI-no-va-en-la-ruta.pdf',$2,'application/pdf',$3,now()) RETURNING id`, patient, key, len(data)).Scan(&study)
		if err != nil {
			t.Fatal(err)
		}
		return study, key
	}
	study, key := makeStudy(patient, syntheticStudy)
	makeStudy(otherPatient, []byte("SECRET-ATTACHMENT-OTHER"))
	var identity string
	if err := db.QueryRow(ctx, "SELECT media_identity::text FROM patients WHERE id=$1", secondPatient).Scan(&identity); err != nil {
		t.Fatal(err)
	}
	managedBytes := fixturePDF(t, "Estudio administrado")
	stored, err := files.Save(storage.WithOwner(ctx, owner, identity), "estudio-administrado.pdf", bytes.NewReader(managedBytes))
	if err != nil {
		t.Fatal(err)
	}
	asset, err := media.Encode(*stored.Asset)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, media.CatalogUpsert, stored.Asset.ID, asset); err != nil {
		t.Fatal(err)
	}
	var managedStudy int64
	if err := db.QueryRow(ctx, `INSERT INTO patient_medical_studies (patient_id,study_name,original_file_name,stored_file_name,media_asset_id,content_type,file_size,uploaded_at)
		VALUES($1,'Estudio administrado','estudio.pdf',$2::text,($2::text)::uuid,'application/pdf',$3,now()) RETURNING id`, secondPatient, stored.Key, stored.Size).Scan(&managedStudy); err != nil {
		t.Fatal(err)
	}
	if opened, _, _, err := files.OpenForExport(ctx, other, identity, stored.Asset.ID, stored.Key); err == nil {
		opened.Close()
		t.Fatal("foreign owner opened a managed attachment")
	}
	avatar := []byte("synthetic webp bytes")
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/doctorchart/owners/"+owner+"/export" || r.Header.Get("X-Internal-Secret") != "billing-fixture" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ownerId": owner, "module": "DOCTORCHART", "capturedAt": time.Now().UTC(),
			"profile":      map[string]string{"name": profileMarker},
			"secretFuture": "INTERNAL-SHOULD-NOT-BE-IN-ZIP",
			"avatar":       map[string]any{"contentType": "image/webp", "size": len(avatar), "data": avatar, "sha256": hex.EncodeToString(calculateSHA(avatar))},
		})
	}))
	t.Cleanup(billing.Close)
	service, err := New(ctx, app, files, billing.URL, "billing-fixture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	mux := http.NewServeMux()
	service.Routes(mux)
	handler := api.Gateway("gateway-fixture", api.Dispatch(mux))

	call := func(method, path, user, role, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Gateway-Secret", "gateway-fixture")
		r.Header.Set("X-User-Id", user)
		r.Header.Set("X-User-Role", role)
		if method == http.MethodPost {
			r.Header.Set("X-Doctor-Export-Profile", base64.RawURLEncoding.EncodeToString([]byte(`{"full_name":"Profesional sintético","token":"NO-EXPORTAR"}`)))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	wait := func(id string) exportState {
		t.Helper()
		for range 400 {
			var current exportState
			w := call("GET", api.DoctorExportsPath+"/"+id, owner, "COMPANY", "", 200)
			if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil {
				t.Fatal(err)
			}
			if current.Password != "" {
				t.Fatal("poll repeated secret")
			}
			if current.Status != "preparing" {
				return current
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("preparation timed out")
		return exportState{}
	}
	call("POST", api.DoctorExportsPath, owner, "STAFF", "{}", 403)
	call("POST", api.DoctorExportsPath, "", "COMPANY", "{}", 401)
	call("POST", api.DoctorExportsPath, owner, "COMPANY", `{"ownerId":"other"}`, 400)
	call("GET", api.DoctorExportsPath, owner, "COMPANY", "", 404)
	var started exportState
	body := fmt.Sprintf(`{"browserPreferences":{"theme":"dark","selectedContextId":"%d","scheduleDrafts":[{"clinicId":"99999999","version":"0","draft":{"weekly":[],"exceptions":[]}}]}}`, foreign)
	if err := json.Unmarshal(call("POST", api.DoctorExportsPath, owner, "COMPANY", body, 202).Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	path := api.DoctorExportsPath + "/" + started.ID
	call("GET", path, other, "COMPANY", "", 404)
	call("GET", path+"/file", other, "COMPANY", "", 404)
	call("GET", api.DoctorExportsPath, other, "COMPANY", "", 404)
	var current exportState
	if err := json.Unmarshal(call("GET", api.DoctorExportsPath, owner, "COMPANY", "", 200).Body.Bytes(), &current); err != nil ||
		current.ID != started.ID || current.Password != "" {
		t.Fatalf("current export discovery must return the job without the key: %+v %v", current, err)
	}
	ready := wait(started.ID)
	if ready.Status != "ready" {
		t.Fatalf("preparation failed: %+v", ready)
	}
	w := call("GET", path+"/file", owner, "COMPANY", "", 200)
	data := w.Body.Bytes()
	if w.Header().Get("Content-Type") != "application/zip" || len(data) != int(ready.Bytes) ||
		w.Header().Get("Content-Digest") != "sha-256=:"+base64.StdEncoding.EncodeToString(calculateSHA(data))+":" {
		t.Fatal("wrong download metadata")
	}
	filesInZIP := unpack(t, data, started.Password)
	if !bytes.Equal(filesInZIP[fmt.Sprintf("documentos/documento-%d.pdf", document)], syntheticPDF) ||
		!bytes.Equal(filesInZIP[fmt.Sprintf("adjuntos/estudio-%d.pdf", study)], syntheticStudy) ||
		!bytes.Equal(filesInZIP[fmt.Sprintf("adjuntos/estudio-%d.pdf", managedStudy)], managedBytes) ||
		!bytes.Equal(filesInZIP["adjuntos/perfil.webp"], avatar) {
		t.Fatal("file bytes did not round-trip")
	}
	for path, required := range map[string][]string{
		"INDICE.html":                 {fmt.Sprintf("contexto-%d", first), fmt.Sprintf("contexto-%d", second)},
		"datos/especialidades.ndjson": {"wizard-actual", "wizard-borrador"},
		fmt.Sprintf("datos/contexto-%d/consultas.ndjson", first): {"wizard-historico", "9007199254740993"},
		fmt.Sprintf("lectura/contexto-%d/indice.html", first):    {"Paciente &lt;sintético&gt;"},
		fmt.Sprintf("lectura/contexto-%d/consultas.html", first): {"<dt>Campo histórico</dt><dd>9007199254740993</dd>"},
		fmt.Sprintf("lectura/contexto-%d/estudios.html", first):  {fmt.Sprintf("../../adjuntos/estudio-%d.pdf", study)},
		"datos/preferencias-navegador.json":                      {`"theme":"dark"`},
		"datos/perfil.json":                                      {profileMarker},
	} {
		for _, value := range required {
			if !bytes.Contains(filesInZIP[path], []byte(value)) {
				t.Fatalf("expected %q in %s", value, path)
			}
		}
	}
	for path, bytes := range filesInZIP {
		for _, forbidden := range []string{other, "SECRET-AISLADO", "SECRET-ATTACHMENT-OTHER", "INTERNAL-SHOULD-NOT-BE-IN-ZIP", "NO-EXPORTAR", "99999999", "selectedContextId", key} {
			if strings.Contains(string(bytes), forbidden) || strings.Contains(path, forbidden) {
				t.Fatalf("leaked %q in %q", forbidden, path)
			}
		}
		if strings.Contains(path, "DNI-") {
			t.Fatalf("entry reveals original filename: %s", path)
		}
	}
	verifyLocalLinks(t, filesInZIP)
	var manifest struct{ Files []exportFile }
	if err := json.Unmarshal(filesInZIP["MANIFIESTO.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != len(filesInZIP)-1 {
		t.Fatal("manifest does not list all payload entries")
	}
	for _, entry := range manifest.Files {
		content, ok := filesInZIP[entry.Path]
		if !ok || int64(len(content)) != entry.Bytes || hex.EncodeToString(calculateSHA(content)) != entry.SHA256 {
			t.Fatalf("bad manifest entry %s", entry.Path)
		}
	}
	if os.Getenv("DOCTORCHART_EXPORT_TEST_7ZIP") == "wsl" {
		path := filepath.Join(t.TempDir(), "synthetic-export.zip")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		winPath := filepath.ToSlash(path)
		linuxPath, err := exec.Command("wsl", "-d", "Ubuntu", "--", "wslpath", "-a", winPath).CombinedOutput()
		if err != nil {
			t.Fatalf("convert fixture path for WSL: %v %s", err, linuxPath)
		}
		output, err := exec.Command("wsl", "-d", "Ubuntu", "--", "7z", "t", "-p"+started.Password, strings.TrimSpace(string(linuxPath))).CombinedOutput()
		if err != nil {
			t.Fatalf("7-Zip could not test the archive: %s", output)
		}
		actual, err := exec.Command("wsl", "-d", "Ubuntu", "--", "7z", "x", "-so", "-p"+started.Password, strings.TrimSpace(string(linuxPath)), fmt.Sprintf("adjuntos/estudio-%d.pdf", study)).Output()
		if err != nil || !bytes.Equal(actual, syntheticStudy) {
			t.Fatalf("7-Zip could not extract the original attachment: %v", err)
		}
	}
	var auditCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM clinical_access_events WHERE owner_user_id=$1 AND operation IN ('PRACTICE_EXPORT_PREPARE','PRACTICE_EXPORT_DOWNLOAD')`, owner).Scan(&auditCount); err != nil || auditCount != 4 {
		t.Fatalf("wrong audit count %d: %v", auditCount, err)
	}
	if err := os.Remove(filepath.Join(dir, key)); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(call("POST", api.DoctorExportsPath, owner, "COMPANY", "{}", 202).Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if state := wait(started.ID); state.Status != "failed" || !strings.Contains(state.Error, "adjunto") {
		t.Fatalf("missing file was not diagnosed: %+v", state)
	}
	call("GET", api.DoctorExportsPath+"/"+started.ID+"/file", owner, "COMPANY", "", 404)
}

func fixturePDF(t *testing.T, title string) []byte {
	t.Helper()
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "", 16)
	pdf.Cell(0, 12, title)
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func verifyLocalLinks(t *testing.T, entries map[string][]byte) {
	t.Helper()
	linkPattern := regexp.MustCompile(`<a href="([^"]+)"`)
	for name, document := range entries {
		if path.Ext(name) != ".html" {
			continue
		}
		for _, match := range linkPattern.FindAllSubmatch(document, -1) {
			href, fragment, _ := strings.Cut(string(match[1]), "#")
			target := path.Join(path.Dir(name), href)
			body, ok := entries[target]
			if !ok || fragment != "" && !bytes.Contains(body, []byte(`id="`+fragment+`"`)) {
				t.Fatalf("%s has an unresolved link to %s", name, match[1])
			}
		}
	}
}

func unpack(t *testing.T, raw []byte, password string) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string][]byte)
	for _, entry := range reader.File {
		if !entry.IsEncrypted() {
			t.Fatalf("entry exposed: %s", entry.Name)
		}
		entry.SetPassword(password)
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		secure, err := io.ReadAll(file)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		result[entry.Name] = secure
	}
	return result
}
