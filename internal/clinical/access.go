package clinical

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

// Fixed labels keep routes, queries and request bodies out of the audit table.
var accessOperations = map[string]string{
	"GET /api/doctorchart/patients":                                                 "PATIENT_SEARCH",
	"GET /api/doctorchart/patients/{id}":                                            "PATIENT_READ",
	"GET /api/doctorchart/patients/archived":                                        "PATIENT_ARCHIVE_LIST",
	"GET /api/doctorchart/patients/{id}/clinical-summary":                           "CLINICAL_SUMMARY",
	"GET /api/doctorchart/patients/{id}/revisions":                                  "CLINICAL_REVISIONS",
	"GET /api/doctorchart/patients/{id}/revisions/calculations":                     "ARCHIVED_CALCULATIONS",
	"GET /api/doctorchart/patients/{id}/growth":                                     "GROWTH_READ",
	"GET /api/doctorchart/patients/{id}/measurements":                               "MEASUREMENTS_READ",
	"GET /api/doctorchart/patients/{id}/vaccinations":                               "VACCINATIONS_READ",
	"GET /api/doctorchart/encounters":                                               "ENCOUNTER_SEARCH",
	"GET /api/doctorchart/encounters/dashboard":                                     "ENCOUNTER_DASHBOARD",
	"GET /api/doctorchart/encounters/{id}":                                          "ENCOUNTER_READ",
	"GET /api/doctorchart/encounters/patient/{patientId}":                           "PATIENT_ENCOUNTERS",
	"GET /api/doctorchart/encounters/patient/{patientId}/specialty/{specialtyId}":   "PATIENT_ENCOUNTERS",
	"GET /api/doctorchart/patients/{patientId}/prescriptions":                       "PRESCRIPTIONS_READ",
	"GET /api/doctorchart/patients/{patientId}/prescriptions/{recordId}":            "PRESCRIPTION_READ",
	"GET /api/doctorchart/patients/{patientId}/prescriptions/{recordId}/print":      "PRESCRIPTION_PRINT_DATA",
	"GET /api/doctorchart/patients/{patientId}/prescriptions/{recordId}/pdf":        "PRESCRIPTION_PDF",
	"GET /api/doctorchart/patients/{patientId}/encounters/{recordId}/print":         "ENCOUNTER_PRINT_DATA",
	"GET /api/doctorchart/patients/{patientId}/encounters/{recordId}/pdf":           "ENCOUNTER_PDF",
	"GET /api/doctorchart/patients/{patientId}/studies":                             "STUDIES_READ",
	"GET /api/doctorchart/patients/{patientId}/studies/{studyId}":                   "STUDY_READ",
	"GET /api/doctorchart/patients/{patientId}/studies/{studyId}/file":              "STUDY_DOWNLOAD",
	"GET /api/doctorchart/patients/{patientId}/revisions/studies/{studyId}/file":    "ARCHIVED_STUDY_DOWNLOAD",
	"GET /api/doctorchart/calculators/patients/{patientId}/history":                 "CALCULATION_HISTORY",
	"GET /api/doctorchart/calculators/patients/{patientId}/autofill":                "CALCULATION_AUTOFILL",
	"POST /api/doctorchart/calculators/execute":                                     "CALCULATION_EXECUTE",
	"GET /api/doctorchart/workspace/clinical-access":                                "AUDIT_READ",
	"POST /api/doctorchart/patients/{patientId}/prescriptions/{recordId}/documents": "DOCUMENT_ISSUE",
	"POST /api/doctorchart/patients/{patientId}/encounters/{recordId}/documents":    "DOCUMENT_ISSUE",
	"GET /api/doctorchart/patients/{patientId}/documents":                           "DOCUMENT_LIST",
	"GET /api/doctorchart/patients/{patientId}/documents/{documentId}/pdf":          "DOCUMENT_REPRINT",
	"POST /api/doctorchart/patients/{patientId}/documents/{documentId}/annul":       "DOCUMENT_ANNUL",
	"POST /api/doctorchart/patients/{patientId}/export":                             "CHART_EXPORT_DOWNLOAD",
}

type accessKey struct{}

type accessWriter struct {
	http.ResponseWriter
	store       *Service
	ctx         context.Context
	operation   string
	requestID   string
	contextID   *int64
	patientID   *int64
	entityID    *int64
	version     *int64
	wroteHeader bool
	blocked     bool
	persisted   bool
}

func (s *Service) accessBoundary(w http.ResponseWriter, r *http.Request, operation string) *accessWriter {
	requestID := r.Header.Get("X-Request-ID")
	decoded, err := hex.DecodeString(requestID)
	if err != nil || len(decoded) != 16 || requestID != hex.EncodeToString(decoded) {
		var id [16]byte
		rand.Read(id[:])
		requestID = hex.EncodeToString(id[:])
	}
	a := &accessWriter{
		ResponseWriter: w, store: s, ctx: r.Context(), operation: operation, requestID: requestID,
		patientID: auditID(r, "patientId"),
	}
	for _, key := range []string{"documentId", "studyId", "recordId", "id"} {
		if value := auditID(r, key); value != nil {
			a.entityID = value
			break
		}
	}
	if a.patientID == nil && operation != "ENCOUNTER_READ" && operation != "AUDIT_READ" {
		a.patientID = a.entityID
	}
	return a
}

func auditID(r *http.Request, key string) *int64 {
	id, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil || id < 1 {
		return nil
	}
	return &id
}

func accessReference(ctx context.Context, entity, patient, version *int64) {
	audit, ok := ctx.Value(accessKey{}).(*accessWriter)
	if !ok {
		return
	}
	if entity != nil {
		audit.entityID = entity
	}
	if patient != nil {
		audit.patientID = patient
	}
	audit.version = version
}

func (a *accessWriter) WriteHeader(status int) {
	if a.wroteHeader {
		return
	}
	a.wroteHeader = true
	a.Header().Set("X-Clinical-Audit-ID", a.requestID)
	if !strings.Contains(a.Header().Get("Cache-Control"), "no-store") {
		a.Header().Set("Cache-Control", "private, no-store")
	}
	if a.persisted && status >= 200 && status < 400 {
		a.ResponseWriter.WriteHeader(status)
		return
	}
	outcome := "FAILED"
	switch {
	case status >= 200 && status < 400:
		outcome = "AUTHORIZED"
		if a.operation == "ENCOUNTER_PDF" || a.operation == "PRESCRIPTION_PDF" {
			outcome = "ARTIFACT_READY"
		}
	case status >= 400 && status < 500:
		outcome = "REJECTED"
	}
	// Bound the write independently of client disconnect. A successful
	// response must never reach the HTTP writer before this commit.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 3*time.Second)
	defer cancel()
	err := a.record(ctx, a.store.pool, a.operation, outcome, a.requestID)
	if err != nil {
		slog.Warn("clinical access audit unavailable", "operation", a.operation, "correlation", a.requestID)
		if status >= 200 && status < 400 {
			a.blocked = true
			for _, key := range []string{"Content-Type", "Content-Disposition", "Content-Length", "Content-Range", "Content-Encoding", "Content-Digest", "ETag", "Last-Modified", "Accept-Ranges", "Location"} {
				a.Header().Del(key)
			}
			api.WriteError(a.ResponseWriter, auditUnavailable())
			return
		}
	}
	a.ResponseWriter.WriteHeader(status)
}

type accessExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func auditUnavailable() error {
	return api.Failure(503, "CLINICAL_AUDIT_UNAVAILABLE", "La auditoría clínica no está disponible. Reintentá en unos momentos.")
}

func commitAccessWith(ctx context.Context, tx pgx.Tx, outcome string) error {
	audit, ok := ctx.Value(accessKey{}).(*accessWriter)
	if !ok {
		return auditUnavailable()
	}
	if err := audit.record(ctx, tx, audit.operation, outcome, audit.requestID); err != nil {
		slog.Warn("clinical access audit unavailable", "operation", audit.operation, "correlation", audit.requestID)
		return auditUnavailable()
	}
	audit.persisted = true
	return nil
}

func (a *accessWriter) record(ctx context.Context, executor accessExecutor, operation, outcome, requestID string) error {
	user := api.User(ctx)
	kind := "DOCTOR"
	if user.HasRole("SUPER_ADMIN") {
		kind = "CATALOG_ADMIN"
	}
	var owner *string
	if a.contextID != nil {
		owner = &user.UserID
	}
	_, err := executor.Exec(ctx, `INSERT INTO clinical_access_events
		(actor_user_id,actor_kind,operation,entity_id,patient_id,record_version,owner_user_id,specialty_context_id,outcome,request_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, user.UserID, kind, operation, a.entityID, a.patientID,
		a.version, owner, a.contextID, outcome, requestID)
	return err
}

func (a *accessWriter) Write(p []byte) (int, error) {
	if !a.wroteHeader {
		a.WriteHeader(http.StatusOK)
	}
	if a.blocked {
		return 0, errors.New("clinical response withheld")
	}
	return a.ResponseWriter.Write(p)
}

type accessEvent struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"occurredAt"`
	Actor     string    `json:"actor"`
	ActorKind string    `json:"actorKind"`
	Operation string    `json:"operation"`
	Entity    *int64    `json:"entityId"`
	Patient   *int64    `json:"patientId"`
	Version   *int64    `json:"recordVersion"`
	Outcome   string    `json:"outcome"`
	RequestID string    `json:"requestId"`
}

func (s *Service) readClinicalAccess(w http.ResponseWriter, r *http.Request) {
	var before, patient int64
	for key, dest := range map[string]*int64{"before": &before, "patientId": &patient} {
		if value := r.URL.Query().Get(key); value != "" {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				api.WriteError(w, api.Invalid("El filtro de auditoría no es válido."))
				return
			}
			*dest = id
		}
	}
	items := []accessEvent{}
	var next *int64
	ctx := r.Context()
	owner := api.User(ctx).UserID
	// Keep the owner/context predicates even with RLS, including in fixtures.
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,occurred_at,actor_user_id,actor_kind,operation,entity_id,patient_id,record_version,outcome,request_id
			FROM clinical_access_events WHERE owner_user_id=$1 AND specialty_context_id=$2
			AND ($3::bigint=0 OR id<$3) AND ($4::bigint=0 OR patient_id=$4)
			ORDER BY id DESC LIMIT 26`, owner, api.SpecialtyContextID(ctx), before, patient)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item accessEvent
			if err := rows.Scan(&item.ID, &item.At, &item.Actor, &item.ActorKind, &item.Operation, &item.Entity, &item.Patient, &item.Version, &item.Outcome, &item.RequestID); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if len(items) > 25 {
		items = items[:25]
		next = &items[24].ID
	}
	clinicalReply(w, map[string]any{"items": items, "nextBefore": next}, err, http.StatusOK)
}
