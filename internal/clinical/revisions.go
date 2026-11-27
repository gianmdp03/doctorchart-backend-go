package clinical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

func revisionReason(value any, required bool) (string, error) {
	if value == nil && !required {
		return "", nil
	}
	reason, ok := value.(string)
	reason = strings.TrimSpace(reason)
	if !ok || reason == "" || api.JavaLength(reason) > 1000 || strings.ContainsRune(reason, 0) {
		return "", api.Invalid("Ingresá un motivo de hasta 1000 caracteres.")
	}
	return reason, nil
}

func bindRevision(ctx context.Context, tx pgx.Tx, actor, reason string) error {
	_, err := tx.Exec(ctx, `SELECT set_config('doctorchart.revision_actor', $1, true),
		set_config('doctorchart.revision_reason', $2, true)`, actor, reason)
	return err
}

type annulment struct {
	reason   string
	expected *int64
}

func readAnnulment(w http.ResponseWriter, r *http.Request) (annulment, error) {
	var request annulment
	body, err := api.Decode(w, r)
	if err != nil {
		return request, err
	}
	if err := api.Only(body, "reason", "expectedVersion"); err != nil {
		return request, err
	}
	request.reason, err = revisionReason(body["reason"], true)
	if err == nil {
		if _, present := body["expectedVersion"]; present {
			request.expected, err = api.Version(body, false)
		}
	}
	return request, err
}

func versionConflict() error {
	return api.Failure(409, "VERSION_CONFLICT", "El registro cambió. Actualizá los datos antes de guardar.")
}

// The table and predicate come only from these internal call sites. The
// patient lock is held until the version change and its revision commit.
func annulRows(ctx context.Context, tx pgx.Tx, table, predicate string, id int64, expected *int64) (int64, error) {
	const clause = ` SET annulled_at=clock_timestamp(),version=version+1
		WHERE annulled_at IS NULL AND ($2::bigint IS NULL OR version=$2) AND `
	tag, err := tx.Exec(ctx, "UPDATE "+table+clause+predicate, id, expected)
	return tag.RowsAffected(), err
}

func annulOne(ctx context.Context, tx pgx.Tx, table string, id int64, expected *int64) error {
	count, err := annulRows(ctx, tx, table, "id=$1", id, expected)
	if err == nil && count != 1 {
		return versionConflict()
	}
	return err
}

func (s *Service) annulPatient(ctx context.Context, user string, id int64, request annulment) error {
	return s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := lockPatient(ctx, tx, user, id); err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, user, "EDIT_PATIENT"); err != nil {
			return err
		}
		if err := bindRevision(ctx, tx, user, request.reason); err != nil {
			return err
		}
		// Preserve child rows and their foreign keys. Keeping the study row
		// also keeps its physical file referenced by both media backends.
		for _, table := range []string{"clinical_encounters", "patient_prescription_records", "patient_medical_studies", "patient_vaccinations", "patient_whatsapp_consultations"} {
			if _, err := annulRows(ctx, tx, table, "patient_id=$1", id, nil); err != nil {
				return err
			}
		}
		return annulOne(ctx, tx, "patients", id, request.expected)
	})
}

type revisionItem struct {
	ID              int64           `json:"id"`
	Entity          string          `json:"entity"`
	EntityID        int64           `json:"entityId"`
	Actor           string          `json:"actorUserId"`
	At              string          `json:"occurredAt"`
	Operation       string          `json:"operation"`
	PreviousVersion int64           `json:"previousVersion"`
	NewVersion      int64           `json:"newVersion"`
	Reason          *string         `json:"reason"`
	Previous        json.RawMessage `json:"previous"`
	Derived         map[string]any  `json:"derivedProjections,omitempty"`
	FirstRecorded   bool            `json:"firstRecorded"`
}

type archivedPatient struct {
	ID         int64      `json:"id"`
	Name       string     `json:"fullName"`
	AnnulledAt *time.Time `json:"annulledAt"`
}

type revisionPage[T any] struct {
	Items      []T    `json:"items"`
	NextBefore *int64 `json:"nextBefore"`
}

const revisionPageSize = 25

func revisionCursor(r *http.Request) (int64, error) {
	values := r.URL.Query()["before"]
	if len(values) == 0 {
		return 0, nil
	}
	before, err := strconv.ParseInt(values[0], 10, 64)
	if len(values) != 1 || err != nil || before <= 0 {
		return 0, api.Invalid("La página de historial no es válida.")
	}
	return before, nil
}

func revisionEncounter(r *http.Request) (int64, error) {
	values := r.URL.Query()["encounterId"]
	if len(values) == 0 {
		return 0, nil
	}
	id, err := strconv.ParseInt(values[0], 10, 64)
	if len(values) != 1 || err != nil || id <= 0 {
		return 0, api.Invalid("La consulta del historial no es válida.")
	}
	return id, nil
}

func readArchivedPatient(ctx context.Context, tx pgx.Tx, user string, id int64) (archivedPatient, error) {
	var patient archivedPatient
	err := tx.QueryRow(ctx, `SELECT id,full_name,annulled_at FROM patients
		WHERE id=$1 AND user_id=$2 AND specialty_context_id=$3`, id, user, api.SpecialtyContextID(ctx)).Scan(&patient.ID, &patient.Name, &patient.AnnulledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = api.Failure(404, "PATIENT_NOT_FOUND", "No se encontró el paciente.")
	}
	return patient, err
}

func (s *Service) revisionRoutes(mux *http.ServeMux) {
	s.handle(mux, "GET /api/doctorchart/patients/{patientId}/revisions/studies/{studyId}/file", s.archivedStudyFile)
	s.handle(mux, "GET /api/doctorchart/patients/{id}/revisions/calculations", s.archivedCalculations)
	s.handle(mux, "GET /api/doctorchart/patients/archived", func(w http.ResponseWriter, r *http.Request) {
		before, err := revisionCursor(r)
		page := revisionPage[archivedPatient]{Items: []archivedPatient{}}
		if err == nil {
			ctx := r.Context()
			err = s.transaction(ctx, true, func(tx pgx.Tx) error {
				rows, err := tx.Query(ctx, `SELECT id,full_name,annulled_at FROM patients
					WHERE user_id=$1 AND specialty_context_id=$2 AND annulled_at IS NOT NULL
					  AND ($3::bigint=0 OR id<$3) ORDER BY id DESC LIMIT $4`,
					api.User(ctx).UserID, api.SpecialtyContextID(ctx), before, revisionPageSize+1)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					var patient archivedPatient
					if err := rows.Scan(&patient.ID, &patient.Name, &patient.AnnulledAt); err != nil {
						return err
					}
					page.Items = append(page.Items, patient)
				}
				if len(page.Items) > revisionPageSize {
					page.Items = page.Items[:revisionPageSize]
					page.NextBefore = &page.Items[revisionPageSize-1].ID
				}
				return rows.Err()
			})
		}
		clinicalReply(w, page, err, 200)
	})
	s.handle(mux, "GET /api/doctorchart/patients/{id}/revisions", func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r, "id")
		if err != nil {
			api.WriteError(w, err)
			return
		}
		before, err := revisionCursor(r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		encounterID, err := revisionEncounter(r)
		if err != nil {
			api.WriteError(w, err)
			return
		}
		ctx := r.Context()
		user := api.User(ctx).UserID
		result := struct {
			Patient             archivedPatient `json:"patient"`
			CalculationsEnabled bool            `json:"calculationsEnabled"`
			revisionPage[revisionItem]
		}{revisionPage: revisionPage[revisionItem]{Items: []revisionItem{}}}
		err = s.transaction(ctx, true, func(tx pgx.Tx) error {
			var err error
			result.Patient, err = readArchivedPatient(ctx, tx, user, id)
			if err != nil {
				return err
			}
			result.CalculationsEnabled = s.workspaces.Feature(ctx, tx, user, "CALCULATORS") == nil
			entities := []string{"PATIENT", "ENCOUNTER", "WHATSAPP_CONSULTATION"}
			for _, item := range []struct{ entity, feature string }{
				{"STUDY", "STUDIES"}, {"PRESCRIPTION", "PRESCRIPTIONS"}, {"VACCINATION", "PATIENT_VACCINATION"},
			} {
				if item.entity != "VACCINATION" && prescriptionRole(r) != nil {
					continue
				}
				if err := s.workspaces.Feature(ctx, tx, user, item.feature); err != nil {
					var failure *api.Error
					if errors.As(err, &failure) && failure.Status == http.StatusForbidden {
						continue
					}
					return err
				}
				entities = append(entities, item.entity)
			}
			rows, err := tx.Query(ctx, `SELECT id,entity,entity_id,actor_user_id,occurred_at,operation,
				previous_version,new_version,reason,previous_content - ARRAY['stored_file_name','media_asset_id','media_identity'],
				NOT EXISTS (SELECT 1 FROM clinical_revisions earlier WHERE earlier.entity=r.entity
					AND earlier.entity_id=r.entity_id AND earlier.new_version<r.new_version)
				FROM clinical_revisions r WHERE patient_id=$1 AND owner_user_id=$2 AND specialty_context_id=$3
				  AND ($4::bigint=0 OR id<$4) AND entity=ANY($5::text[])
				  AND (entity<>'PRESCRIPTION' OR previous_content->>'author_user_id'=$2)
				  AND ($6::bigint=0 OR (entity='ENCOUNTER' AND entity_id=$6))
				ORDER BY id DESC LIMIT $7`, id, user, api.SpecialtyContextID(ctx), before, entities, encounterID, revisionPageSize+1)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var item revisionItem
				var at time.Time
				if err := rows.Scan(&item.ID, &item.Entity, &item.EntityID, &item.Actor, &at, &item.Operation,
					&item.PreviousVersion, &item.NewVersion, &item.Reason, &item.Previous, &item.FirstRecorded); err != nil {
					return err
				}
				item.At = api.FormatInstant(at)
				if item.Entity == "ENCOUNTER" || item.Entity == "PATIENT" {
					item.Derived, err = historicalProjections(item.Previous)
					if err != nil {
						return err
					}
				}
				result.Items = append(result.Items, item)
			}
			if len(result.Items) > revisionPageSize {
				result.Items = result.Items[:revisionPageSize]
				result.NextBefore = &result.Items[revisionPageSize-1].ID
			}
			return rows.Err()
		})
		clinicalReply(w, result, err, 200)
	})
}

func (s *Service) archivedCalculations(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	before, err := revisionCursor(r)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	encounterID, err := revisionEncounter(r)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	ctx := r.Context()
	user := api.User(ctx).UserID
	page := revisionPage[calculationLog]{Items: []calculationLog{}}
	err = s.transaction(ctx, true, func(tx pgx.Tx) error {
		if _, err := readArchivedPatient(ctx, tx, user, id); err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, user, "CALCULATORS"); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT "+calculationLogColumns+`
			FROM patient_calculation_logs l JOIN patients p ON p.id=l.patient_id
			LEFT JOIN clinical_encounters e ON e.id=l.encounter_id AND e.patient_id=p.id
			WHERE p.id=$1 AND p.user_id=$2 AND p.specialty_context_id=$3 AND l.user_id=$2
			  AND (p.annulled_at IS NOT NULL OR e.annulled_at IS NOT NULL)
			  AND ($4::bigint=0 OR l.id<$4) AND ($5::bigint=0 OR l.encounter_id=$5)
			ORDER BY l.id DESC LIMIT $6`, id, user, api.SpecialtyContextID(ctx), before, encounterID, revisionPageSize+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			calculation, err := scanCalculationLog(rows)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, calculation)
		}
		if len(page.Items) > revisionPageSize {
			page.Items = page.Items[:revisionPageSize]
			page.NextBefore = &page.Items[revisionPageSize-1].ID
		}
		return rows.Err()
	})
	clinicalReply(w, page, err, 200)
}

func (s *Service) archivedStudyFile(w http.ResponseWriter, r *http.Request) {
	if err := prescriptionRole(r); err != nil {
		api.WriteError(w, err)
		return
	}
	patientID, err := pathID(r, "patientId")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	studyID, err := pathID(r, "studyId")
	if err != nil {
		api.WriteError(w, err)
		return
	}
	ctx := r.Context()
	user := api.User(ctx).UserID
	var item study
	err = s.transaction(ctx, true, func(tx pgx.Tx) error {
		if _, err := readArchivedPatient(ctx, tx, user, patientID); err != nil {
			return err
		}
		if err := s.workspaces.Feature(ctx, tx, user, "STUDIES"); err != nil {
			return err
		}
		var err error
		item, err = scanStudy(tx.QueryRow(ctx, "SELECT "+studyColumns+` FROM patient_medical_studies
			WHERE patient_id=$1 AND id=$2 AND annulled_at IS NOT NULL
			  AND EXISTS (SELECT 1 FROM clinical_revisions r WHERE r.patient_id=$1
			                AND r.entity='STUDY' AND r.entity_id=$2 AND r.operation='ANNUL')`, patientID, studyID))
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(404, "STUDY_NOT_FOUND", "No se encontró el estudio en el historial.")
		}
		return err
	})
	if err != nil {
		api.WriteError(w, err)
		return
	}
	s.serveStudy(w, r, item)
}
