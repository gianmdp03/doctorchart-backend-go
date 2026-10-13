package clinical

import (
	"context"
	"errors"
	"golang.org/x/text/unicode/norm"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
)

type study struct {
	ID          int64   `json:"id"`
	PatientID   int64   `json:"patientId"`
	Name        string  `json:"originalFileName"`
	ContentType string  `json:"contentType"`
	Size        int64   `json:"fileSize"`
	Description *string `json:"description"`
	Uploaded    string  `json:"uploadedAt"`
	key         string
}

const studyColumns = "id,patient_id,original_file_name,content_type,file_size,description,uploaded_at,stored_file_name"

func scanStudy(row pgx.Row) (study, error) {
	var result study
	var uploaded time.Time
	err := row.Scan(&result.ID, &result.PatientID, &result.Name, &result.ContentType, &result.Size, &result.Description, &uploaded, &result.key)
	result.Uploaded = uploaded.UTC().Format(time.RFC3339Nano)
	return result, err
}
func (s *Service) studyOwner(ctx context.Context, tx pgx.Tx, user string, patientID int64) error {
	if _, err := ownedPatient(ctx, tx, user, patientID); err != nil {
		return err
	}
	return s.workspaces.Feature(ctx, tx, user, "STUDIES")
}
func (s *Service) ownedStudy(ctx context.Context, tx pgx.Tx, user string, patientID, id int64) (study, error) {
	if err := s.studyOwner(ctx, tx, user, patientID); err != nil {
		return study{}, err
	}
	result, err := scanStudy(tx.QueryRow(ctx, "SELECT "+studyColumns+" FROM patient_medical_studies WHERE id=$1 AND patient_id=$2", id, patientID))
	if errors.Is(err, pgx.ErrNoRows) {
		return result, storage.Missing()
	}
	return result, err
}
func enqueueStudyDeletion(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "INSERT INTO study_file_deletions(storage_key,requested_at) VALUES($1,$2) ON CONFLICT(storage_key) DO UPDATE SET requested_at=EXCLUDED.requested_at", key, time.Now().UTC())
	return err
}
func (s *Service) DeletePatient(ctx context.Context, user string, id int64) error {
	err := s.transaction(ctx, false, func(tx pgx.Tx) error {
		// Keep the same patient-before-encounter ordering as encounter writers.
		if err := lockPatient(ctx, tx, user, id); err != nil {
			return err
		}
		patient, err := ownedPatient(ctx, tx, user, id)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT stored_file_name FROM patient_medical_studies WHERE patient_id=$1", id)
		if err != nil {
			return err
		}
		keys := []string{}
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				rows.Close()
				return err
			}
			keys = append(keys, key)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, key := range keys {
			if err := enqueueStudyDeletion(ctx, tx, key); err != nil {
				return err
			}
		}
		for _, query := range []string{
			"DELETE FROM prescription_medications WHERE record_id IN (SELECT id FROM patient_prescription_records WHERE patient_id=$1)",
			"DELETE FROM prescription_diagnostic_orders WHERE record_id IN (SELECT id FROM patient_prescription_records WHERE patient_id=$1)",
			"DELETE FROM patient_prescription_records WHERE patient_id=$1",
			"DELETE FROM patient_medical_studies WHERE patient_id=$1",
			"DELETE FROM patient_vaccinations WHERE patient_id=$1",
			"DELETE FROM clinical_encounters WHERE patient_id=$1",
		} {
			if _, err := tx.Exec(ctx, query, id); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, "DELETE FROM patients WHERE id=$1 AND version=$2", id, patient.Version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return api.Failure(409, "VERSION_CONFLICT", "La configuración cambió. Actualizá los datos antes de guardar.")
		}
		return nil
	})
	if err == nil {
		s.deletePendingFiles(ctx)
	}
	return err
}
func (s *Service) StudyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("DELETE /api/doctorchart/patients/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err == nil {
			err = s.DeletePatient(r.Context(), api.User(r.Context()).UserID, id)
		}
		clinicalReply(w, nil, err, 204)
	})
	mux.HandleFunc("GET /api/doctorchart/patients/{patientId}/studies", func(w http.ResponseWriter, r *http.Request) {
		if err := prescriptionRole(r); err != nil {
			api.WriteError(w, err)
			return
		}
		id, err := pathID(r, "patientId")
		result := []study{}
		if err == nil {
			err = s.transaction(r.Context(), true, func(tx pgx.Tx) error {
				if err := s.studyOwner(r.Context(), tx, api.User(r.Context()).UserID, id); err != nil {
					return err
				}
				rows, err := tx.Query(r.Context(), "SELECT "+studyColumns+" FROM patient_medical_studies WHERE patient_id=$1 ORDER BY uploaded_at DESC", id)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					item, err := scanStudy(rows)
					if err != nil {
						return err
					}
					result = append(result, item)
				}
				return rows.Err()
			})
		}
		clinicalReply(w, result, err, 200)
	})
	mux.HandleFunc("POST /api/doctorchart/patients/{patientId}/studies", func(w http.ResponseWriter, r *http.Request) {
		if err := prescriptionRole(r); err != nil {
			api.WriteError(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 175*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		id, err := pathID(r, "patientId")
		var result study
		if deadlineErr := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(180 * time.Second)); deadlineErr != nil && !errors.Is(deadlineErr, http.ErrNotSupported) {
			api.WriteError(w, api.Failure(503, "IMAGE_CONVERTER_UNAVAILABLE", "No se pudo iniciar la carga. Reintentá más tarde."))
			return
		}
		mediaType, _, parseErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err == nil && (parseErr != nil || mediaType != "multipart/form-data") {
			err = api.Failure(500, "INTERNAL_ERROR", "No se pudo completar la solicitud.")
		}
		var upload *storage.Upload
		if err == nil {
			upload, err = s.files.BeginUpload(ctx)
			if err == nil {
				defer upload.Close()
			}
		}
		if err == nil {
			r.Body = http.MaxBytesReader(w, r.Body, storage.MaxBytes+(64<<10))
			err = r.ParseMultipartForm(1 << 20)
			if r.MultipartForm != nil {
				defer r.MultipartForm.RemoveAll()
			}
			if err != nil {
				var maximum *http.MaxBytesError
				if errors.As(err, &maximum) {
					err = storage.TooLarge()
				} else {
					err = api.Failure(500, "INTERNAL_ERROR", "No se pudo completar la solicitud.")
				}
			}
		}
		var input multipart.File
		var header *multipart.FileHeader
		if err == nil {
			input, header, err = r.FormFile("file")
			if err != nil {
				err = api.Failure(500, "INTERNAL_ERROR", "No se pudo completar la solicitud.")
			} else {
				defer input.Close()
				if header.Size > storage.MaxBytes {
					err = storage.TooLarge()
				}
			}
		}
		var stored storage.File
		if err == nil {
			var description *string
			if values, ok := r.MultipartForm.Value["description"]; ok && len(values) > 0 {
				value := values[0]
				if api.JavaLength(value) > 4000 {
					err = api.Failure(400, "STUDY_DESCRIPTION_TOO_LONG", "La descripción admite hasta 4000 caracteres.")
				}
				if !api.JavaBlank(value) {
					value = api.JavaTrim(value)
					description = &value
				}
			}
			if err == nil {
				// Authorize before conversion, then release the database connection
				// while the request waits in the shared converter's queue.
				err = s.transaction(ctx, true, func(tx pgx.Tx) error {
					return s.studyOwner(ctx, tx, api.User(ctx).UserID, id)
				})
			}
			if err == nil {
				originalName := header.Filename
				if _, parameters, parseErr := mime.ParseMediaType(header.Header.Get("Content-Disposition")); parseErr == nil {
					if name, present := parameters["filename"]; present {
						originalName = name
					}
				}
				stored, err = upload.Save(ctx, originalName, input)
			}
			if err == nil {
				err = s.transaction(ctx, false, func(tx pgx.Tx) error {
					// Ownership and feature access may have changed during queueing.
					if err := s.studyOwner(ctx, tx, api.User(ctx).UserID, id); err != nil {
						return err
					}
					at := time.Now().UTC()
					err := tx.QueryRow(ctx, "INSERT INTO patient_medical_studies(patient_id,original_file_name,stored_file_name,content_type,file_size,description,uploaded_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id", id, stored.Name, stored.Key, stored.ContentType, stored.Size, description, at).Scan(&result.ID)
					result.PatientID = id
					result.Name = stored.Name
					result.ContentType = stored.ContentType
					result.Size = stored.Size
					result.Description = description
					result.Uploaded = at.Format(time.RFC3339Nano)
					result.key = stored.Key
					return err
				})
			}
			if err != nil && stored.Key != "" {
				cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				if deleteErr := s.files.Delete(cleanup, stored.Key); deleteErr != nil {
					slog.Warn("rolled-back study file awaits reconciliation")
				}
			}
		}
		if err == nil {
			w.Header().Set("Location", "/api/doctorchart/patients/"+strconv.FormatInt(id, 10)+"/studies/"+strconv.FormatInt(result.ID, 10))
		}
		clinicalReply(w, result, err, 201)
	})
	mux.HandleFunc("GET /api/doctorchart/patients/{patientId}/studies/{studyId}", func(w http.ResponseWriter, r *http.Request) {
		if err := prescriptionRole(r); err != nil {
			api.WriteError(w, err)
			return
		}
		result, err := s.readStudy(r)
		clinicalReply(w, result, err, 200)
	})
	mux.HandleFunc("GET /api/doctorchart/patients/{patientId}/studies/{studyId}/file", func(w http.ResponseWriter, r *http.Request) {
		if err := prescriptionRole(r); err != nil {
			api.WriteError(w, err)
			return
		}
		result, err := s.readStudy(r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		file, _, err := s.files.Open(r.Context(), result.key)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		defer file.Close()
		var seeker io.ReadSeeker
		if local, ok := file.(io.ReadSeeker); ok {
			seeker = local
		} else {
			temporary, err := os.CreateTemp("", "doctorchart-study-download-")
			if err != nil {
				api.WriteError(w, err)
				return
			}
			defer os.Remove(temporary.Name())
			defer temporary.Close()
			if _, err := io.Copy(temporary, file); err != nil {
				api.WriteError(w, err)
				return
			}
			if _, err := temporary.Seek(0, io.SeekStart); err != nil {
				api.WriteError(w, err)
				return
			}
			seeker = temporary
		}
		contentType, _, err := mime.ParseMediaType(result.ContentType)
		if err != nil {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Del("Pragma")
		w.Header().Del("Expires")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.Header().Set("Content-Disposition", studyDisposition(result.Name))
		http.ServeContent(w, r, "", time.Time{}, seeker)
	})
	mux.HandleFunc("DELETE /api/doctorchart/patients/{patientId}/studies/{studyId}", func(w http.ResponseWriter, r *http.Request) {
		if err := prescriptionRole(r); err != nil {
			api.WriteError(w, err)
			return
		}
		patientID, err := pathID(r, "patientId")
		id, idErr := pathID(r, "studyId")
		if err == nil {
			err = idErr
		}
		if err == nil {
			err = s.transaction(r.Context(), false, func(tx pgx.Tx) error {
				item, err := s.ownedStudy(r.Context(), tx, api.User(r.Context()).UserID, patientID, id)
				if err != nil {
					return err
				}
				if err := enqueueStudyDeletion(r.Context(), tx, item.key); err != nil {
					return err
				}
				_, err = tx.Exec(r.Context(), "DELETE FROM patient_medical_studies WHERE id=$1", id)
				return err
			})
		}
		if err == nil {
			s.deletePendingFiles(r.Context())
		}
		clinicalReply(w, nil, err, 204)
	})
}
func (s *Service) readStudy(r *http.Request) (study, error) {
	patientID, err := pathID(r, "patientId")
	id, idErr := pathID(r, "studyId")
	if err == nil {
		err = idErr
	}
	var result study
	if err == nil {
		err = s.transaction(r.Context(), true, func(tx pgx.Tx) error {
			var err error
			result, err = s.ownedStudy(r.Context(), tx, api.User(r.Context()).UserID, patientID, id)
			return err
		})
	}
	return result, err
}
func studyDisposition(name string) string {
	var encoded strings.Builder
	special := map[rune]string{'ä': "ae", 'ö': "oe", 'ü': "ue", 'Ä': "Ae", 'Ö': "Oe", 'Ü': "Ue", 'ß': "ss", 'æ': "ae", 'Æ': "AE", 'œ': "oe", 'Œ': "OE"}
	for _, value := range name {
		if replacement, ok := special[value]; ok {
			encoded.WriteString(replacement)
			continue
		}
		for _, decomposed := range norm.NFD.String(string(value)) {
			if decomposed < 128 {
				encoded.WriteRune(decomposed)
			} else if !unicode.Is(unicode.Mn, decomposed) && !unicode.Is(unicode.Mc, decomposed) && !unicode.Is(unicode.Me, decomposed) {
				encoded.WriteByte('_')
			}
		}
	}
	var extended strings.Builder
	for _, value := range []byte(name) {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || strings.ContainsRune("!#$&+-._^`|~", rune(value)) {
			extended.WriteByte(value)
		} else {
			extended.WriteByte('%')
			extended.WriteByte("0123456789ABCDEF"[value>>4])
			extended.WriteByte("0123456789ABCDEF"[value&15])
		}
	}
	return "inline; filename=\"" + strings.ReplaceAll(encoded.String(), "\"", "\\\"") + "\"; filename*=UTF-8''" + extended.String()
}
func (s *Service) deletePendingFiles(ctx context.Context) {
	cursor := ""
	for ctx.Err() == nil {
		rows, err := s.pool.Query(ctx, "SELECT storage_key FROM study_file_deletions WHERE storage_key>$1 ORDER BY storage_key LIMIT 100", cursor)
		if err != nil {
			slog.Warn("study deletion queue unavailable")
			return
		}
		keys := []string{}
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				rows.Close()
				slog.Warn("study deletion queue unavailable")
				return
			}
			keys = append(keys, key)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			slog.Warn("study deletion queue unavailable")
			return
		}
		if len(keys) == 0 {
			return
		}
		cursor = keys[len(keys)-1]
		for _, key := range keys {
			err := s.transaction(ctx, false, func(tx pgx.Tx) error {
				var locked string
				err := tx.QueryRow(ctx, "SELECT storage_key FROM study_file_deletions WHERE storage_key=$1 FOR UPDATE", key).Scan(&locked)
				if errors.Is(err, pgx.ErrNoRows) {
					return nil
				}
				if err != nil {
					return err
				}
				var referenced bool
				if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM patient_medical_studies WHERE stored_file_name=$1)", key).Scan(&referenced); err != nil {
					return err
				}
				if referenced {
					return nil
				}
				if err := s.files.Delete(ctx, key); err != nil {
					return err
				}
				_, err = tx.Exec(ctx, "DELETE FROM study_file_deletions WHERE storage_key=$1", key)
				return err
			})
			if err != nil {
				slog.Warn("study file deletion retained for retry")
			}
		}
	}
}

// Maintain runs only on this process's configured storage/database and exits with its context.
func (s *Service) Maintain(ctx context.Context) {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.deletePendingFiles(ctx)
		err := s.files.Candidates(ctx, time.Now().UTC().Add(-24*time.Hour), func(key string) error {
			var referenced bool
			if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM patient_medical_studies WHERE stored_file_name=$1)", key).Scan(&referenced); err != nil {
				return err
			}
			if referenced {
				return nil
			}
			if err := s.files.Delete(ctx, key); err != nil {
				slog.Warn("orphan study deletion retained for retry")
			}
			return nil
		})
		if err != nil && ctx.Err() == nil {
			slog.Warn("study reconciliation will retry")
		}
		timer.Reset(300 * time.Second)
	}
}
