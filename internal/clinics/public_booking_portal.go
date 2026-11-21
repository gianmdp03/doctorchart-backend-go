package clinics

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/notifications"
)

type portalIdentity struct {
	DocumentCountry string `json:"documentCountry"`
	DocumentType    string `json:"documentType"`
	Document        string `json:"document"`
	Email           string `json:"email"`
}

func (p *portalIdentity) normalize() error {
	p.Document = strings.ToUpper(strings.TrimSpace(p.Document))
	p.Email = strings.TrimSpace(p.Email)
	return validatePublicIdentity(p.DocumentCountry, p.DocumentType, p.Document, p.Email)
}

type portalAccess struct {
	portalIdentity
	RequestID string    `json:"requestId"`
	Session   string    `json:"session"`
	ExpiresAt time.Time `json:"expiresAt"`
}

const portalCookie = "__Secure-dc_turnos"
const portalCookiePath = "/api/doctorchart/public/appointments"
const portalSessionSeconds = 48 * 60 * 60

func setPortalCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: portalCookie, Value: value, Path: portalCookiePath, Secure: true,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

const portalPair = "p.document_country=$1 AND p.document_type=$2 AND p.document=$3 AND p.email=$4"

func (p portalIdentity) args() []any {
	return []any{p.DocumentCountry, p.DocumentType, p.Document, p.Email}
}

func (s *Service) portalHash(scope string, values ...string) string {
	mac := hmac.New(sha256.New, []byte(s.publicSecret))
	mac.Write([]byte("doctorchart-portal:" + scope + "\n" + strings.Join(values, "\n")))
	return hex.EncodeToString(mac.Sum(nil))
}

func (p portalIdentity) subject(s *Service) string {
	return s.portalHash("identity", p.DocumentCountry, p.DocumentType, p.Document, p.Email)
}

func portalDenied() error {
	return api.Failure(403, "BOOKING_ACCESS_DENIED", "El código no es válido o venció. Revisalo o pedí uno nuevo.")
}

var portalCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

func maskedPortalEmail(email string) string {
	at := strings.LastIndexByte(email, '@')
	if at < 1 {
		return "***"
	}
	prefix := []rune(email[:at])
	return string(prefix[:min(3, len(prefix)-1)]) + "***" + email[at:]
}

func (s *Service) portalIdentityForReference(ctx context.Context, document, reference string) (portalIdentity, error) {
	p := portalIdentity{DocumentCountry: "AR", DocumentType: "DNI", Document: document}
	if !dniPattern.MatchString(document) || !opaqueID.MatchString(reference) {
		return p, portalDenied()
	}
	err := s.pool.QueryRow(ctx, `SELECT email FROM booking_public_details
		WHERE reference=$1 AND document_country='AR' AND document_type='DNI' AND document=$2`,
		reference, document).Scan(&p.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, portalDenied()
	}
	return p, err
}

// The booking path and the public lookup share the same inbox limit.
func (s *Service) issuePortalCode(ctx context.Context, tx pgx.Tx, p portalIdentity, bookingID, proofHash *string) (string, error) {
	inbox := s.portalHash("inbox", strings.ToLower(p.Email))
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('doctorchart-booking-code:'||$1,0))", inbox); err != nil {
		return "", err
	}
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT count(*)<5 AND count(*) FILTER (WHERE created_at>clock_timestamp()-interval '45 seconds')=0
		FROM booking_portal_codes WHERE email_hash=$1 AND created_at>clock_timestamp()-interval '1 hour'`, inbox).Scan(&allowed); err != nil || !allowed {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	code, id := fmt.Sprintf("%06d", n.Int64()), newOpaqueID()
	result, err := tx.Exec(ctx, `INSERT INTO booking_portal_codes(id,booking_id,subject_hash,email_hash,proof_hash,code_hash,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()+interval '10 minutes') ON CONFLICT DO NOTHING`,
		id, bookingID, p.subject(s), inbox, proofHash, s.portalHash("code", id, code))
	if err != nil || result.RowsAffected() == 0 {
		return "", err
	}
	return id, s.notices.BookingPortalCode(ctx, tx, id, p.Email, code)
}

func (s *Service) requestPortalCode(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		portalIdentity
		Reference string `json:"reference"`
		Challenge string `json:"challenge"`
		Solution  string `json:"solution"`
	}
	if err := decodePublic(w, r, &req); err != nil {
		return err
	}
	proof, err := s.verifyProof(req.Challenge, req.Solution, r.Header.Get("X-Public-Client"))
	if err != nil {
		return err
	}
	if req.Reference != "" {
		req.portalIdentity, err = s.portalIdentityForReference(r.Context(), req.Document, req.Reference)
	} else {
		err = req.normalize()
	}
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var exists bool
	if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM booking_public_details p WHERE "+portalPair+")", req.args()...).Scan(&exists); err != nil {
		return err
	}
	var id string
	if exists {
		id, err = s.issuePortalCode(r.Context(), tx, req.portalIdentity, nil, &proof)
		if err != nil {
			return err
		}
		if id == "" && req.Reference != "" {
			return api.Failure(429, "BOOKING_CODE_LIMIT", "Esperá antes de pedir otro código.")
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	if id == "" {
		id = newOpaqueID()
	}
	response := map[string]string{"requestId": id}
	if req.Reference != "" {
		response["maskedEmail"] = maskedPortalEmail(req.Email)
	}
	api.WriteJSON(w, 202, response)
	return nil
}

func (s *Service) verifyPortalCode(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		portalIdentity
		Reference string `json:"reference"`
		RequestID string `json:"requestId"`
		Code      string `json:"code"`
	}
	if err := decodePublic(w, r, &req); err != nil {
		return err
	}
	if !opaqueID.MatchString(req.RequestID) || !portalCodePattern.MatchString(req.Code) {
		return portalDenied()
	}
	var err error
	if req.Reference != "" {
		req.portalIdentity, err = s.portalIdentityForReference(r.Context(), req.Document, req.Reference)
	} else {
		err = req.normalize()
	}
	if err != nil {
		return err
	}
	session := notifications.Token()
	var accepted bool
	err = s.pool.QueryRow(r.Context(), `UPDATE booking_portal_codes SET attempts=attempts+1,
		session_hash=CASE WHEN code_hash=$3 THEN $4::character(64) END,
		session_expires_at=CASE WHEN code_hash=$3 THEN clock_timestamp()+make_interval(secs=>$5) END
		WHERE id=$1 AND subject_hash=$2 AND session_hash IS NULL AND attempts<5 AND expires_at>clock_timestamp()
		RETURNING session_hash IS NOT NULL`,
		req.RequestID, req.subject(s), s.portalHash("code", req.RequestID, req.Code), notifications.Hash(session), float64(portalSessionSeconds)).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !accepted {
		return portalDenied()
	}
	if err != nil {
		return err
	}
	raw, err := json.Marshal(portalAccess{
		portalIdentity: req.portalIdentity, RequestID: req.RequestID, Session: session,
		ExpiresAt: time.Now().UTC().Add(portalSessionSeconds * time.Second),
	})
	if err != nil {
		return err
	}
	setPortalCookie(w, s.notices.SealPortalSession(raw), portalSessionSeconds)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) portalAccess(r *http.Request) (portalAccess, error) {
	var access portalAccess
	cookies := r.CookiesNamed(portalCookie)
	if len(cookies) != 1 || len(cookies[0].Value) > 3072 {
		return access, portalDenied()
	}
	raw, err := s.notices.OpenPortalSession(cookies[0].Value)
	if err != nil || json.Unmarshal(raw, &access) != nil {
		return access, portalDenied()
	}
	return access, s.authorizePortal(r.Context(), &access)
}

func (s *Service) closePortal(w http.ResponseWriter, r *http.Request) error {
	var req struct{}
	if err := decodePublic(w, r, &req); err != nil {
		return err
	}
	access, err := s.portalAccess(r)
	if err == nil {
		_, err = s.pool.Exec(r.Context(), "UPDATE booking_portal_codes SET session_expires_at=clock_timestamp() WHERE id=$1 AND session_hash=$2",
			access.RequestID, notifications.Hash(access.Session))
	}
	if err != nil {
		var failure *api.Error
		if !errors.As(err, &failure) || failure.Code != "BOOKING_ACCESS_DENIED" {
			return err
		}
	}
	setPortalCookie(w, "", -1)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) authorizePortal(ctx context.Context, access *portalAccess) error {
	if err := access.normalize(); err != nil {
		return err
	}
	if !opaqueID.MatchString(access.RequestID) || len(access.Session) != 43 {
		return portalDenied()
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM booking_portal_codes WHERE id=$1 AND subject_hash=$2
		AND session_hash=$3 AND session_expires_at>clock_timestamp())`,
		access.RequestID, access.subject(s), notifications.Hash(access.Session)).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return portalDenied()
	}
	return nil
}

func (s *Service) portalBookings(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Offset    int    `json:"offset"`
		Document  string `json:"document"`
		Challenge string `json:"challenge"`
		Solution  string `json:"solution"`
		Focus     string `json:"focusReference"`
	}
	if err := decodePublic(w, r, &req); err != nil {
		return err
	}
	if req.Offset < 0 {
		return api.Invalid("Página inválida.")
	}
	if req.Document != "" {
		if !dniPattern.MatchString(req.Document) {
			return field("document", "INVALID_DOCUMENT")
		}
		if _, err := s.verifyProof(req.Challenge, req.Solution, r.Header.Get("X-Public-Client")); err != nil {
			return err
		}
		return s.previewPortalBookings(w, r, req.Document, req.Offset)
	}
	access, err := s.portalAccess(r)
	if err != nil {
		return err
	}
	if req.Focus != "" && !opaqueID.MatchString(req.Focus) {
		return api.Invalid("Reserva inválida.")
	}
	rows, err := s.pool.Query(r.Context(), `SELECT jsonb_build_object(
		'reference',p.reference,'status',b.status,'start',b.start_at,'end',b.end_at,'doctorName',p.doctor_name,
		'clinicName',b.clinic_snapshot->>'name',
		'address',CASE WHEN COALESCE(b.clinic_snapshot->>'publicAddress','')='' AND EXISTS(
			SELECT 1 FROM booking_events e WHERE e.booking_id=b.id AND e.version=1 AND e.kind='CREATED' AND e.actor_id<>'PUBLIC_BOOKING')
			THEN b.clinic_snapshot->>'address' ELSE b.clinic_snapshot->>'publicAddress' END,
		'timezone',b.clinic_snapshot->>'timezone',
		'modality',b.clinic_snapshot->>'modality','specialtyName',b.specialty_snapshot->>'name','version',b.version::text,
		'canChange',b.start_at>clock_timestamp() AND b.status='CONFIRMED',
		'publicDoctorId',CASE WHEN cp.enabled AND bp.published THEN bp.public_id::text END,
		'publicClinicId',CASE WHEN cp.enabled AND bp.published THEN cp.public_id::text END,
		'specialtyContextId',b.specialty_context_id::text)
		FROM booking_public_details p JOIN bookings b ON b.id=p.booking_id
		LEFT JOIN clinic_publications cp ON cp.clinic_id=b.clinic_id
		LEFT JOIN booking_publications bp ON bp.workspace_id=b.workspace_id
		WHERE `+portalPair+` ORDER BY (p.reference::text=$6) DESC,(b.start_at>=now()) DESC,
		CASE WHEN b.start_at>=now() THEN b.start_at END ASC,b.start_at DESC,b.id DESC LIMIT 51 OFFSET $5`, append(access.args(), req.Offset, req.Focus)...)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, pgx.RowTo[json.RawMessage])
	if err != nil {
		return err
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	api.WriteJSON(w, 200, map[string]any{"items": items, "hasMore": more, "expiresAt": access.ExpiresAt})
	return nil
}

func (s *Service) previewPortalBookings(w http.ResponseWriter, r *http.Request, document string, offset int) error {
	// A DNI is an identifier, not proof of access. Keep personal and clinical details behind the code.
	var verifiedEmail string
	if len(r.CookiesNamed(portalCookie)) != 0 {
		access, err := s.portalAccess(r)
		if err == nil && access.DocumentCountry == "AR" && access.DocumentType == "DNI" && access.Document == document {
			verifiedEmail = access.Email
		} else if err != nil {
			var failure *api.Error
			if !errors.As(err, &failure) || failure.Code != "BOOKING_ACCESS_DENIED" {
				return err
			}
		}
	}
	rows, err := s.pool.Query(r.Context(), `SELECT jsonb_build_object('reference',p.reference,
		'date',to_char(b.start_at AT TIME ZONE p.timezone,'YYYY-MM-DD'),
		'canAccess',p.email=$3,
		'canChange',b.start_at>clock_timestamp() AND b.status='CONFIRMED',
		'canReschedule',b.start_at>clock_timestamp() AND b.status='CONFIRMED' AND COALESCE(cp.enabled AND bp.published,false))
		FROM booking_public_details p JOIN bookings b ON b.id=p.booking_id
		LEFT JOIN clinic_publications cp ON cp.clinic_id=b.clinic_id
		LEFT JOIN booking_publications bp ON bp.workspace_id=b.workspace_id
		WHERE p.document_country='AR' AND p.document_type='DNI' AND p.document=$1
		ORDER BY (b.start_at>=now()) DESC, CASE WHEN b.start_at>=now() THEN b.start_at END ASC,
		b.start_at DESC,b.id DESC LIMIT 51 OFFSET $2`, document, offset, verifiedEmail)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, pgx.RowTo[json.RawMessage])
	if err != nil {
		return err
	}
	more := len(items) > 50
	api.WriteJSON(w, 200, map[string]any{"items": items[:min(len(items), 50)], "hasMore": more})
	return nil
}

func (s *Service) changePortalBooking(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Reference       string  `json:"reference"`
		ExpectedVersion string  `json:"expectedVersion"`
		Start           string  `json:"start,omitempty"`
		Action          string  `json:"action"`
		Reason          *string `json:"reason,omitempty"`
	}
	if err := decodePublic(w, r, &req); err != nil {
		return err
	}
	access, err := s.portalAccess(r)
	if err != nil {
		return err
	}
	key := r.Header.Get("Idempotency-Key")
	if !opaqueID.MatchString(key) || !opaqueID.MatchString(req.Reference) {
		return api.Invalid("Reserva inválida.")
	}
	if _, err := decimal(req.ExpectedVersion, true); err != nil {
		return err
	}
	op := "status"
	reason, err := bookingReason(req.Reason)
	if err != nil {
		return err
	}
	request := bookingRequest{ExpectedVersion: req.ExpectedVersion, Status: "CANCELLED", Reason: reason}
	switch req.Action {
	case "cancel":
		if req.Start != "" {
			return api.Invalid("Solicitud inválida.")
		}
	case "reschedule":
		if _, err := bookingInstant(req.Start); err != nil {
			return err
		}
		op, request.Start, request.Status = "reschedule", req.Start, ""
	default:
		return api.Invalid("Elegí cancelar o reprogramar el turno.")
	}
	var previous Booking
	var owner string
	a := &publicAdmission{management: op}
	err = s.pool.QueryRow(r.Context(), `SELECT b.id::text,b.clinic_id::text,b.specialty_context_id::text,w.user_id,
		COALESCE(bp.public_id::text,''),COALESCE(cp.public_id::text,'')
		FROM booking_public_details p JOIN bookings b ON b.id=p.booking_id JOIN doctor_workspaces w ON w.id=b.workspace_id
		LEFT JOIN booking_publications bp ON bp.workspace_id=b.workspace_id
		LEFT JOIN clinic_publications cp ON cp.clinic_id=b.clinic_id
		WHERE `+portalPair+` AND p.reference=$5`, append(access.args(), req.Reference)...).
		Scan(&previous.ID, &request.ClinicID, &a.contextID, &owner, &a.doctorID, &a.clinicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return bookingMissing()
	}
	if err != nil {
		return err
	}
	change := []string{req.Reference, req.ExpectedVersion, req.Action, req.Start}
	if reason != nil {
		change = append(change, *reason)
	}
	hash := notifications.Hash(strings.Join(change, "\n"))
	return s.applyBooking(w, r, owner, op, previous, request, "portal:"+key, hash, a)
}
