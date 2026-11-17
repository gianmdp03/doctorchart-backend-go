package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

type reviewEvidence struct {
	Specialty   string `json:"specialty"`
	Revision    int64  `json:"revision"`
	Fingerprint string `json:"fingerprint"`
	Reviewer    string `json:"reviewer"`
	Reference   string `json:"reference"`
}

// ReviewAuthority holds externally assessed evidence provisioned by the operator.
// The authenticated subject only records the matching evidence in DoctorChart.
type ReviewAuthority struct {
	evidence          []reviewEvidence
	syntheticReviewer string
}

func NewReviewAuthority(manifest, syntheticReviewer string) (ReviewAuthority, error) {
	a := ReviewAuthority{syntheticReviewer: syntheticReviewer}
	if manifest == "" {
		return a, nil
	}
	decoder := json.NewDecoder(strings.NewReader(manifest))
	decoder.DisallowUnknownFields()
	if !json.Valid([]byte(manifest)) || decoder.Decode(&a.evidence) != nil || a.evidence == nil {
		return a, fmt.Errorf("invalid clinical template evidence manifest")
	}
	for i, e := range a.evidence {
		if !codePattern.MatchString(e.Specialty) || e.Revision < 0 || !reviewFingerprint.MatchString(e.Fingerprint) ||
			!validReviewText(e.Reviewer, 100) || !validReviewText(e.Reference, 500) {
			return a, fmt.Errorf("invalid clinical template evidence entry %d", i+1)
		}
		for _, prior := range a.evidence[:i] {
			if prior.Specialty == e.Specialty && prior.Revision == e.Revision && prior.Fingerprint == e.Fingerprint && prior.Reviewer == e.Reviewer {
				return a, fmt.Errorf("duplicate clinical template evidence entry %d", i+1)
			}
		}
	}
	return a, nil
}

var reviewFingerprint = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validReviewText(text string, maximum int) bool {
	return strings.TrimSpace(text) != "" && api.JavaLength(text) <= maximum && !strings.ContainsRune(text, 0)
}

type templateReview struct {
	ID               int64      `json:"id"`
	Revision         int64      `json:"revision"`
	Fingerprint      string     `json:"fingerprint"`
	Actor            string     `json:"actor"`
	Kind             string     `json:"kind"`
	Reference        string     `json:"reference"`
	At               time.Time  `json:"occurredAt"`
	RevokedBy        *string    `json:"revokedBy"`
	RevokedAt        *time.Time `json:"revokedAt"`
	RevocationReason *string    `json:"revocationReason"`
}

type reviewStatus struct {
	Revision            int64            `json:"revision"`
	Fingerprint         string           `json:"fingerprint"`
	DraftState          string           `json:"draftState"`
	PublicationState    string           `json:"publicationState"`
	CanApprove          bool             `json:"canApprove"`
	CanPublish          bool             `json:"canPublish"`
	CanActivate         bool             `json:"canActivate"`
	PublishedApprovalID *int64           `json:"publishedApprovalId"`
	History             []templateReview `json:"history"`
	NextBefore          *int64           `json:"nextBefore"`
}

type reviewDraft struct {
	Code                string
	Revision            int64
	PublishedVersion    int64
	PublishedApprovalID *int64
	Content             json.RawMessage
	Fingerprint         string
}

func templateAvailable(ctx context.Context, tx pgx.Tx, id int64, requireActive bool) (bool, error) {
	var available bool
	err := tx.QueryRow(ctx, "SELECT public.template_available($1,$2)", id, requireActive).Scan(&available)
	return available, err
}

func reviewDraftFor(ctx context.Context, tx pgx.Tx, id int64) (reviewDraft, error) {
	var draft reviewDraft
	err := tx.QueryRow(ctx, `SELECT code,version,published_version,published_approval_id,content,
		encode(sha256(convert_to(content::text,'UTF8')),'hex')
		FROM (SELECT s.code,w.version,w.published_version,w.published_approval_id,
		public.template_review_content(w.draft_definition,coalesce(w.draft_experience,
			public.template_experience(s.navigation,s.patient_record_tabs,s.patient_record_actions))) content
		FROM specialties s JOIN wizard_defaults w ON w.specialty_id=s.id WHERE s.id=$1) v`, id).
		Scan(&draft.Code, &draft.Revision, &draft.PublishedVersion, &draft.PublishedApprovalID, &draft.Content, &draft.Fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		err = api.Failure(404, "SPECIALTY_NOT_FOUND", "La especialidad no está disponible.")
	}
	return draft, err
}

func (s *Store) reviewEvidence(ctx context.Context, tx pgx.Tx, draft reviewDraft) (kind, reference string, err error) {
	actor := api.User(ctx)
	if !actor.HasRole("SUPER_ADMIN") || actor.UserID == "" {
		return "", "", nil
	}
	for _, e := range s.reviews.evidence {
		if e.Specialty == draft.Code && e.Revision == draft.Revision && e.Fingerprint == draft.Fingerprint && e.Reviewer == actor.UserID {
			return "CLINICAL", e.Reference, nil
		}
	}
	if actor.UserID == s.reviews.syntheticReviewer {
		var enabled bool
		if err := tx.QueryRow(ctx, "SELECT public.template_approval_allowed('SYNTHETIC')").Scan(&enabled); err != nil {
			return "", "", err
		}
		if enabled {
			return "SYNTHETIC", "Ensayo aislado SP-04C", nil
		}
	}
	return "", "", nil
}

func currentDraftApproval(ctx context.Context, tx pgx.Tx, id int64, draft reviewDraft) (*int64, error) {
	var approval *int64
	err := tx.QueryRow(ctx, `SELECT (SELECT a.id FROM wizard_approvals a
		WHERE a.specialty_id=$1 AND a.revision=$2 AND a.fingerprint=$3 AND a.content=$4
		  AND public.template_approval_allowed(a.kind)
		  AND NOT EXISTS (SELECT 1 FROM wizard_approval_revocations r WHERE r.approval_id=a.id))`, id, draft.Revision, draft.Fingerprint, draft.Content).Scan(&approval)
	return approval, err
}

func (s *Store) approveDraft(ctx context.Context, id int64, request map[string]any) error {
	if err := api.Only(request, "expectedVersion"); err != nil {
		return err
	}
	expected, err := api.Version(request, false)
	if err != nil {
		return err
	}
	return s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := catalogLock(ctx, tx); err != nil {
			return err
		}
		draft, err := reviewDraftFor(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := version(draft.Revision, expected, workspaceConflict); err != nil {
			return err
		}
		kind, reference, err := s.reviewEvidence(ctx, tx, draft)
		if err != nil {
			return err
		}
		if kind == "" {
			return api.Failure(403, "TEMPLATE_REVIEW_NOT_AUTHORIZED", "Falta una constancia de revisión autorizada para esta versión.")
		}
		content, err := wizard.Decode(draft.Content)
		if err != nil {
			return err
		}
		definition, err := api.Object(content, "definition")
		if err != nil {
			return err
		}
		if err := s.validator.Validate(definition); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO wizard_approvals(specialty_id,revision,fingerprint,content,actor,kind,reference)
			VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (specialty_id,revision) DO NOTHING`,
			id, draft.Revision, draft.Fingerprint, draft.Content, api.User(ctx).UserID, kind, reference)
		if err == nil && tag.RowsAffected() != 1 {
			return api.Failure(409, "TEMPLATE_REVIEW_ALREADY_RECORDED", "Esta revisión ya tiene una decisión. Guardá un nuevo borrador para volver a evaluarla.")
		}
		return err
	})
}

func (s *Store) revokeDraftApproval(ctx context.Context, id, approval int64, request map[string]any) error {
	if !api.User(ctx).HasRole("SUPER_ADMIN") || api.User(ctx).UserID == "" {
		return api.Failure(403, "FORBIDDEN", "Acceso denegado.")
	}
	if err := api.Only(request, "reason"); err != nil {
		return err
	}
	reason, _ := request["reason"].(string)
	if !validReviewText(reason, 500) {
		return api.Invalid("Ingresá un motivo de revocación de hasta 500 caracteres.")
	}
	return s.transaction(ctx, false, func(tx pgx.Tx) error {
		if err := catalogLock(ctx, tx); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO wizard_approval_revocations(approval_id,actor,reason)
			SELECT id,$3,$4 FROM wizard_approvals WHERE id=$1 AND specialty_id=$2
			ON CONFLICT (approval_id) DO NOTHING`, approval, id, api.User(ctx).UserID, strings.TrimSpace(reason))
		if err == nil && tag.RowsAffected() != 1 {
			return api.Failure(409, "TEMPLATE_REVOCATION_UNAVAILABLE", "La aprobación es inexistente o ya fue revocada.")
		}
		return err
	})
}

func (s *Store) reviewStatus(ctx context.Context, id, before int64) (reviewStatus, error) {
	result := reviewStatus{DraftState: "PENDING", PublicationState: "UNPUBLISHED", History: []templateReview{}}
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		draft, err := reviewDraftFor(ctx, tx, id)
		if err != nil {
			return err
		}
		result.Revision, result.Fingerprint, result.PublishedApprovalID = draft.Revision, draft.Fingerprint, draft.PublishedApprovalID
		approval, err := currentDraftApproval(ctx, tx, id, draft)
		if err != nil {
			return err
		}
		result.CanPublish = approval != nil
		kind, _, err := s.reviewEvidence(ctx, tx, draft)
		if err != nil {
			return err
		}
		result.CanApprove = kind != "" && !result.CanPublish
		if result.CanPublish {
			result.DraftState = "APPROVED"
		}
		if draft.PublishedVersion > 0 {
			result.PublicationState = "LEGACY"
		}
		if draft.PublishedApprovalID != nil {
			result.CanActivate, err = templateAvailable(ctx, tx, id, false)
			if err != nil {
				return err
			}
			result.PublicationState = "BLOCKED"
			if result.CanActivate {
				result.PublicationState = "APPROVED"
			}
			var revoked, same bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wizard_approval_revocations WHERE approval_id=a.id),a.revision+1=$2 AND a.fingerprint=$3
				FROM wizard_approvals a WHERE a.id=$1`, *draft.PublishedApprovalID, draft.Revision, draft.Fingerprint).Scan(&revoked, &same)
			if err != nil {
				return err
			}
			if revoked {
				result.PublicationState = "REVOKED"
			}
			if same && result.CanActivate {
				result.DraftState = "PUBLISHED"
				result.CanApprove = false
			}
		}
		var recorded bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wizard_approvals WHERE specialty_id=$1 AND revision=$2)`, id, draft.Revision).Scan(&recorded); err != nil {
			return err
		}
		if recorded && !result.CanPublish {
			result.DraftState = "BLOCKED"
			result.CanApprove = false
		}
		rows, err := tx.Query(ctx, `SELECT a.id,a.revision,a.fingerprint,a.actor,a.kind,a.reference,a.occurred_at,r.actor,r.occurred_at,r.reason
			FROM wizard_approvals a LEFT JOIN wizard_approval_revocations r ON r.approval_id=a.id
			WHERE a.specialty_id=$1 AND ($2::bigint=0 OR a.id<$2) ORDER BY a.id DESC LIMIT 26`, id, before)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v templateReview
			if err := rows.Scan(&v.ID, &v.Revision, &v.Fingerprint, &v.Actor, &v.Kind, &v.Reference, &v.At, &v.RevokedBy, &v.RevokedAt, &v.RevocationReason); err != nil {
				return err
			}
			result.History = append(result.History, v)
		}
		if len(result.History) > 25 {
			result.History = result.History[:25]
			result.NextBefore = &result.History[24].ID
		}
		return rows.Err()
	})
	return result, err
}

func (s *Store) reviewRoutes(mux *http.ServeMux) {
	const root = "/api/doctorchart/admin/specialties/{id}/wizard/reviews"
	register(mux, "GET "+root, func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, 0, err
		}
		var before int64
		if raw := r.URL.Query().Get("before"); raw != "" {
			before, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || before <= 0 {
				return nil, 0, api.Invalid("La página de historial no es válida.")
			}
		}
		value, err := s.reviewStatus(r.Context(), id, before)
		return value, 200, err
	})
	register(mux, "POST "+root, func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, 0, err
		}
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		return nil, 204, s.approveDraft(r.Context(), id, request)
	})
	register(mux, "POST "+root+"/{approval}/revoke", func(w http.ResponseWriter, r *http.Request) (any, int, error) {
		id, err := pathID(r)
		if err != nil {
			return nil, 0, err
		}
		approval, err := strconv.ParseInt(r.PathValue("approval"), 10, 64)
		if err != nil || approval <= 0 {
			return nil, 0, api.Invalid("La aprobación no es válida.")
		}
		request, err := api.Decode(w, r)
		if err != nil {
			return nil, 0, err
		}
		return nil, 204, s.revokeDraftApproval(r.Context(), id, approval, request)
	})
}
