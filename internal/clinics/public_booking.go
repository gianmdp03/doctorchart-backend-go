package clinics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
	"golang.org/x/text/language"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
)

type PublicBookingRequest struct {
	ContextID       string            `json:"specialtyContextId,omitempty"`
	ClinicID        string            `json:"clinicId"`
	Start           string            `json:"start"`
	FormVersion     string            `json:"formVersion"`
	ContactName     string            `json:"contactName"`
	ContactPhone    string            `json:"contactPhone"`
	AttendeeName    string            `json:"attendeeName"`
	DocumentCountry string            `json:"documentCountry"`
	DocumentType    string            `json:"documentType"`
	Document        string            `json:"document"`
	Email           string            `json:"email"`
	Answers         map[string]string `json:"answers"`
	Challenge       string            `json:"challenge,omitempty"`
	Solution        string            `json:"solution,omitempty"`
}
type publicAdmission struct {
	contextID                   string
	doctorID, clinicID, network string
	request                     PublicBookingRequest
	doctor                      PublicDoctor
	clinic                      PublicClinic
	challengeHash               string
	management                  string
}

var dniPattern = regexp.MustCompile(`^[0-9]{5,8}$`)
var passportPattern = regexp.MustCompile(`^[A-Z0-9]{3,24}$`)
var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)

func validatePublicRequest(p *PublicBookingRequest) error {
	if p.ContextID != "" {
		if _, err := decimal(p.ContextID, true); err != nil {
			return field("specialtyContextId", "INVALID_ID")
		}
	}
	p.ContactName = strings.TrimSpace(p.ContactName)
	p.AttendeeName = strings.TrimSpace(p.AttendeeName)
	p.Email = strings.TrimSpace(p.Email)
	p.Document = strings.ToUpper(strings.TrimSpace(p.Document))
	p.ContactPhone = strings.TrimSpace(p.ContactPhone)
	if !opaqueID.MatchString(p.ClinicID) {
		return api.Invalid("Consultorio inválido.")
	}
	if _, err := decimal(p.FormVersion, true); err != nil {
		return err
	}
	if _, err := bookingInstant(p.Start); err != nil {
		return err
	}
	for _, name := range []string{p.ContactName, p.AttendeeName} {
		if len([]rune(name)) < 1 || len([]rune(name)) > 160 || strings.ContainsRune(name, 0) {
			return api.Invalid("Ingresá nombre y apellido del solicitante y de quien será atendido.")
		}
	}
	if err := validatePublicIdentity(p.DocumentCountry, p.DocumentType, p.Document, p.Email); err != nil {
		return err
	}
	return validatePublicAnswers(p.Answers)
}

func validatePublicAnswers(answers map[string]string) error {
	if len(answers) > 8 {
		return api.Invalid("Demasiadas respuestas.")
	}
	for k, v := range answers {
		if !questionKey.MatchString(k) || len([]rune(v)) > 240 || strings.ContainsRune(v, 0) {
			return api.Invalid("Respuesta administrativa inválida.")
		}
	}
	return nil
}

func validatePublicIdentity(country, kind, document, email string) error {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || !strings.Contains(address.Address, ".") || strings.ContainsAny(email, "\r\n") {
		return field("email", "INVALID_EMAIL")
	}
	region, regionErr := language.ParseRegion(country)
	if regionErr != nil || !region.IsCountry() {
		return field("documentCountry", "INVALID_COUNTRY")
	}
	if !countryPattern.MatchString(country) || kind == "DNI" && (country != "AR" || !dniPattern.MatchString(document)) || kind == "PASSPORT" && !passportPattern.MatchString(document) || kind != "DNI" && kind != "PASSPORT" {
		return field("document", "INVALID_DOCUMENT")
	}
	return nil
}
func (a *publicAdmission) check(ctx context.Context, tx pgx.Tx, s *Service, workspace int64, snap billing.Snapshot) error {
	if a.management == "status" || a.management == "confirm" {
		return nil
	}
	if snap.OnlineBooking == nil || !*snap.OnlineBooking || !snap.ClinicManagement || !snap.AllowsAppointments() {
		return publicDenied()
	}
	current, err := s.readPublicDoctor(ctx, tx, a.doctorID)
	if err != nil {
		return err
	}
	if current.workspace != workspace || !current.published {
		return publicMissing()
	}
	if a.management == "reschedule" {
		if !publicContextClinic(current, a.contextID, a.clinicID) {
			return publicMissing()
		}
		return nil
	}
	a.contextID, err = choosePublicContext(current, a.request.ContextID)
	if err != nil {
		return err
	}
	if !publicContextClinic(current, a.contextID, a.clinicID) {
		return publicMissing()
	}
	a.doctor = current
	found := false
	for _, c := range current.Clinics {
		if c.ID == a.clinicID {
			a.clinic = c
			found = true
		}
	}
	if !found {
		return publicMissing()
	}
	if err := checkPublicForm(current.Form, a.request.FormVersion, a.request.Answers); err != nil {
		return err
	}
	a.challengeHash, err = s.verifyProof(a.request.Challenge, a.request.Solution, a.network)
	return err
}

func checkPublicForm(form PublicForm, version string, answers map[string]string) error {
	if form.Version != version {
		return api.Failure(409, "FORM_VERSION_CONFLICT", "El formulario cambió. Revisá las preguntas actualizadas; tus datos se conservaron.")
	}
	allowed := map[string]bool{}
	for _, q := range form.Questions {
		allowed[q.Key] = true
		if q.Required && strings.TrimSpace(answers[q.Key]) == "" {
			return field(q.Key, "REQUIRED")
		}
	}
	for k := range answers {
		if !allowed[k] {
			return api.Invalid("El formulario contiene preguntas que ya no están vigentes.")
		}
	}
	return nil
}
func (a *publicAdmission) record(ctx context.Context, tx pgx.Tx, b Booking) error {
	form, _ := json.Marshal(a.doctor.Form)
	answers, _ := json.Marshal(a.request.Answers)
	if a.request.Answers == nil {
		answers = []byte("{}")
	}
	var used bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM booking_public_details WHERE challenge_hash=$1)", a.challengeHash).Scan(&used); err != nil {
		return err
	}
	if used {
		return api.Failure(403, "CHALLENGE_ALREADY_USED", "La verificación ya se utilizó. Reintentá para obtener una nueva.")
	}
	result, err := tx.Exec(ctx, "INSERT INTO booking_public_details(booking_id,reference,form_version,form_snapshot,answers,document_country,document_type,document,email,challenge_hash,doctor_name,clinic_name,address,timezone) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT(challenge_hash) DO NOTHING", b.ID, newOpaqueID(), a.request.FormVersion, form, answers, a.request.DocumentCountry, a.request.DocumentType, a.request.Document, a.request.Email, a.challengeHash, a.doctor.Name, a.clinic.Name, a.clinic.Address, a.clinic.Timezone)
	if err == nil && result.RowsAffected() == 0 {
		return api.Failure(403, "CHALLENGE_ALREADY_USED", "La verificación ya se utilizó. Reintentá para obtener una nueva.")
	}
	return err
}
func (s *Service) writeBookingResult(w http.ResponseWriter, r *http.Request, a *publicAdmission, status int, response json.RawMessage) error {
	if a == nil {
		if staffFrom(r.Context()) != nil {
			doc, err := staffBookingDocument(response)
			if err != nil {
				return err
			}
			api.WriteJSON(w, status, doc)
			return nil
		}
	}
	// Project historical idempotency responses through the administrative shape.
	var b Booking
	if err := json.Unmarshal(response, &b); err != nil {
		return err
	}
	if a == nil {
		api.WriteJSON(w, status, b)
		return nil
	}
	var reference, name, clinic, address, timezone string
	if err := s.pool.QueryRow(r.Context(), "SELECT reference::text,doctor_name,clinic_name,address,timezone FROM booking_public_details WHERE booking_id=$1", b.ID).Scan(&reference, &name, &clinic, &address, &timezone); err != nil {
		return err
	}
	if b.ClinicSnapshot.Timezone != "" {
		clinic, address, timezone = b.ClinicSnapshot.Name, b.ClinicSnapshot.PublicAddress, b.ClinicSnapshot.Timezone
	}
	document := map[string]any{"reference": reference, "status": b.Status, "start": b.Start, "end": b.End, "doctorName": name, "clinicName": clinic, "modality": b.ClinicSnapshot.Modality, "address": address, "timezone": timezone, "contactName": b.ContactName, "attendeeName": b.AttendeeName}
	if b.Specialty.Name != "" {
		document["specialtyName"] = b.Specialty.Name
	}
	if a.management != "" {
		delete(document, "contactName")
		delete(document, "attendeeName")
	} else if s.notices != nil {
		var id string
		err := s.pool.QueryRow(r.Context(), "SELECT id::text FROM booking_portal_codes WHERE booking_id=$1 AND expires_at>clock_timestamp() AND session_hash IS NULL", b.ID).Scan(&id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if id != "" {
			document["verificationRequestId"] = id
			document["maskedEmail"] = maskedPortalEmail(a.request.Email)
		}
	}
	api.WriteJSON(w, status, document)
	return nil
}
func (s *Service) publicCreate(w http.ResponseWriter, r *http.Request) error {
	var p PublicBookingRequest
	if err := decodePublic(w, r, &p); err != nil {
		return err
	}
	if p.DocumentType != "DNI" || p.DocumentCountry != "AR" {
		return field("document", "INVALID_DOCUMENT")
	}
	if err := validatePublicRequest(&p); err != nil {
		return err
	}
	key := r.Header.Get("Idempotency-Key")
	if len(r.Header.Values("Idempotency-Key")) != 1 || !opaqueID.MatchString(key) {
		return field("Idempotency-Key", "INVALID_KEY")
	}
	doctor, err := s.readPublicDoctor(r.Context(), s.pool, r.PathValue("publicId"))
	if err != nil {
		return err
	}
	var internalID string
	err = s.pool.QueryRow(r.Context(), "SELECT clinic_id::text FROM clinic_publications WHERE public_id=$1 AND workspace_id=$2", p.ClinicID, doctor.workspace).Scan(&internalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return publicMissing()
	}
	if err != nil {
		return err
	}
	a := &publicAdmission{doctorID: doctor.ID, clinicID: p.ClinicID, network: r.Header.Get("X-Public-Client"), request: p, doctor: doctor}
	logical := p
	logical.Challenge = ""
	logical.Solution = ""
	raw, _ := json.Marshal(logical)
	sum := sha256.Sum256(append([]byte("public-create\n"+doctor.ID+"\n"), raw...))
	request := bookingRequest{ClinicID: internalID, Start: p.Start, ContactName: p.ContactName, ContactPhone: p.ContactPhone, AttendeeName: &p.AttendeeName, ContextID: p.ContextID}
	return s.applyBooking(w, r, doctor.owner, "create", Booking{}, request, "public:"+key, hex.EncodeToString(sum[:]), a)
}
func (s *Service) publicAvailability(w http.ResponseWriter, r *http.Request) error {
	d, err := s.readPublicDoctor(r.Context(), s.pool, r.PathValue("publicId"))
	if err != nil {
		return err
	}
	if !d.published {
		return publicMissing()
	}
	snap, err := s.onlineRights(r.Context(), d.owner)
	if err != nil {
		return err
	}
	if !snap.AllowsTeleconsultation() {
		filterPublicOffers(&d, inPerson, "", "", "", "")
	}
	var id string
	err = s.pool.QueryRow(r.Context(), "SELECT clinic_id::text FROM clinic_publications WHERE public_id=$1 AND workspace_id=$2 AND enabled", r.PathValue("publicClinicId"), d.workspace).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return publicMissing()
	}
	if err != nil {
		return err
	}
	r.SetPathValue("id", id)
	selected, err := choosePublicContext(d, r.URL.Query().Get("specialtyContextId"))
	if err != nil {
		return err
	}
	if !publicContextClinic(d, selected, r.PathValue("publicClinicId")) {
		return publicMissing()
	}
	return s.availabilityFor(w, r, d.owner, &publicAdmission{doctorID: d.ID, clinicID: r.PathValue("publicClinicId"), doctor: d, contextID: selected})
}
func (s *Service) publicBookingDetails(w http.ResponseWriter, r *http.Request, user string) error {
	b, err := ownedBooking(r.Context(), s.pool, user, r.PathValue("id"))
	if err != nil {
		return err
	}
	var doc []byte
	err = s.pool.QueryRow(r.Context(), "SELECT jsonb_build_object('reference',reference::text,'formVersion',form_version::text,'form',form_snapshot,'answers',answers,'documentCountry',document_country,'documentType',document_type,'document',document,'email',email) FROM booking_public_details WHERE booking_id=$1", b.ID).Scan(&doc)
	if errors.Is(err, pgx.ErrNoRows) {
		api.WriteJSON(w, 200, nil)
		return nil
	}
	if err != nil {
		return err
	}
	api.WriteJSON(w, 200, json.RawMessage(doc))
	return nil
}
