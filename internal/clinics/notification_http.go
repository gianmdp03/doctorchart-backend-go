package clinics

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/notifications"
)

func (s *Service) WithNotifications(n *notifications.Service) *Service { s.notices = n; return s }
func confirmationMissing() error {
	return api.Failure(404, "CONFIRMATION_NOT_FOUND", "La constancia no está disponible. Revisá el código o su vencimiento.")
}
func (s *Service) notificationRoutes(mux *http.ServeMux, wrap func(func(http.ResponseWriter, *http.Request, string) error) http.HandlerFunc) {
	if s.publicEnabled {
		mux.HandleFunc("POST /api/doctorchart/public/appointments/confirmation", func(w http.ResponseWriter, r *http.Request) {
			publicHeaders(w)
			if err := s.confirmation(w, r); err != nil {
				if failure, ok := err.(*api.Error); ok && failure.Status == 429 {
					w.Header().Set("Retry-After", "60")
				}
				api.WriteError(w, err)
			}
		})
	}
	mux.HandleFunc("DELETE /api/doctorchart/bookings/{id}/confirmation-access", wrap(func(w http.ResponseWriter, r *http.Request, owner string) error {
		b, err := ownedBooking(r.Context(), s.pool, owner, r.PathValue("id"))
		if err != nil {
			return err
		}
		if _, err = s.pool.Exec(r.Context(), "UPDATE booking_confirmation_access SET revoked_at=COALESCE(revoked_at,clock_timestamp()) WHERE booking_id=$1", b.ID); err != nil {
			return err
		}
		w.WriteHeader(204)
		return nil
	}))
}
func (s *Service) confirmation(w http.ResponseWriter, r *http.Request) error {
	if err := s.publicLimit(r, "confirmation", 30); err != nil {
		return err
	}
	if r.URL.RawQuery != "" {
		return api.Invalid("El código se envía solamente en el formulario.")
	}
	var req struct {
		Code string `json:"accessCode"`
	}
	if err := decodePublic(w, r, &req); err != nil {
		return err
	}
	if len(req.Code) != 43 {
		return confirmationMissing()
	}
	var reference, status, doctor, clinic, address, zone, specialty string
	var start, end, expires time.Time
	err := s.pool.QueryRow(r.Context(), `SELECT p.reference::text,b.status,b.start_at,b.end_at,p.doctor_name,b.clinic_snapshot->>'name',b.clinic_snapshot->>'publicAddress',b.clinic_snapshot->>'timezone',a.expires_at,b.specialty_snapshot->>'name' FROM booking_confirmation_access a JOIN bookings b ON b.id=a.booking_id JOIN booking_public_details p ON p.booking_id=b.id WHERE a.token_hash=$1 AND a.revoked_at IS NULL AND a.expires_at>clock_timestamp()`, notifications.Hash(req.Code)).Scan(&reference, &status, &start, &end, &doctor, &clinic, &address, &zone, &expires, &specialty)
	if errors.Is(err, pgx.ErrNoRows) {
		return confirmationMissing()
	}
	if err != nil {
		return err
	}
	api.WriteJSON(w, 200, map[string]any{"reference": reference, "status": status, "start": start.UTC(), "end": end.UTC(), "doctorName": doctor, "clinicName": clinic, "address": address, "timezone": zone, "accessExpiresAt": expires.UTC(), "specialtyName": specialty})
	return nil
}
func (s *Service) staffRecoveryEmail(w http.ResponseWriter, r *http.Request) error {
	if err := s.staffOriginCheck(r); err != nil {
		return err
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := decodePublic(w, r, &req); err != nil {
		return err
	}
	email, err := staffEmail(req.Email)
	if err != nil {
		return api.Invalid("Revisá el correo electrónico.")
	}
	if err = s.staffRate(r, email); err != nil {
		return err
	}
	var count int
	err = s.pool.QueryRow(r.Context(), "INSERT INTO staff_rate_windows(key,bucket,attempts) VALUES($1,$2,1) ON CONFLICT(key,bucket) DO UPDATE SET attempts=staff_rate_windows.attempts+1 RETURNING attempts", staffHash("email-recovery:"+email), time.Now().UTC().Truncate(24*time.Hour)).Scan(&count)
	if err != nil {
		return err
	}
	if count > 3 {
		return api.Failure(429, "STAFF_RATE_LIMITED", "Demasiadas solicitudes. Reintentá más tarde.")
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var account string
	err = tx.QueryRow(r.Context(), "SELECT id::text FROM staff_accounts WHERE email=$1 FOR SHARE", email).Scan(&account)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if account != "" {
		if err = s.notices.Recovery(r.Context(), tx, account, email); err != nil {
			return err
		}
		if err = staffAudit(r.Context(), tx, account, "", "EMAIL_RECOVERY_REQUESTED"); err != nil {
			return err
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	api.WriteJSON(w, 202, map[string]string{"message": "Si existe una cuenta con ese correo, recibirás un código para recuperar tu acceso."})
	return nil
}
func (s *Service) staffRecoverEmail(w http.ResponseWriter, r *http.Request) error {
	if err := s.staffOriginCheck(r); err != nil {
		return err
	}
	var req struct {
		Email    string `json:"email"`
		Code     string `json:"emailCode"`
		Password string `json:"password"`
	}
	if err := decodePublic(w, r, &req); err != nil {
		return err
	}
	email, err := staffEmail(req.Email)
	if err != nil {
		return staffCredentials()
	}
	if err = s.staffRate(r, email); err != nil {
		return err
	}
	if len(req.Code) != 43 {
		return staffCredentials()
	}
	hash, err := staffPassword(req.Password)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var account string
	err = tx.QueryRow(r.Context(), "SELECT a.id::text FROM staff_accounts a WHERE a.email=$1 AND EXISTS(SELECT 1 FROM staff_email_recovery e WHERE e.account_id=a.id AND e.token_hash=$2 AND e.consumed_at IS NULL AND e.expires_at>clock_timestamp()) FOR UPDATE OF a", email, staffHash(req.Code)).Scan(&account)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffCredentials()
	}
	if err != nil {
		return err
	}
	// READ COMMITTED must recheck after acquiring the account lock: another
	// successful recovery can have invalidated all sibling codes while we waited.
	var valid bool
	if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM staff_email_recovery WHERE account_id=$1 AND token_hash=$2 AND consumed_at IS NULL AND expires_at>clock_timestamp())", account, staffHash(req.Code)).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return staffCredentials()
	}
	recovery := staffToken()
	if _, err = tx.Exec(r.Context(), "UPDATE staff_accounts SET password_hash=$1,recovery_hash=$2 WHERE id=$3", string(hash), staffHash(recovery), account); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), "UPDATE staff_email_recovery SET consumed_at=COALESCE(consumed_at,clock_timestamp()) WHERE account_id=$1", account); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), "UPDATE staff_sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND revoked_at IS NULL", account); err != nil {
		return err
	}
	if err = staffAudit(r.Context(), tx, account, "", "ACCESS_RECOVERED_BY_EMAIL"); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	setStaffCookie(w, "", time.Unix(1, 0))
	api.WriteJSON(w, 200, map[string]string{"recoveryCode": recovery})
	return nil
}
