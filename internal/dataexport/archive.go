package dataexport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/db"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	zip "go.foxforensics.eu/go-zip/zip"
)

type exportFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type archive struct {
	ctx       context.Context
	zip       *zip.Writer
	password  string
	remaining int64
	entries   []exportFile
}

type archiveWriter struct {
	ctx       context.Context
	dst       io.Writer
	remaining *int64
}

func exportTooLarge() error {
	return api.Failure(413, "EXPORT_TOO_LARGE", "La práctica supera el límite de tamaño de esta copia.")
}

func (w archiveWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > *w.remaining {
		return 0, exportTooLarge()
	}
	n, err := w.dst.Write(p)
	*w.remaining -= int64(n)
	return n, err
}

func (a *archive) add(name string, write func(io.Writer) error) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if len(a.entries) >= maxEntries {
		return exportTooLarge()
	}
	dst, err := a.zip.Encrypt(name, a.password, zip.AES256Encryption)
	if err != nil {
		return err
	}
	hash := sha256.New()
	before := a.remaining
	if err := write(archiveWriter{a.ctx, io.MultiWriter(dst, hash), &a.remaining}); err != nil {
		return err
	}
	a.entries = append(a.entries, exportFile{name, before - a.remaining, hex.EncodeToString(hash.Sum(nil))})
	return nil
}

func (a *archive) addJSON(path string, data any) error {
	return a.add(path, func(dst io.Writer) error { return json.NewEncoder(dst).Encode(data) })
}

type buildResult struct {
	cut       time.Time
	bytes     int64
	digest    string
	contexts  []int64
	file      *os.File
	namedPath string
}

type specialtyContext struct {
	ID   int64
	Name string
}

type attachment struct {
	key             string
	assetID         string
	patientIdentity string
	contentType     string
	id              int64
	size            int64
}

func (s *Service) build(ctx context.Context, exportID, owner, password string, prefs browserPreferences, profile map[string]string) (buildResult, error) {
	f, err := os.CreateTemp(s.dir, exportID+"-*.zip")
	if err != nil {
		return buildResult{}, err
	}
	path := f.Name()
	// Linux can unlink the path while the open handle remains readable.
	// On those systems even a process crash releases the temporary archive.
	if err := os.Remove(path); err == nil {
		path = ""
	}
	keep := false
	defer func() {
		if !keep {
			_ = f.Close()
			if path != "" {
				_ = os.Remove(path)
			}
		}
	}()
	digest := sha256.New()
	remaining := int64(maxZipBytes)
	a := archive{ctx: ctx, zip: zip.NewWriter(archiveWriter{ctx, io.MultiWriter(f, digest), &remaining}), password: password, remaining: maxRawBytes}
	cut, contexts, attachments, counts, err := s.snapshot(ctx, &a, owner, prefs)
	if err != nil {
		return buildResult{}, err
	}
	if err := a.addJSON("datos/perfil.json", profile); err != nil {
		return buildResult{}, err
	}
	if err := a.add("lectura/perfil.html", func(w io.Writer) error { return writeObjectPage(w, "Perfil", profile) }); err != nil {
		return buildResult{}, err
	}
	billingCut, err := s.exportBilling(ctx, &a, owner)
	if err != nil {
		return buildResult{}, err
	}
	if err := s.exportAttachments(ctx, &a, owner, attachments); err != nil {
		return buildResult{}, err
	}
	if err := a.add("INDICE.html", func(w io.Writer) error { return writeIndex(w, cut, contexts) }); err != nil {
		return buildResult{}, err
	}

	manifest := struct {
		Format       string         `json:"format"`
		Version      int            `json:"version"`
		Owner        string         `json:"ownerId"`
		Cut          time.Time      `json:"cut"`
		BillingCut   time.Time      `json:"billingCut"`
		BrowserScope string         `json:"browserScope"`
		Counts       map[string]int `json:"counts"`
		Files        []exportFile   `json:"files"`
	}{"doctorchart-practice-export", 1, owner, cut, billingCut, "Datos disponibles en el navegador al solicitar la copia.", counts, a.entries}
	if err := a.addJSON("MANIFIESTO.json", manifest); err != nil {
		return buildResult{}, err
	}
	if err := a.zip.Close(); err != nil {
		return buildResult{}, err
	}
	ids := make([]int64, 0, len(contexts))
	for _, context := range contexts {
		ids = append(ids, context.ID)
	}
	if err := s.audit(ctx, exportID, owner, ids, "PRACTICE_EXPORT_PREPARE", "ARTIFACT_READY"); err != nil {
		return buildResult{}, err
	}
	keep = true
	return buildResult{cut, maxZipBytes - remaining, base64.StdEncoding.EncodeToString(digest.Sum(nil)), ids, f, path}, nil
}

func (s *Service) snapshot(ctx context.Context, a *archive, owner string, prefs browserPreferences) (time.Time, []specialtyContext, []attachment, map[string]int, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return time.Time{}, nil, nil, nil, err
	}
	defer func() {
		rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollback)
	}()

	var cut time.Time
	if err := tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&cut); err != nil {
		return time.Time{}, nil, nil, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT c.id,s.display_name FROM doctor_specialty_contexts c
		JOIN doctor_workspaces w ON w.id=c.workspace_id JOIN specialties s ON s.id=c.specialty_id
		WHERE w.user_id=$1 ORDER BY c.id`, owner)
	if err != nil {
		return time.Time{}, nil, nil, nil, err
	}
	contexts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[specialtyContext])
	if err != nil {
		return time.Time{}, nil, nil, nil, err
	}
	prefs, err = scopePreferences(ctx, tx, owner, contexts, prefs)
	if err != nil {
		return time.Time{}, nil, nil, nil, err
	}
	if err := a.addJSON("datos/preferencias-navegador.json", prefs); err != nil {
		return time.Time{}, nil, nil, nil, err
	}
	if err := a.add("lectura/preferencias-navegador.html", func(w io.Writer) error { return writeObjectPage(w, "Preferencias de este navegador", prefs) }); err != nil {
		return time.Time{}, nil, nil, nil, err
	}

	counts := make(map[string]int)
	if err := writeSections(ctx, a, tx, "", workspaceSections, counts, owner); err != nil {
		return time.Time{}, nil, nil, nil, err
	}

	var attachments []attachment
	for _, sc := range contexts {
		if err := db.BindClinicalScope(ctx, tx, owner, sc.ID); err != nil {
			return time.Time{}, nil, nil, nil, err
		}
		directory := fmt.Sprintf("contexto-%d/", sc.ID)
		if err := writeSections(ctx, a, tx, directory, contextSections, counts, owner, sc.ID); err != nil {
			return time.Time{}, nil, nil, nil, err
		}
		if err := a.add("lectura/"+directory+"indice.html", func(out io.Writer) error {
			return writeContext(ctx, tx, out, owner, sc)
		}); err != nil {
			return time.Time{}, nil, nil, nil, err
		}
		if err := writePDFs(ctx, a, tx, owner, sc.ID); err != nil {
			return time.Time{}, nil, nil, nil, err
		}
		rows, err := tx.Query(ctx, `SELECT s.stored_file_name, COALESCE(s.media_asset_id::text,''), p.media_identity::text,
			s.content_type,s.id,s.file_size FROM patient_medical_studies s JOIN patients p ON p.id=s.patient_id
			WHERE p.user_id=$1 AND p.specialty_context_id=$2 ORDER BY s.id`, owner, sc.ID)
		if err != nil {
			return time.Time{}, nil, nil, nil, err
		}
		for rows.Next() {
			var item attachment
			if err := rows.Scan(&item.key, &item.assetID, &item.patientIdentity, &item.contentType, &item.id, &item.size); err != nil {
				rows.Close()
				return time.Time{}, nil, nil, nil, err
			}
			if len(attachments)+len(a.entries) >= maxEntries {
				rows.Close()
				return time.Time{}, nil, nil, nil, exportTooLarge()
			}
			attachments = append(attachments, item)
		}
		if err := rows.Err(); err != nil {
			return time.Time{}, nil, nil, nil, err
		}
	}
	return cut.UTC(), contexts, attachments, counts, tx.Commit(ctx)
}

func scopePreferences(ctx context.Context, tx pgx.Tx, owner string, contexts []specialtyContext, prefs browserPreferences) (browserPreferences, error) {
	matches := false
	for _, sc := range contexts {
		if prefs.SelectedContextID == strconv.FormatInt(sc.ID, 10) {
			matches = true
		}
	}
	if !matches {
		prefs.SelectedContextID = ""
	}
	if len(prefs.ScheduleDrafts) == 0 {
		return prefs, nil
	}
	rows, err := tx.Query(ctx, `SELECT c.id::text FROM clinics c JOIN doctor_workspaces w ON w.id=c.workspace_id WHERE w.user_id=$1`, owner)
	if err != nil {
		return browserPreferences{}, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return browserPreferences{}, err
	}
	owned := make(map[string]bool, len(ids))
	for _, id := range ids {
		owned[id] = true
	}
	drafts := prefs.ScheduleDrafts[:0]
	for _, draft := range prefs.ScheduleDrafts {
		if owned[draft.ClinicID] {
			drafts = append(drafts, draft)
		}
	}
	prefs.ScheduleDrafts = drafts
	return prefs, nil
}

func writeSections(ctx context.Context, a *archive, tx pgx.Tx, folder string, sections []section, counts map[string]int, arguments ...any) error {
	for _, section := range sections {
		dataPath := "datos/" + folder + section.name + ".ndjson"
		count := 0
		err := a.add(dataPath, func(w io.Writer) error {
			return visitRows(ctx, tx, section.query(), func(row []byte, _ *int64) error {
				count++
				_, err := w.Write(append(row, '\n'))
				return err
			}, arguments...)
		})
		if err != nil {
			return fmt.Errorf("%s: %w", section.name, err)
		}
		counts[dataPath] = count
		if err := a.add("lectura/"+folder+section.name+".html", func(w io.Writer) error {
			return writeSectionPage(ctx, tx, w, section, folder, arguments...)
		}); err != nil {
			return fmt.Errorf("%s: %w", section.name, err)
		}
	}
	return nil
}

func visitRows(ctx context.Context, tx pgx.Tx, query string, visit func([]byte, *int64) error, arguments ...any) error {
	rows, err := tx.Query(ctx, query, arguments...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var json []byte
		var patient *int64
		if err := rows.Scan(&json, &patient); err != nil {
			return err
		}
		if err := visit(json, patient); err != nil {
			return err
		}
	}
	return rows.Err()
}

func writePDFs(ctx context.Context, a *archive, tx pgx.Tx, owner string, contextID int64) error {
	rows, err := tx.Query(ctx, `SELECT id,pdf,sha256 FROM clinical_document_issues WHERE owner_user_id=$1 AND specialty_context_id=$2 ORDER BY id`, owner, contextID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var pdf []byte
		var expected string
		if err := rows.Scan(&id, &pdf, &expected); err != nil {
			return err
		}
		if hex.EncodeToString(calculateSHA(pdf)) != expected {
			return attachmentUnavailable()
		}
		if err := a.add(fmt.Sprintf("documentos/documento-%d.pdf", id), func(w io.Writer) error {
			_, err := w.Write(pdf)
			return err
		}); err != nil {
			return err
		}
	}
	return rows.Err()
}

func calculateSHA(data []byte) []byte { sum := sha256.Sum256(data); return sum[:] }

func attachmentUnavailable() error {
	return api.Failure(409, "EXPORT_INCOMPLETE", "Un adjunto o documento no está disponible o no superó la verificación. Volvé a intentar.")
}

func (s *Service) exportAttachments(ctx context.Context, a *archive, owner string, files []attachment) error {
	for _, file := range files {
		if file.size < 1 {
			return attachmentUnavailable()
		}
		if file.size > a.remaining {
			return exportTooLarge()
		}
		name := fmt.Sprintf("adjuntos/estudio-%d%s", file.id, studyExtension(file.contentType))
		expected := ""
		err := a.add(name, func(w io.Writer) error {
			source, size, hash, err := s.files.OpenForExport(ctx, owner, file.patientIdentity, file.assetID, file.key)
			if err != nil {
				return attachmentUnavailable()
			}
			defer source.Close()
			if size != file.size {
				return attachmentUnavailable()
			}
			expected = hash
			actual, err := io.Copy(w, io.LimitReader(source, size+1))
			if err != nil {
				return err
			}
			if actual != size {
				return attachmentUnavailable()
			}
			return nil
		})
		if err != nil {
			return err
		}
		if expected != "" && a.entries[len(a.entries)-1].SHA256 != expected {
			return attachmentUnavailable()
		}
	}
	return nil
}

func studyExtension(contentType string) string {
	ext := map[string]string{"application/pdf": ".pdf", "image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif", "image/bmp": ".bmp", "image/tiff": ".tiff"}[contentType]
	if ext == "" {
		return ".bin"
	}
	return ext
}

const pageStart = `<!doctype html><html lang="es"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><title>Doctor Chart</title><style>body{max-width:64rem;margin:2rem auto;padding:0 1rem;font:1rem/1.55 system-ui,sans-serif}a{color:#14525f}article{border-top:1px solid #ccc;padding:1rem 0}summary{cursor:pointer}pre{white-space:pre-wrap;overflow-wrap:anywhere}li{margin:.25rem 0}</style>`

func writeIndex(w io.Writer, cut time.Time, contexts []specialtyContext) error {
	_, err := fmt.Fprintf(w, "%s<h1>Copia de seguridad de Doctor Chart</h1><p>Datos guardados al %s (UTC).</p><h2>Especialidades e historia clínica</h2><ul>", pageStart, cut.Format("02/01/2006 15:04"))
	if err != nil {
		return err
	}
	for _, sc := range contexts {
		if _, err := fmt.Fprintf(w, `<li><a href="lectura/contexto-%d/indice.html">%s</a></li>`, sc.ID, html.EscapeString(sc.Name)); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, `</ul><h2>Configuración, agenda y equipo</h2><ul><li><a href="lectura/perfil.html">Perfil profesional</a></li><li><a href="lectura/preferencias-navegador.html">Preferencias de este navegador</a></li><li><a href="lectura/facturacion.html">Cuenta y facturación</a></li>`); err != nil {
		return err
	}
	for _, section := range workspaceSections {
		if _, err := fmt.Fprintf(w, `<li><a href="lectura/%s.html">%s</a></li>`, section.name, sectionTitle(section.name)); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, `</ul><p><a href="MANIFIESTO.json">Inventario de archivos</a></p></html>`)
	return err
}

func writeContext(ctx context.Context, tx pgx.Tx, w io.Writer, owner string, sc specialtyContext) error {
	if _, err := fmt.Fprintf(w, `%s<p><a href="../../INDICE.html">Volver al índice</a></p><h1>%s</h1><ul>`, pageStart, html.EscapeString(sc.Name)); err != nil {
		return err
	}
	for _, section := range contextSections {
		if _, err := fmt.Fprintf(w, `<li><a href="%s.html">%s</a></li>`, section.name, sectionTitle(section.name)); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "</ul><h2>Pacientes</h2>"); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT p.id,p.full_name,p.annulled_at IS NOT NULL,
		EXISTS(SELECT 1 FROM clinical_encounters e WHERE e.patient_id=p.id),
		EXISTS(SELECT 1 FROM patient_medical_studies s WHERE s.patient_id=p.id),
		EXISTS(SELECT 1 FROM patient_prescription_records r WHERE r.patient_id=p.id),
		EXISTS(SELECT 1 FROM patient_vaccinations v WHERE v.patient_id=p.id),
		EXISTS(SELECT 1 FROM clinical_document_issues d WHERE d.patient_id=p.id),
		EXISTS(SELECT 1 FROM patient_whatsapp_consultations w WHERE w.patient_id=p.id)
		FROM patients p WHERE p.user_id=$1 AND p.specialty_context_id=$2 ORDER BY p.full_name,p.id`, owner, sc.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		var archived bool
		var canVisit [7]bool
		canVisit[0] = true
		if err := rows.Scan(&id, &name, &archived, &canVisit[1], &canVisit[2], &canVisit[3], &canVisit[4], &canVisit[5], &canVisit[6]); err != nil {
			return err
		}
		status := ""
		if archived {
			status = " (anulada)"
		}
		if _, err := fmt.Fprintf(w, `<article><h3>%s%s</h3><p>Ficha %d</p><ul>`, html.EscapeString(name), status, id); err != nil {
			return err
		}
		for i, section := range []string{"fichas", "consultas", "estudios", "recetas-y-ordenes", "vacunas", "emisiones", "consultas-whatsapp"} {
			if !canVisit[i] {
				continue
			}
			if _, err := fmt.Fprintf(w, `<li><a href="%s.html#paciente-%d">%s</a></li>`, section, id, sectionTitle(section)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, "</ul></article>"); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = io.WriteString(w, "</html>")
	return err
}

func sectionTitle(name string) string {
	titles := map[string]string{"fichas": "Fichas", "consultas": "Consultas", "estudios": "Estudios y adjuntos", "recetas-y-ordenes": "Recetas y órdenes", "vacunas": "Vacunas", "emisiones": "Documentos emitidos", "consultas-whatsapp": "Consultas por WhatsApp", "especialidades": "Especialidades, wizards y borradores", "calculos": "Cálculos", "auditoria-accesos": "Auditoría de accesos"}
	if title := titles[name]; title != "" {
		return title
	}
	title := strings.ReplaceAll(name, "-", " ")
	return strings.ToUpper(title[:1]) + title[1:]
}

func writeSectionPage(ctx context.Context, tx pgx.Tx, w io.Writer, section section, folder string, arguments ...any) error {
	up := "../"
	if folder != "" {
		up = "../../"
	}
	if _, err := fmt.Fprintf(w, `%s<p><a href="%sINDICE.html">Volver al índice</a></p><h1>%s</h1>`, pageStart, up, sectionTitle(section.name)); err != nil {
		return err
	}
	if folder != "" {
		if _, err := io.WriteString(w, `<p><a href="indice.html">Ver esta especialidad</a></p>`); err != nil {
			return err
		}
	}
	seen := make(map[int64]bool)
	count := 0
	err := visitRows(ctx, tx, section.query(), func(row []byte, patient *int64) error {
		count++
		anchor := ""
		if patient != nil && !seen[*patient] {
			seen[*patient] = true
			anchor = fmt.Sprintf(` id="paciente-%d"`, *patient)
		}
		if _, err := fmt.Fprintf(w, "<article%s>", anchor); err != nil {
			return err
		}
		if err := writeRecord(w, row, up, section.table); err != nil {
			return err
		}
		_, err := io.WriteString(w, "</article>")
		return err
	}, arguments...)
	if err != nil {
		return err
	}
	if count == 0 {
		if _, err := io.WriteString(w, "<p>No hay registros en esta sección.</p>"); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, "</html>")
	return err
}

func writeRecord(w io.Writer, row []byte, up, table string) error {
	var summary struct {
		ID        string          `json:"id"`
		Name      string          `json:"full_name"`
		Study     string          `json:"study_name"`
		Original  string          `json:"original_file_name"`
		Code      string          `json:"code"`
		When        string          `json:"encounter_date"`
		ConsultedAt string          `json:"consulted_at"`
		Complaint   string          `json:"chief_complaint"`
		File      string          `json:"archivo"`
		Form      json.RawMessage `json:"form_definition_snapshot"`
		Values    json.RawMessage `json:"form_values"`
	}
	if err := json.Unmarshal(row, &summary); err != nil {
		return err
	}
	label := summary.Name
	if table == "clinical_encounters" {
		label = strings.TrimSpace(summary.When + " " + summary.Complaint)
	}
	if table == "patient_medical_studies" {
		label = summary.Study
	}
	if table == "clinical_document_issues" {
		label = summary.Code
	}
	if table == "patient_whatsapp_consultations" {
		label = strings.TrimSpace("Consulta por WhatsApp " + summary.ConsultedAt)
	}
	if label == "" {
		label = strings.TrimSpace("Registro " + summary.ID)
	}
	if _, err := fmt.Fprintf(w, "<h2>%s</h2>", html.EscapeString(label)); err != nil {
		return err
	}
	if summary.File != "" {
		linkText := summary.Original
		if linkText == "" {
			linkText = "Abrir archivo"
		}
		if _, err := fmt.Fprintf(w, `<p><a href="%s%s">%s</a></p>`, up, html.EscapeString(summary.File), html.EscapeString(linkText)); err != nil {
			return err
		}
	}
	if len(summary.Values) != 0 {
		if err := writeFormValues(w, summary.Form, summary.Values); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, "<details open><summary>Ver datos</summary>"); err != nil {
		return err
	}
	if err := writePrettyJSON(w, row); err != nil {
		return err
	}
	_, err := io.WriteString(w, "</details>")
	return err
}

func writeFormValues(w io.Writer, snapshot, formValues json.RawMessage) error {
	var form struct {
		Categories []struct {
			Fields []struct {
				Key   string `json:"key"`
				Label string `json:"label"`
			} `json:"fields"`
		} `json:"categories"`
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(snapshot, &form) != nil || json.Unmarshal(formValues, &values) != nil {
		return nil // The exact stored definition and values are shown below.
	}
	started := false
	seen := make(map[string]bool)
	for _, category := range form.Categories {
		for _, field := range category.Fields {
			raw, ok := values[field.Key]
			if !ok || seen[field.Key] || field.Label == "" {
				continue
			}
			seen[field.Key] = true
			if !started {
				if _, err := io.WriteString(w, "<h3>Formulario registrado</h3><dl>"); err != nil {
					return err
				}
				started = true
			}
			value := string(raw)
			if len(raw) > 0 && raw[0] == '"' {
				_ = json.Unmarshal(raw, &value)
			}
			if _, err := fmt.Fprintf(w, "<dt>%s</dt><dd>%s</dd>", html.EscapeString(field.Label), html.EscapeString(value)); err != nil {
				return err
			}
		}
	}
	if started {
		_, err := io.WriteString(w, "</dl>")
		return err
	}
	return nil
}

func writePrettyJSON(w io.Writer, raw []byte) error {
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, raw, "", "  "); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "<pre>%s</pre>", html.EscapeString(formatted.String()))
	return err
}

func writeObjectPage(w io.Writer, title string, object any) error {
	if _, err := fmt.Fprintf(w, `%s<p><a href="../INDICE.html">Volver al índice</a></p><h1>%s</h1>`, pageStart, title); err != nil {
		return err
	}
	raw, err := json.Marshal(object)
	if err != nil {
		return err
	}
	if err := writePrettyJSON(w, raw); err != nil {
		return err
	}
	_, err = io.WriteString(w, "</html>")
	return err
}
