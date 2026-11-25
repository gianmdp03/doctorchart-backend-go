package clinics

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/notifications"
)

func (s *Service) WithNotifications(n *notifications.Service) *Service { s.notices = n; return s }
func (s *Service) bookingPortalRoutes(mux *http.ServeMux) {
	const prefix = "POST /api/doctorchart/public/appointments/"
	register := func(path, kind string, limit int, fn func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(prefix+path, s.publicWrap(kind, limit, func(w http.ResponseWriter, r *http.Request) error {
			if r.URL.RawQuery != "" {
				return api.Invalid("Ingresá los datos solamente en el formulario.")
			}
			if origin := s.notices.PublicOrigin(); origin != "" && r.Header.Get("Origin") != origin {
				return api.Failure(403, "INVALID_PUBLIC_CONTEXT", "Abrí Mis turnos desde DoctorChart.")
			}
			return fn(w, r)
		}))
	}
	register("access/request", "portal-request", 6, s.requestPortalCode)
	register("access/verify", "portal-verify", 12, s.verifyPortalCode)
	register("my-bookings", "portal-list", 30, s.portalBookings)
	register("my-bookings/change", "portal-change", 10, s.changePortalBooking)
	register("my-bookings/logout", "portal-logout", 10, s.closePortal)
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
	err = tx.QueryRow(r.Context(), "SELECT id::text FROM staff_accounts WHERE email=$1 FOR UPDATE", email).Scan(&account)
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

var staffEmailCode = regexp.MustCompile(`^[0-9]{6}$`)

func (s *Service) staffRecoverEmail(w http.ResponseWriter, r *http.Request) error {
	if err := s.staffOriginCheck(r); err != nil {
		return err
	}
	var req struct {
		Email      string `json:"email"`
		Code       string `json:"emailCode"`
		ResetToken string `json:"resetToken"`
		Password   string `json:"password"`
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
	verifying := req.ResetToken == ""
	var hash []byte
	if verifying {
		if !staffEmailCode.MatchString(req.Code) || req.Password != "" {
			return staffCredentials()
		}
	} else {
		if req.Code != "" || len(req.ResetToken) != 43 {
			return staffCredentials()
		}
		hash, err = staffPassword(req.Password)
		if err != nil {
			return err
		}
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var account string
	err = tx.QueryRow(r.Context(), "SELECT id::text FROM staff_accounts WHERE email=$1 FOR UPDATE", email).Scan(&account)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffCredentials()
	}
	if err != nil {
		return err
	}
	// Read recovery state after taking the same account lock as issuance.
	if verifying {
		var id, stored string
		var expires time.Time
		err = tx.QueryRow(r.Context(), `SELECT id::text,token_hash,expires_at FROM staff_email_recovery
			WHERE account_id=$1 AND consumed_at IS NULL AND expires_at>clock_timestamp()
			ORDER BY expires_at DESC LIMIT 1`, account).Scan(&id, &stored, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return staffCredentials()
		}
		if err != nil {
			return err
		}
		if !s.notices.MatchesRecoveryCode(id, req.Code, stored) {
			return staffCredentials()
		}
		token := staffToken()
		if _, err = tx.Exec(r.Context(), "UPDATE staff_email_recovery SET consumed_at=clock_timestamp() WHERE account_id=$1 AND consumed_at IS NULL", account); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "INSERT INTO staff_email_recovery(id,account_id,token_hash,expires_at) VALUES($1,$2,$3,$4)",
			newOpaqueID(), account, staffHash("reset:"+token), expires); err != nil {
			return err
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
		api.WriteJSON(w, 200, map[string]string{"resetToken": token})
		return nil
	}
	var valid bool
	if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM staff_email_recovery WHERE account_id=$1 AND token_hash=$2 AND consumed_at IS NULL AND expires_at>clock_timestamp())", account, staffHash("reset:"+req.ResetToken)).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return staffCredentials()
	}
	if _, err = tx.Exec(r.Context(), "UPDATE staff_accounts SET password_hash=$1,recovery_hash=$2 WHERE id=$3", string(hash), staffHash(staffToken()), account); err != nil {
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
	w.WriteHeader(http.StatusNoContent)
	return nil
}
