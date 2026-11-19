package clinics

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type bookingPortalIntake struct {
	Document    string            `json:"document"`
	Email       string            `json:"email"`
	FormVersion string            `json:"formVersion"`
	Answers     map[string]string `json:"answers"`
}

func (p *bookingPortalIntake) validate() error {
	p.Document = strings.TrimSpace(p.Document)
	p.Email = strings.TrimSpace(p.Email)
	if err := validatePublicIdentity("AR", "DNI", p.Document, p.Email); err != nil {
		return err
	}
	if _, err := decimal(p.FormVersion, false); err != nil {
		return field("formVersion", "INVALID_VERSION")
	}
	return validatePublicAnswers(p.Answers)
}

func readBookingForm(ctx context.Context, q scheduleReader, workspace int64) (PublicForm, string, error) {
	form := PublicForm{Version: "0", Questions: []PublicQuestion{}}
	var name string
	var questions []byte
	err := q.QueryRow(ctx, "SELECT version::text, name, questions FROM booking_publications WHERE workspace_id=$1", workspace).
		Scan(&form.Version, &name, &questions)
	if errors.Is(err, pgx.ErrNoRows) {
		return form, "Profesional de DoctorChart", nil
	}
	if err != nil {
		return form, "", err
	}
	if err := json.Unmarshal(questions, &form.Questions); err != nil {
		return form, "", err
	}
	return form, name, nil
}

func (s *Service) getBookingForm(w http.ResponseWriter, r *http.Request, user string) error {
	return s.writeBookingForm(w, r, s.pool, user)
}

func (s *Service) getStaffBookingForm(w http.ResponseWriter, r *http.Request) error {
	admission, err := s.staffAdmission(r, "create")
	if err != nil {
		return err
	}
	admission.clinic = r.PathValue("clinicId")
	tx, err := s.staffReadTx(r, admission)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	return s.writeBookingForm(w, r, tx, admission.owner)
}

func (s *Service) writeBookingForm(w http.ResponseWriter, r *http.Request, q scheduleReader, user string) error {
	ctx := r.Context()
	var workspace int64
	err := q.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1", user).Scan(&workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.Failure(409, "ONBOARDING_REQUIRED", "Completá la configuración del médico.")
	}
	if err != nil {
		return err
	}
	form, _, err := readBookingForm(ctx, q, workspace)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	api.WriteJSON(w, http.StatusOK, form)
	return nil
}

func recordManualBooking(ctx context.Context, tx pgx.Tx, workspace int64, b Booking, intake *bookingPortalIntake) error {
	form, name, err := readBookingForm(ctx, tx, workspace)
	if err != nil {
		return err
	}
	if err := checkPublicForm(form, intake.FormVersion, intake.Answers); err != nil {
		return err
	}
	if form.Version == "0" {
		form.Version = "1"
	}
	rawForm, err := json.Marshal(form)
	if err != nil {
		return err
	}
	answers := intake.Answers
	if answers == nil {
		answers = map[string]string{}
	}
	rawAnswers, err := json.Marshal(answers)
	if err != nil {
		return err
	}
	reference := newOpaqueID()
	address := b.ClinicSnapshot.PublicAddress
	if address == "" {
		address = b.ClinicSnapshot.Address
	}
	// Keep authenticated creations in a separate hash domain from public proofs.
	replayGuard := fmt.Sprintf("%x", sha256.Sum256([]byte("manual-booking:"+reference)))
	_, err = tx.Exec(ctx, `INSERT INTO booking_public_details
	 (booking_id,reference,form_version,form_snapshot,answers,document_country,document_type,document,email,challenge_hash,doctor_name,clinic_name,address,timezone)
	 VALUES ($1,$2,$3,$4,$5,'AR','DNI',$6,$7,$8,$9,$10,$11,$12)`,
		b.ID, reference, form.Version, rawForm, rawAnswers, intake.Document, intake.Email, replayGuard,
		name, b.ClinicSnapshot.Name, address, b.ClinicSnapshot.Timezone)
	return err
}
