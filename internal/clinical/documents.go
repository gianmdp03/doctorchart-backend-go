package clinical

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type documentIssue struct {
	ID            int64      `json:"id"`
	Code          string     `json:"code"`
	Source        string     `json:"source"`
	SourceID      int64      `json:"sourceId"`
	SourceVersion int64      `json:"sourceVersion"`
	Type          string     `json:"type"`
	Specialty     string     `json:"specialty"`
	Issuer        string     `json:"issuer"`
	IssuedAt      time.Time  `json:"issuedAt"`
	SHA256        string     `json:"sha256"`
	Status        string     `json:"status"`
	AnnulledAt    *time.Time `json:"annulledAt"`
	Reason        *string    `json:"reason"`
	SourceStatus  string     `json:"sourceStatus"`
}

func (s *Service) issueDocument(ctx context.Context, patientID, sourceID int64, encounter bool, selection string) (documentIssue, error) {
	var item documentIssue
	owner := api.User(ctx).UserID
	err := s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := lockPatient(ctx, tx, owner, patientID); err != nil {
			return err
		}
		var doc printDocument
		if err := s.readPrintable(ctx, tx, owner, patientID, sourceID, encounter, &doc); err != nil {
			return err
		}
		if selection == "" {
			selection = doc.Type
		}
		if selection == "STUDY_ORDERS" && len(doc.Orders) == 0 ||
			selection == "PRESCRIPTION" && (string(doc.Medications) == "[]" || string(doc.Medications) == "null") {
			return api.Failure(409, "DOCUMENT_TYPE_UNAVAILABLE", "El registro no contiene datos para ese tipo de documento.")
		}
		var code [16]byte
		rand.Read(code[:])
		doc.Code = "DC-" + hex.EncodeToString(code[:])
		doc.emitted = true
		doc.at = time.Now().UTC()
		doc.Issued = api.FormatInstant(doc.at)
		birth, err := time.Parse("2006-01-02", doc.Patient.BirthDate)
		if err != nil {
			return err
		}
		issuedLocal := doc.at.In(s.documentZone(doc.IssuanceZone))
		doc.Patient.Age = printAge(birth, time.Date(issuedLocal.Year(), issuedLocal.Month(), issuedLocal.Day(), 0, 0, 0, 0, time.UTC))
		pdf, err := s.printPDF(doc, url.Values{"type": {selection}})
		if err != nil {
			return err
		}
		if len(pdf) > 4<<20 {
			return api.Failure(413, "DOCUMENT_TOO_LARGE", "El PDF supera el tamaño de emisión admitido.")
		}
		specialty := ""
		if doc.Author.Specialty != nil {
			specialty = *doc.Author.Specialty
		}
		item = documentIssue{Code: doc.Code, Source: doc.Source, SourceID: sourceID, SourceVersion: doc.version,
			Type: selection, Specialty: specialty, Issuer: owner, IssuedAt: doc.at,
			SHA256: fmt.Sprintf("%x", sha256.Sum256(pdf)), Status: "ISSUED", SourceStatus: "CURRENT"}
		err = tx.QueryRow(ctx, `INSERT INTO clinical_document_issues
			(code,owner_user_id,specialty_context_id,patient_id,source,source_id,source_version,document_type,issuer_specialty,issued_at,pdf,sha256)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id,issued_at`,
			item.Code, owner, api.SpecialtyContextID(ctx), patientID, item.Source, sourceID, doc.version, item.Type, specialty, doc.at, pdf, item.SHA256).
			Scan(&item.ID, &item.IssuedAt)
		if err != nil {
			return err
		}
		accessReference(ctx, &item.ID, &patientID, &doc.version)
		return commitAccessWith(ctx, tx, "ARTIFACT_READY")
	})
	return item, err
}

func (s *Service) documentList(ctx context.Context, patient, before int64) ([]documentIssue, *int64, error) {
	items := []documentIssue{}
	owner, scope := api.User(ctx).UserID, api.SpecialtyContextID(ctx)
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		if _, err := readArchivedPatient(ctx, tx, owner, patient); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT d.id,d.code,d.source,d.source_id,d.source_version,d.document_type,d.issuer_specialty,d.owner_user_id,
			d.issued_at,d.sha256,a.annulled_at,a.reason,
			CASE WHEN COALESCE(e.annulled_at,r.annulled_at) IS NOT NULL THEN 'ANNULLED'
			     WHEN COALESCE(e.version,r.version) IS DISTINCT FROM d.source_version THEN 'CHANGED' ELSE 'CURRENT' END
			FROM clinical_document_issues d LEFT JOIN clinical_document_annulments a ON a.document_id=d.id
			LEFT JOIN clinical_encounters e ON d.source='ENCOUNTER' AND e.id=d.source_id AND e.patient_id=d.patient_id
			LEFT JOIN patient_prescription_records r ON d.source='PRESCRIPTION_RECORD' AND r.id=d.source_id AND r.patient_id=d.patient_id
			WHERE d.patient_id=$1 AND d.owner_user_id=$2 AND d.specialty_context_id=$3 AND ($4::bigint=0 OR d.id<$4)
			ORDER BY d.id DESC LIMIT 26`, patient, owner, scope, before)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item documentIssue
			err = rows.Scan(&item.ID, &item.Code, &item.Source, &item.SourceID, &item.SourceVersion, &item.Type, &item.Specialty, &item.Issuer,
				&item.IssuedAt, &item.SHA256, &item.AnnulledAt, &item.Reason, &item.SourceStatus)
			if err != nil {
				return err
			}
			item.Status = "ISSUED"
			if item.AnnulledAt != nil {
				item.Status = "ANNULLED"
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	var next *int64
	if len(items) > 25 {
		items = items[:25]
		next = &items[24].ID
	}
	return items, next, err
}

func (s *Service) documentRoutes(mux *http.ServeMux) {
	for _, source := range []string{"prescriptions", "encounters"} {
		s.handle(mux, "POST /api/doctorchart/patients/{patientId}/"+source+"/{recordId}/documents", func(w http.ResponseWriter, r *http.Request) {
			patient, err := pathID(r, "patientId")
			record, recordErr := pathID(r, "recordId")
			if err == nil {
				err = recordErr
			}
			if err == nil && source == "prescriptions" {
				err = prescriptionRole(r)
			}
			var item documentIssue
			if err == nil {
				var body map[string]any
				body, err = api.Decode(w, r)
				if err == nil {
					err = api.Only(body, "type")
				}
				selection, ok := body["type"].(string)
				if value, present := body["type"]; err == nil && present && (!ok || value == nil || selection != "PRESCRIPTION" && selection != "STUDY_ORDERS") {
					err = api.Invalid("El tipo de documento no es válido.")
				}
				if err == nil {
					item, err = s.issueDocument(r.Context(), patient, record, source == "encounters", selection)
				}
			}
			clinicalReply(w, item, err, http.StatusCreated)
		})
	}
	const route = "/api/doctorchart/patients/{patientId}/documents"
	s.handle(mux, "GET "+route, func(w http.ResponseWriter, r *http.Request) {
		patient, err := pathID(r, "patientId")
		var before int64
		if raw := r.URL.Query().Get("before"); err == nil && raw != "" {
			before, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || before < 1 {
				err = api.Invalid("El cursor de documentos es inválido.")
			}
		}
		if err != nil {
			api.WriteError(w, err)
			return
		}
		items, next, err := s.documentList(r.Context(), patient, before)
		clinicalReply(w, map[string]any{"items": items, "nextBefore": next}, err, 200)
	})
	s.handle(mux, "GET "+route+"/{documentId}/pdf", s.historicalPDF)
	s.handle(mux, "POST "+route+"/{documentId}/annul", s.annulDocument)
}

func (s *Service) historicalPDF(w http.ResponseWriter, r *http.Request) {
	patient, err := pathID(r, "patientId")
	id, idErr := pathID(r, "documentId")
	if err == nil {
		err = idErr
	}
	var pdf []byte
	var code, fingerprint string
	var version int64
	ctx := r.Context()
	if err == nil {
		err = s.transaction(ctx, true, func(tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `SELECT code,pdf,sha256,source_version FROM clinical_document_issues
				WHERE id=$1 AND patient_id=$2 AND owner_user_id=$3 AND specialty_context_id=$4`,
				id, patient, api.User(ctx).UserID, api.SpecialtyContextID(ctx)).Scan(&code, &pdf, &fingerprint, &version)
			if errors.Is(err, pgx.ErrNoRows) {
				return api.Failure(404, "DOCUMENT_NOT_FOUND", "No se encontró la emisión clínica.")
			}
			return err
		})
	}
	if err != nil {
		api.WriteError(w, err)
		return
	}
	if fmt.Sprintf("%x", sha256.Sum256(pdf)) != fingerprint {
		api.WriteError(w, api.Failure(409, "DOCUMENT_INTEGRITY_FAILED", "La emisión guardada no superó la verificación de integridad."))
		return
	}
	accessReference(ctx, &id, &patient, &version)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "inline; filename=\""+code+".pdf\"")
	http.ServeContent(w, r, code+".pdf", time.Time{}, bytes.NewReader(pdf))
}

func (s *Service) annulDocument(w http.ResponseWriter, r *http.Request) {
	patient, err := pathID(r, "patientId")
	id, idErr := pathID(r, "documentId")
	if err == nil {
		err = idErr
	}
	ctx := r.Context()
	var reason string
	if err == nil {
		body, decodeErr := api.Decode(w, r)
		err = decodeErr
		if err == nil {
			err = api.Only(body, "reason")
		}
		if err == nil {
			reason, err = api.Text(body, "reason", 500)
		}
	}
	if err == nil {
		err = s.transaction(ctx, false, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `INSERT INTO clinical_document_annulments(document_id,owner_user_id,specialty_context_id,reason)
				SELECT id,owner_user_id,specialty_context_id,$5 FROM clinical_document_issues
				WHERE id=$1 AND patient_id=$2 AND owner_user_id=$3 AND specialty_context_id=$4
				ON CONFLICT DO NOTHING`, id, patient, api.User(ctx).UserID, api.SpecialtyContextID(ctx), strings.TrimSpace(reason))
			if err == nil && tag.RowsAffected() != 1 {
				return api.Failure(404, "DOCUMENT_NOT_ANNULABLE", "La emisión no está disponible para anular.")
			}
			if err == nil {
				accessReference(ctx, &id, &patient, nil)
				return commitAccessWith(ctx, tx, "AUTHORIZED")
			}
			return err
		})
	}
	clinicalReply(w, nil, err, 204)
}
