package clinical

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

const maxWhatsAppContentLength = 20000

// WhatsAppConsultation represents a manual clinical record of a WhatsApp interaction.
type WhatsAppConsultation struct {
	ID          int64  `json:"id"`
	PatientID   int64  `json:"patientId"`
	ContextID   int64  `json:"specialtyContextId"`
	Author      string `json:"authorUserId"`
	ConsultedAt string `json:"consultedAt"`
	Content     string `json:"content"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	Version     int64  `json:"version"`
}

const whatsAppConsultationColumns = "id,patient_id,specialty_context_id,author_user_id,consulted_at,content,created_at,updated_at,version"

func scanWhatsAppConsultation(row pgx.Row) (WhatsAppConsultation, error) {
	var item WhatsAppConsultation
	var consultedAt, createdAt, updatedAt time.Time
	err := row.Scan(
		&item.ID,
		&item.PatientID,
		&item.ContextID,
		&item.Author,
		&consultedAt,
		&item.Content,
		&createdAt,
		&updatedAt,
		&item.Version,
	)
	if err != nil {
		return item, err
	}
	item.ConsultedAt = api.FormatInstant(consultedAt)
	item.CreatedAt = api.FormatInstant(createdAt)
	item.UpdatedAt = api.FormatInstant(updatedAt)
	return item, nil
}

func parseWhatsAppConsultedAt(request map[string]any) (time.Time, error) {
	value, err := api.Text(request, "consultedAt", 64)
	if err != nil {
		return time.Time{}, api.Failure(400, "INVALID_CONSULTED_AT", "Indicá una fecha y hora válidas para la consulta.")
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, api.Failure(400, "INVALID_CONSULTED_AT", "Indicá una fecha y hora válidas para la consulta.")
	}
	utc := parsed.UTC().Truncate(time.Microsecond)
	if utc.Year() < 1900 || utc.After(time.Now().UTC().Add(2*time.Minute)) {
		return time.Time{}, api.Failure(400, "INVALID_CONSULTED_AT", "La fecha y hora de la consulta no pueden ser futuras ni anteriores a 1900.")
	}
	return utc, nil
}

func parseWhatsAppContent(request map[string]any) (string, error) {
	raw, ok := request["content"].(string)
	if !ok || !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
		return "", api.Failure(400, "CONTENT_REQUIRED", "Ingresá la descripción de la consulta.")
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	trimmed := strings.TrimSpace(normalized)
	if api.JavaBlank(trimmed) {
		return "", api.Failure(400, "CONTENT_REQUIRED", "Ingresá la descripción de la consulta.")
	}
	if api.JavaLength(trimmed) > maxWhatsAppContentLength {
		return "", api.Failure(400, "CONTENT_TOO_LONG", "La descripción admite hasta 20000 caracteres.")
	}
	return trimmed, nil
}

func ownedWhatsAppConsultation(ctx context.Context, tx pgx.Tx, userID string, patientID, id int64) (WhatsAppConsultation, error) {
	item, err := scanWhatsAppConsultation(tx.QueryRow(ctx,
		`SELECT `+whatsAppConsultationColumns+`
		FROM patient_whatsapp_consultations
		WHERE id=$1 AND patient_id=$2 AND author_user_id=$3 AND specialty_context_id=$4 AND annulled_at IS NULL`,
		id, patientID, userID, api.SpecialtyContextID(ctx)))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, api.Failure(404, "WHATSAPP_CONSULTATION_NOT_FOUND", "No se encontró la consulta por WhatsApp.")
	}
	return item, err
}

func (s *Service) ListWhatsAppConsultations(ctx context.Context, userID string, patientID int64) ([]WhatsAppConsultation, error) {
	result := []WhatsAppConsultation{}
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		if _, err := ownedPatient(ctx, tx, userID, patientID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx,
			`SELECT `+whatsAppConsultationColumns+`
			FROM patient_whatsapp_consultations
			WHERE patient_id=$1 AND author_user_id=$2 AND specialty_context_id=$3 AND annulled_at IS NULL
			ORDER BY consulted_at DESC, id DESC`,
			patientID, userID, api.SpecialtyContextID(ctx))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanWhatsAppConsultation(rows)
			if err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, err
}

func (s *Service) GetWhatsAppConsultation(ctx context.Context, userID string, patientID, id int64) (WhatsAppConsultation, error) {
	var result WhatsAppConsultation
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		if _, err := ownedPatient(ctx, tx, userID, patientID); err != nil {
			return err
		}
		var err error
		result, err = ownedWhatsAppConsultation(ctx, tx, userID, patientID, id)
		return err
	})
	return result, err
}

func (s *Service) CreateWhatsAppConsultation(ctx context.Context, userID string, patientID int64, body map[string]any) (WhatsAppConsultation, error) {
	var result WhatsAppConsultation
	if err := api.Only(body, "consultedAt", "content"); err != nil {
		return result, err
	}
	consultedAt, err := parseWhatsAppConsultedAt(body)
	if err != nil {
		return result, err
	}
	content, err := parseWhatsAppContent(body)
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := lockPatient(ctx, tx, userID, patientID); err != nil {
			return err
		}
		var workspaceID, contextID int64
		if err := tx.QueryRow(ctx,
			`SELECT workspace_id, specialty_context_id
			FROM patients
			WHERE id=$1 AND user_id=$2 AND specialty_context_id=$3 AND annulled_at IS NULL`,
			patientID, userID, api.SpecialtyContextID(ctx)).Scan(&workspaceID, &contextID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return api.Failure(404, "PATIENT_NOT_FOUND", "No se encontró el paciente.")
			}
			return err
		}
		var err error
		result, err = scanWhatsAppConsultation(tx.QueryRow(ctx,
			`INSERT INTO patient_whatsapp_consultations
			(patient_id, workspace_id, specialty_context_id, author_user_id, consulted_at, content, created_at, updated_at, version)
			VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp(), clock_timestamp(), 0)
			RETURNING `+whatsAppConsultationColumns,
			patientID, workspaceID, contextID, userID, consultedAt, content))
		return err
	})
	return result, err
}

func (s *Service) UpdateWhatsAppConsultation(ctx context.Context, userID string, patientID, id int64, body map[string]any) (WhatsAppConsultation, error) {
	var result WhatsAppConsultation
	if err := api.Only(body, "expectedVersion", "consultedAt", "content", "reason"); err != nil {
		return result, err
	}
	expected, err := api.Version(body, false)
	if err != nil {
		return result, err
	}
	consultedAt, err := parseWhatsAppConsultedAt(body)
	if err != nil {
		return result, err
	}
	content, err := parseWhatsAppContent(body)
	if err != nil {
		return result, err
	}
	reason, err := revisionReason(body["reason"], false)
	if err != nil {
		return result, err
	}
	err = s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := lockPatient(ctx, tx, userID, patientID); err != nil {
			return err
		}
		current, err := ownedWhatsAppConsultation(ctx, tx, userID, patientID, id)
		if err != nil {
			return err
		}
		if current.Version != *expected {
			return versionConflict()
		}
		if err := bindRevision(ctx, tx, userID, reason); err != nil {
			return err
		}
		result, err = scanWhatsAppConsultation(tx.QueryRow(ctx,
			`UPDATE patient_whatsapp_consultations
			SET consulted_at=$3,
				content=$4,
				updated_at=greatest(clock_timestamp(), updated_at + interval '1 microsecond'),
				version=version+1
			WHERE id=$1 AND patient_id=$2 AND version=$5 AND annulled_at IS NULL
			RETURNING `+whatsAppConsultationColumns,
			id, patientID, consultedAt, content, *expected))
		if errors.Is(err, pgx.ErrNoRows) {
			return versionConflict()
		}
		return err
	})
	return result, err
}

func (s *Service) AnnulWhatsAppConsultation(ctx context.Context, userID string, patientID, id int64, request annulment) error {
	return s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := lockPatient(ctx, tx, userID, patientID); err != nil {
			return err
		}
		if _, err := ownedWhatsAppConsultation(ctx, tx, userID, patientID, id); err != nil {
			return err
		}
		if err := bindRevision(ctx, tx, userID, request.reason); err != nil {
			return err
		}
		return annulOne(ctx, tx, "patient_whatsapp_consultations", id, request.expected)
	})
}

func (s *Service) WhatsAppConsultationRoutes(mux *http.ServeMux) {
	s.handle(mux, "GET /api/doctorchart/patients/{patientId}/whatsapp-consultations", func(w http.ResponseWriter, r *http.Request) {
		if err := prescriptionRole(r); err != nil {
			api.WriteError(w, err)
			return
		}
		patientID, err := pathID(r, "patientId")
		if err != nil {
			api.WriteError(w, err)
			return
		}
		items, err := s.ListWhatsAppConsultations(r.Context(), api.User(r.Context()).UserID, patientID)
		clinicalReply(w, items, err, http.StatusOK)
	})

	s.handle(mux, "POST /api/doctorchart/patients/{patientId}/whatsapp-consultations", func(w http.ResponseWriter, r *http.Request) {
		if err := prescriptionRole(r); err != nil {
			api.WriteError(w, err)
			return
		}
		patientID, err := pathID(r, "patientId")
		if err != nil {
			api.WriteError(w, err)
			return
		}
		body, err := api.Decode(w, r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		item, err := s.CreateWhatsAppConsultation(r.Context(), api.User(r.Context()).UserID, patientID, body)
		if err == nil {
			accessReference(r.Context(), &item.ID, &item.PatientID, &item.Version)
			w.Header().Set("Location", "/api/doctorchart/patients/"+strconv.FormatInt(patientID, 10)+"/whatsapp-consultations/"+strconv.FormatInt(item.ID, 10))
		}
		clinicalReply(w, item, err, http.StatusCreated)
	})

	s.handle(mux, "GET /api/doctorchart/patients/{patientId}/whatsapp-consultations/{consultationId}", func(w http.ResponseWriter, r *http.Request) {
		if err := prescriptionRole(r); err != nil {
			api.WriteError(w, err)
			return
		}
		patientID, err := pathID(r, "patientId")
		if err != nil {
			api.WriteError(w, err)
			return
		}
		id, err := pathID(r, "consultationId")
		if err != nil {
			api.WriteError(w, err)
			return
		}
		item, err := s.GetWhatsAppConsultation(r.Context(), api.User(r.Context()).UserID, patientID, id)
		if err == nil {
			accessReference(r.Context(), &item.ID, &item.PatientID, &item.Version)
		}
		clinicalReply(w, item, err, http.StatusOK)
	})

	s.handle(mux, "PUT /api/doctorchart/patients/{patientId}/whatsapp-consultations/{consultationId}", func(w http.ResponseWriter, r *http.Request) {
		if err := prescriptionRole(r); err != nil {
			api.WriteError(w, err)
			return
		}
		patientID, err := pathID(r, "patientId")
		if err != nil {
			api.WriteError(w, err)
			return
		}
		id, err := pathID(r, "consultationId")
		if err != nil {
			api.WriteError(w, err)
			return
		}
		body, err := api.Decode(w, r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		item, err := s.UpdateWhatsAppConsultation(r.Context(), api.User(r.Context()).UserID, patientID, id, body)
		if err == nil {
			accessReference(r.Context(), &item.ID, &item.PatientID, &item.Version)
		}
		clinicalReply(w, item, err, http.StatusOK)
	})

	s.handle(mux, "DELETE /api/doctorchart/patients/{patientId}/whatsapp-consultations/{consultationId}", func(w http.ResponseWriter, r *http.Request) {
		if err := prescriptionRole(r); err != nil {
			api.WriteError(w, err)
			return
		}
		patientID, err := pathID(r, "patientId")
		if err != nil {
			api.WriteError(w, err)
			return
		}
		id, err := pathID(r, "consultationId")
		if err != nil {
			api.WriteError(w, err)
			return
		}
		request, err := readAnnulment(w, r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		err = s.AnnulWhatsAppConsultation(r.Context(), api.User(r.Context()).UserID, patientID, id, request)
		if err == nil {
			accessReference(r.Context(), &id, &patientID, nil)
		}
		clinicalReply(w, nil, err, http.StatusNoContent)
	})
}
