package clinical

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

const maxChartArchiveBytes = 128 << 20
const maxChartEntries = 4096

func chartArchiveTooLarge() error {
	return api.Failure(413, "CHART_EXPORT_LIMIT", "La ficha supera el límite de preparación segura. El paquete no fue entregado.")
}

type chartEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type chartArchive struct {
	zip      *zip.Writer
	entries  []chartEntry
	left     int64
	manifest []byte
}

type chartBudgetWriter struct {
	archive *chartArchive
	output  io.Writer
}

func (w chartBudgetWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.archive.left {
		return 0, chartArchiveTooLarge()
	}
	n, err := w.output.Write(data)
	w.archive.left -= int64(n)
	return n, err
}

func (a *chartArchive) add(name string, write func(io.Writer) error) error {
	if len(a.entries) >= maxChartEntries-1 {
		return chartArchiveTooLarge()
	}
	entry, err := a.zip.Create(name)
	if err != nil {
		return err
	}
	digest, before := sha256.New(), a.left
	if err := write(chartBudgetWriter{a, io.MultiWriter(entry, digest)}); err != nil {
		return err
	}
	a.entries = append(a.entries, chartEntry{name, before - a.left, hex.EncodeToString(digest.Sum(nil))})
	return nil
}

var chartSections = []struct{ name, query string }{
	{"ficha", `SELECT to_jsonb(p)-ARRAY['media_identity','workspace_id'] FROM patients p WHERE p.id=$1`},
	{"consultas", `SELECT to_jsonb(e)-'workspace_id' FROM clinical_encounters e WHERE e.patient_id=$1 ORDER BY e.encounter_date,e.id`},
	{"recetas-y-ordenes", `SELECT to_jsonb(p) || jsonb_build_object(
		'medications',coalesce((SELECT jsonb_agg(to_jsonb(m) ORDER BY m.item_position) FROM prescription_medications m WHERE m.record_id=p.id),'[]'::jsonb),
		'orders',coalesce((SELECT jsonb_agg(to_jsonb(o) ORDER BY o.item_position) FROM prescription_diagnostic_orders o WHERE o.record_id=p.id),'[]'::jsonb))
		FROM patient_prescription_records p WHERE p.patient_id=$1 ORDER BY p.created_at,p.id`},
	{"estudios", `SELECT to_jsonb(s)-ARRAY['stored_file_name','media_asset_id'] FROM patient_medical_studies s WHERE s.patient_id=$1 ORDER BY s.uploaded_at,s.id`},
	{"vacunas", `SELECT to_jsonb(v)||jsonb_build_object('dose',to_jsonb(d),'calendar',c.definition) FROM patient_vaccinations v
		LEFT JOIN vaccination_doses d ON d.id=v.dose_id LEFT JOIN vaccination_calendars c ON c.id=d.calendar_id
		WHERE v.patient_id=$1 ORDER BY v.applied_at,v.id`},
	{"calculos", `SELECT to_jsonb(c) FROM patient_calculation_logs c WHERE c.patient_id=$1 ORDER BY c.created_at,c.id`},
	{"revisiones", `SELECT (to_jsonb(r)-ARRAY['workspace_id','previous_content']) ||
		jsonb_build_object('previous_content',r.previous_content-ARRAY['stored_file_name','media_asset_id','media_identity','workspace_id'])
		FROM clinical_revisions r WHERE r.patient_id=$1 ORDER BY r.occurred_at,r.id`},
	{"emisiones", `SELECT (to_jsonb(i)-'pdf')||jsonb_build_object('annulled_at',a.annulled_at,'annulment_reason',a.reason,
		'status',CASE WHEN a.document_id IS NULL THEN 'ISSUED' ELSE 'ANNULLED' END)
		FROM clinical_document_issues i LEFT JOIN clinical_document_annulments a ON a.document_id=i.id
		WHERE i.patient_id=$1 ORDER BY i.issued_at,i.id`},
	{"cronologia", `SELECT jsonb_build_object('fecha',fecha,'tipo',tipo,'id',id,'anulado',anulado)
		FROM (
			SELECT encounter_date fecha,'CONSULTA' tipo,id,annulled_at IS NOT NULL anulado FROM clinical_encounters WHERE patient_id=$1
			UNION ALL SELECT created_at,'RECETA_ORDEN',id,annulled_at IS NOT NULL FROM patient_prescription_records WHERE patient_id=$1
			UNION ALL SELECT uploaded_at,'ESTUDIO',id,annulled_at IS NOT NULL FROM patient_medical_studies WHERE patient_id=$1
			UNION ALL SELECT applied_at::timestamp AT TIME ZONE 'UTC','VACUNA',id,annulled_at IS NOT NULL FROM patient_vaccinations WHERE patient_id=$1
			UNION ALL SELECT created_at,'CALCULO',id,false FROM patient_calculation_logs WHERE patient_id=$1
			UNION ALL SELECT occurred_at,operation,id,operation='ANNUL' FROM clinical_revisions WHERE patient_id=$1
			UNION ALL SELECT issued_at,'EMISION',id,false FROM clinical_document_issues WHERE patient_id=$1
			UNION ALL SELECT a.annulled_at,'ANULACION_DOCUMENTAL',i.id,true FROM clinical_document_annulments a
				JOIN clinical_document_issues i ON i.id=a.document_id WHERE i.patient_id=$1
		) timeline ORDER BY fecha,tipo,id`},
}

type chartAttachment struct {
	key, contentType string
	id, size         int64
}

func (s *Service) chartRows(ctx context.Context, archive *chartArchive, patient int64) (string, []chartAttachment, error) {
	tx, err := s.beginClinicalTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return "", nil, err
	}
	defer rollbackClinicalTx(ctx, tx)
	if _, err := readArchivedPatient(ctx, tx, api.User(ctx).UserID, patient); err != nil {
		return "", nil, err
	}
	var specialty string
	if err := tx.QueryRow(ctx, `SELECT s.display_name FROM doctor_specialty_contexts c
		JOIN specialties s ON s.id=c.specialty_id WHERE c.id=$1`, api.SpecialtyContextID(ctx)).Scan(&specialty); err != nil {
		return "", nil, err
	}
	for _, section := range chartSections {
		err := archive.add("datos/"+section.name+".ndjson", func(out io.Writer) error {
			rows, err := tx.Query(ctx, section.query, patient)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var data []byte
				if err := rows.Scan(&data); err != nil {
					return err
				}
				data, err = chartRowProjections(section.name, data)
				if err != nil {
					return err
				}
				if _, err := out.Write(append(data, '\n')); err != nil {
					return err
				}
			}
			return rows.Err()
		})
		if err != nil {
			return "", nil, err
		}
	}
	if err := archive.add("LECTURA.html", func(out io.Writer) error {
		if _, err := io.WriteString(out, "<!doctype html><html lang=\"es\"><meta charset=\"utf-8\"><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'\"><title>Ficha contextual DoctorChart</title><style>body{font:16px sans-serif;max-width:70rem;margin:2rem auto;padding:1rem}pre{white-space:pre-wrap;overflow-wrap:anywhere}h2{border-bottom:1px solid}</style><h1>Copia de ficha contextual</h1><p>Los registros con fecha de anulación conservan su carácter histórico. Esta copia comprende una ficha y un contexto.</p>"); err != nil {
			return err
		}
		for _, section := range chartSections {
			if _, err := fmt.Fprintf(out, "<h2>%s</h2>", html.EscapeString(section.name)); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, section.query, patient)
			if err != nil {
				return err
			}
			for rows.Next() {
				var data []byte
				if err := rows.Scan(&data); err != nil {
					rows.Close()
					return err
				}
				data, err = chartRowProjections(section.name, data)
				if err != nil {
					rows.Close()
					return err
				}
				var formatted bytes.Buffer
				if err := json.Indent(&formatted, data, "", "  "); err != nil {
					rows.Close()
					return err
				}
				if _, err := fmt.Fprintf(out, "<pre>%s</pre>", html.EscapeString(formatted.String())); err != nil {
					rows.Close()
					return err
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		_, err := io.WriteString(out, "</html>")
		return err
	}); err != nil {
		return "", nil, err
	}
	rows, err := tx.Query(ctx, "SELECT id,pdf,sha256 FROM clinical_document_issues WHERE patient_id=$1 ORDER BY id", patient)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var pdf []byte
		var sha string
		if err := rows.Scan(&id, &pdf, &sha); err != nil {
			return "", nil, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(pdf)) != sha {
			return "", nil, chartAttachmentUnavailable()
		}
		if err := archive.add(fmt.Sprintf("emisiones/documento-%d.pdf", id), func(out io.Writer) error {
			_, err := out.Write(pdf)
			return err
		}); err != nil {
			return "", nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	rows, err = tx.Query(ctx, "SELECT id,stored_file_name,content_type,file_size FROM patient_medical_studies WHERE patient_id=$1 ORDER BY id", patient)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	attachments := []chartAttachment{}
	remaining := archive.left
	for rows.Next() {
		var attachment chartAttachment
		if err := rows.Scan(&attachment.id, &attachment.key, &attachment.contentType, &attachment.size); err != nil {
			return "", nil, err
		}
		if attachment.size <= 0 {
			return "", nil, chartAttachmentUnavailable()
		}
		if len(archive.entries)+len(attachments) >= maxChartEntries || attachment.size > remaining {
			return "", nil, chartArchiveTooLarge()
		}
		remaining -= attachment.size
		attachments = append(attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	return specialty, attachments, tx.Commit(ctx)
}

func chartAttachmentUnavailable() error {
	return api.Failure(409, "CHART_EXPORT_INCOMPLETE", "Un adjunto o documento conservado no está disponible o no superó la verificación. El paquete no fue entregado.")
}

func (s *Service) addChartAttachment(ctx context.Context, archive *chartArchive, attachment chartAttachment) error {
	src, size, expected, err := s.files.OpenWithDigest(ctx, attachment.key)
	if err != nil {
		return chartAttachmentUnavailable()
	}
	defer src.Close()
	if size != attachment.size {
		return chartAttachmentUnavailable()
	}
	extension := map[string]string{"application/pdf": ".pdf", "image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[attachment.contentType]
	if extension == "" {
		extension = ".bin"
	}
	err = archive.add(fmt.Sprintf("adjuntos/estudio-%d%s", attachment.id, extension), func(out io.Writer) error {
		n, err := io.Copy(out, io.LimitReader(src, size+1))
		if err != nil || n != size {
			return chartAttachmentUnavailable()
		}
		return nil
	})
	if err == nil && expected != "" && archive.entries[len(archive.entries)-1].SHA256 != expected {
		return chartAttachmentUnavailable()
	}
	return err
}

func (s *Service) prepareChart(ctx context.Context, archive *chartArchive, patient int64) error {
	specialty, attachments, err := s.chartRows(ctx, archive, patient)
	if err != nil {
		return err
	}
	for _, attachment := range attachments {
		if err := s.addChartAttachment(ctx, archive, attachment); err != nil {
			return err
		}
	}
	prepared := time.Now().UTC()
	index := fmt.Sprintf("DoctorChart\nCopia técnica de una ficha contextual\nPaciente interno: %d\nContexto: %d\nEspecialidad: %s\nPreparación (UTC): %s\n\n"+
		"LECTURA.html presenta los datos y la cronología para su consulta local. datos/cronologia.ndjson ordena los eventos por fecha. Las secciones NDJSON contienen un registro JSON por línea. "+
		"El campo annulled_at y los estados de anulación identifican registros históricos.\n"+
		"derivedProjections contiene una interpretación derivada del snapshot propio. form_values, form_definition_snapshot y previous_content conservan los datos originales, incluso contradicciones y campos desconocidos.\n"+
		"Las mediciones y el crecimiento conservan los valores guardados en las consultas. "+
		"Los adjuntos son los bytes conservados por DoctorChart, incluida la conversión previa cuando correspondió.\n"+
		"El alcance es este paciente interno y este contexto. Los demás contextos y prestadores quedan fuera. "+
		"Esta copia carece de autenticación institucional y firma digital.\n",
		patient, api.SpecialtyContextID(ctx), specialty, prepared.Format(time.RFC3339))
	if err := archive.add("INDICE.txt", func(w io.Writer) error {
		_, err := io.WriteString(w, index)
		return err
	}); err != nil {
		return err
	}
	manifest, err := archive.zip.Create("MANIFIESTO.json")
	if err != nil {
		return err
	}
	archive.manifest, err = json.MarshalIndent(map[string]any{"format": 1, "scope": "PATIENT_CONTEXT", "patientId": patient,
		"contextId": api.SpecialtyContextID(ctx), "specialty": specialty, "preparedAt": prepared,
		"omissions": []string{}, "files": archive.entries}, "", "  ")
	if err != nil {
		return err
	}
	_, err = (chartBudgetWriter{archive, manifest}).Write(archive.manifest)
	return err
}

func (s *Service) exportChart(w http.ResponseWriter, r *http.Request) {
	patient, err := pathID(r, "patientId")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	select {
	case s.exports <- struct{}{}:
		defer func() { <-s.exports }()
	default:
		api.WriteError(w, api.Failure(503, "CHART_EXPORT_BUSY", "La preparación de copias está ocupada. Reintentá en unos momentos."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	file, err := os.CreateTemp("", "doctorchart-export-")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()
	digest := sha256.New()
	archive := chartArchive{zip: zip.NewWriter(io.MultiWriter(file, digest)), left: maxChartArchiveBytes, entries: []chartEntry{}}
	err = s.prepareChart(ctx, &archive, patient)
	if err == nil {
		err = archive.zip.Close()
	}
	var size int64
	if err == nil {
		size, err = file.Seek(0, io.SeekCurrent)
	}
	if err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err != nil {
		api.WriteError(w, err)
		return
	}
	audit, ok := ctx.Value(accessKey{}).(*accessWriter)
	if !ok {
		api.WriteError(w, auditUnavailable())
		return
	}
	accessReference(ctx, &patient, &patient, nil)
	var evidenceID int64
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO clinical_export_evidence(owner_user_id,specialty_context_id,patient_id,sha256,manifest)
			VALUES($1,$2,$3,$4,$5) RETURNING id`, api.User(ctx).UserID, api.SpecialtyContextID(ctx), patient,
			hex.EncodeToString(digest.Sum(nil)), archive.manifest).Scan(&evidenceID); err != nil {
			return err
		}
		var preparedID [16]byte
		rand.Read(preparedID[:])
		accessReference(ctx, &evidenceID, &patient, nil)
		return audit.record(ctx, tx, "CHART_EXPORT_PREPARE", "ARTIFACT_READY", hex.EncodeToString(preparedID[:]))
	})
	if err != nil {
		api.WriteError(w, auditUnavailable())
		return
	}
	accessReference(ctx, &evidenceID, &patient, nil)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"historia-contextual.zip\"")
	w.Header().Set("Content-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(digest.Sum(nil))+":")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}
