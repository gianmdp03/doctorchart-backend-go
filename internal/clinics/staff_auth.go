package clinics

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"golang.org/x/crypto/bcrypt"
)

const staffCookie = "__Secure-dc_staff"
const staffBase = "/api/doctorchart/staff"

type staffSession struct {
	ID      string    `json:"-"`
	Email   string    `json:"email"`
	Name    string    `json:"name"`
	CSRF    string    `json:"csrfToken"`
	Expires time.Time `json:"expiresAt"`
	hash    string
}

func staffHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func staffToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func staffEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	p, err := mail.ParseAddress(email)
	if err != nil || p.Address != email || len(email) > 254 || !strings.Contains(email, ".") {
		return "", api.Invalid("Revisá el correo electrónico.")
	}
	return email, nil
}
func staffPassword(raw string) ([]byte, error) {
	if len(raw) < 12 || len(raw) > 72 {
		return nil, api.Invalid("La contraseña debe tener entre 12 y 72 bytes.")
	}
	return bcrypt.GenerateFromPassword([]byte(raw), 12)
}
func staffUnauth() error {
	return api.Failure(401, "STAFF_UNAUTHENTICATED", "La sesión de secretaría venció. Iniciá sesión nuevamente.")
}
func staffCredentials() error {
	return api.Failure(401, "INVALID_CREDENTIALS", "No se pudieron verificar los datos de acceso.")
}
func staffForbidden() error {
	return api.Failure(403, "STAFF_FORBIDDEN", "No tenés permiso para esta operación.")
}

func (s *Service) WithStaff(enabled bool, origin string) *Service {
	s.staffEnabled = enabled && s.bookingsEnabled && s.schedulesEnabled
	s.staffOrigin = origin
	if s.staffEnabled {
		s.staffRights = s.rights.Secretaries()
	}
	return s
}
func (s *Service) staffOriginCheck(r *http.Request) error {
	if s.staffOrigin == "" || r.Header.Get("Origin") != s.staffOrigin {
		return api.Failure(403, "CSRF_REJECTED", "El origen de la solicitud no está autorizado.")
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return api.Failure(403, "CSRF_REJECTED", "La solicitud no está autorizada.")
	}
	return nil
}
func (s *Service) staffSession(r *http.Request, mutate bool) (staffSession, error) {
	var session staffSession
	cookies := r.CookiesNamed(staffCookie)
	if len(cookies) != 1 || len(cookies[0].Value) != 43 {
		return session, staffUnauth()
	}
	token := cookies[0].Value
	session.hash = staffHash(token)
	err := s.pool.QueryRow(r.Context(), "SELECT a.id::text,a.email,a.name,t.expires_at FROM staff_sessions t JOIN staff_accounts a ON a.id=t.account_id WHERE t.token_hash=$1 AND t.revoked_at IS NULL AND t.expires_at>clock_timestamp()", session.hash).Scan(&session.ID, &session.Email, &session.Name, &session.Expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return session, staffUnauth()
	}
	if err != nil {
		return session, err
	}
	session.CSRF = staffHash(token + ":csrf")
	if mutate {
		if err = s.staffOriginCheck(r); err != nil {
			return session, err
		}
		if len(r.Header.Values("X-CSRF-Token")) != 1 || subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
			return session, api.Failure(403, "CSRF_REJECTED", "La sesión requiere una nueva verificación.")
		}
	}
	return session, nil
}
func issueStaffSession(ctx context.Context, tx pgx.Tx, id string) (string, time.Time, error) {
	token := staffToken()
	expiry := time.Now().UTC().Add(8 * time.Hour)
	_, err := tx.Exec(ctx, "INSERT INTO staff_sessions(token_hash,account_id,expires_at) VALUES($1,$2,$3)", staffHash(token), id, expiry)
	return token, expiry, err
}
func setStaffCookie(w http.ResponseWriter, token string, expiry time.Time) {
	max := int(time.Until(expiry).Seconds())
	if token == "" {
		max = -1
	}
	http.SetCookie(w, &http.Cookie{Name: staffCookie, Value: token, Path: staffBase, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expiry, MaxAge: max})
}
func staffAudit(ctx context.Context, tx pgx.Tx, account, access, kind string) error {
	_, err := tx.Exec(ctx, "INSERT INTO staff_security_events(account_id,access_id,kind) VALUES(NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,$3)", account, access, kind)
	return err
}
func (s *Service) staffRate(r *http.Request, email string) error {
	network := r.Header.Get("X-Public-Client")
	if !networkID.MatchString(network) {
		return staffForbidden()
	}
	window := time.Now().UTC().Truncate(15 * time.Minute)
	for _, limit := range []struct {
		key string
		max int
	}{{staffHash("network:" + network), 60}, {staffHash("account:" + email), 10}} {
		var count int
		if err := s.pool.QueryRow(r.Context(), "INSERT INTO staff_rate_windows(key,bucket,attempts) VALUES($1,$2,1) ON CONFLICT(key,bucket) DO UPDATE SET attempts=staff_rate_windows.attempts+1 RETURNING attempts", limit.key, window).Scan(&count); err != nil {
			return err
		}
		if count > limit.max {
			return api.Failure(429, "STAFF_RATE_LIMITED", "Demasiados intentos. Reintentá más tarde.")
		}
	}
	_, err := s.pool.Exec(r.Context(), "DELETE FROM staff_rate_windows WHERE bucket<$1", window.Add(-24*time.Hour))
	return err
}

func (s *Service) staffAuthRoutes(mux *http.ServeMux) {
	wrap := func(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store, private")
			w.Header().Set("Referrer-Policy", "no-referrer")
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
			if err := fn(w, r); err != nil {
				if failure, ok := err.(*api.Error); ok && (failure.Status == 401 || failure.Status == 403) {
					_, _ = s.pool.Exec(r.Context(), "INSERT INTO staff_security_events(kind) VALUES('AUTH_REJECTED')")
				}
				if e, ok := err.(*api.Error); ok && e.Status == 429 {
					w.Header().Set("Retry-After", "900")
				}
				api.WriteError(w, err)
			}
		}
	}
	mux.HandleFunc("POST "+staffBase+"/login", wrap(s.staffLogin))
	mux.HandleFunc("POST "+staffBase+"/activate", wrap(s.staffActivate))
	mux.HandleFunc("POST "+staffBase+"/recover", wrap(s.staffRecover))
	mux.HandleFunc("GET "+staffBase+"/session", wrap(func(w http.ResponseWriter, r *http.Request) error {
		session, err := s.staffSession(r, false)
		if err != nil {
			return err
		}
		api.WriteJSON(w, 200, session)
		return nil
	}))
	mux.HandleFunc("POST "+staffBase+"/logout", wrap(func(w http.ResponseWriter, r *http.Request) error {
		session, err := s.staffSession(r, true)
		if err != nil {
			return err
		}
		tx, err := s.pool.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(r.Context(), "SELECT id FROM staff_accounts WHERE id=$1 FOR UPDATE", session.ID); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "UPDATE staff_sessions SET revoked_at=clock_timestamp() WHERE token_hash=$1", session.hash); err != nil {
			return err
		}
		if err = staffAudit(r.Context(), tx, session.ID, "", "LOGOUT"); err != nil {
			return err
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
		setStaffCookie(w, "", time.Unix(1, 0))
		w.WriteHeader(204)
		return nil
	}))
	mux.HandleFunc("POST "+staffBase+"/password", wrap(s.staffChangePassword))
	if s.notices != nil {
		mux.HandleFunc("POST "+staffBase+"/recovery-email", wrap(s.staffRecoveryEmail))
		mux.HandleFunc("POST "+staffBase+"/recover-email", wrap(s.staffRecoverEmail))
	}
	s.staffDelegatedRoutes(mux, wrap)
}

// The dummy hash equalizes the expensive part of unknown-account login.
var staffDummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-account-password"), 12)

func (s *Service) staffLogin(w http.ResponseWriter, r *http.Request) error {
	if err := s.staffOriginCheck(r); err != nil {
		return err
	}
	var req struct {
		Email    string `json:"email"`
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
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var id, name, hash string
	err = tx.QueryRow(r.Context(), "SELECT id::text,name,password_hash FROM staff_accounts WHERE email=$1 FOR UPDATE", email).Scan(&id, &name, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		hash = string(staffDummyHash)
	} else if err != nil {
		return err
	}
	valid := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) == nil && id != ""
	if !valid {
		if err = staffAudit(r.Context(), tx, id, "", "LOGIN_REJECTED"); err != nil {
			return err
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
		return staffCredentials()
	}
	token, expires, err := issueStaffSession(r.Context(), tx, id)
	if err != nil {
		return err
	}
	if err = staffAudit(r.Context(), tx, id, "", "LOGIN"); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	setStaffCookie(w, token, expires)
	api.WriteJSON(w, 200, staffSession{Email: email, Name: name, CSRF: staffHash(token + ":csrf"), Expires: expires})
	return nil
}

func (s *Service) staffActivate(w http.ResponseWriter, r *http.Request) error {
	if err := s.staffOriginCheck(r); err != nil {
		return err
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"invitationCode"`
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
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Serialize with physician revocation using workspace first, invitation second.
	var workspace int64
	err = tx.QueryRow(r.Context(), "SELECT a.workspace_id FROM staff_invitations i JOIN staff_access a ON a.id=i.access_id WHERE token_hash=$1", staffHash(req.Code)).Scan(&workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffCredentials()
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), "SELECT id FROM doctor_workspaces WHERE id=$1 FOR UPDATE", workspace); err != nil {
		return err
	}
	var access, name string
	err = tx.QueryRow(r.Context(), "SELECT a.id::text,a.name FROM staff_invitations i JOIN staff_access a ON a.id=i.access_id WHERE i.token_hash=$1 AND a.email=$2 AND a.active AND i.consumed_at IS NULL AND i.expires_at>clock_timestamp() FOR UPDATE OF i,a", staffHash(req.Code), email).Scan(&access, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffCredentials()
	}
	if err != nil {
		return err
	}
	// Advisory lock orders concurrent first activation of the same email across doctors.
	if _, err = tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(17212,hashtext($1))", email); err != nil {
		return err
	}
	var id, stored string
	recovery := ""
	err = tx.QueryRow(r.Context(), "SELECT id::text,password_hash FROM staff_accounts WHERE email=$1 FOR UPDATE", email).Scan(&id, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		hash, e := staffPassword(req.Password)
		if e != nil {
			return e
		}
		id = newOpaqueID()
		recovery = staffToken()
		_, err = tx.Exec(r.Context(), "INSERT INTO staff_accounts(id,email,name,password_hash,recovery_hash) VALUES($1,$2,$3,$4,$5)", id, email, name, string(hash), staffHash(recovery))
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if bcrypt.CompareHashAndPassword([]byte(stored), []byte(req.Password)) != nil {
		return staffCredentials()
	}
	if _, err = tx.Exec(r.Context(), "UPDATE staff_access SET account_id=$1,version=version+1 WHERE id=$2", id, access); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), "UPDATE staff_invitations SET consumed_at=clock_timestamp() WHERE token_hash=$1", staffHash(req.Code)); err != nil {
		return err
	}
	token, expires, err := issueStaffSession(r.Context(), tx, id)
	if err != nil {
		return err
	}
	if err = staffAudit(r.Context(), tx, id, access, "ACTIVATED"); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	setStaffCookie(w, token, expires)
	api.WriteJSON(w, 200, map[string]any{"email": email, "name": name, "csrfToken": staffHash(token + ":csrf"), "expiresAt": expires, "recoveryCode": recovery})
	return nil
}
func (s *Service) staffRecover(w http.ResponseWriter, r *http.Request) error {
	if err := s.staffOriginCheck(r); err != nil {
		return err
	}
	var req struct {
		Email    string `json:"email"`
		Code     string `json:"recoveryCode"`
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
	var id string
	err = tx.QueryRow(r.Context(), "SELECT id::text FROM staff_accounts WHERE email=$1 AND recovery_hash=$2 FOR UPDATE", email, staffHash(req.Code)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffCredentials()
	}
	if err != nil {
		return err
	}
	recovery := staffToken()
	if s.notices != nil {
		if _, err = tx.Exec(r.Context(), "UPDATE staff_email_recovery SET consumed_at=COALESCE(consumed_at,clock_timestamp()) WHERE account_id=$1", id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(r.Context(), "UPDATE staff_accounts SET password_hash=$1,recovery_hash=$2 WHERE id=$3", string(hash), staffHash(recovery), id); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), "UPDATE staff_sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND revoked_at IS NULL", id); err != nil {
		return err
	}
	if err = staffAudit(r.Context(), tx, id, "", "ACCESS_RECOVERED"); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	setStaffCookie(w, "", time.Unix(1, 0))
	api.WriteJSON(w, 200, map[string]string{"recoveryCode": recovery})
	return nil
}
func (s *Service) staffChangePassword(w http.ResponseWriter, r *http.Request) error {
	session, err := s.staffSession(r, true)
	if err != nil {
		return err
	}
	var req struct {
		Current  string `json:"currentPassword"`
		Password string `json:"password"`
	}
	if err = decodePublic(w, r, &req); err != nil {
		return err
	}
	if err = s.staffRate(r, session.Email); err != nil {
		return err
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
	var stored string
	err = tx.QueryRow(r.Context(), "SELECT password_hash FROM staff_accounts WHERE id=$1 FOR UPDATE", session.ID).Scan(&stored)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(stored), []byte(req.Current)) != nil {
		return staffCredentials()
	}
	// Revalidate after the account lock to prevent a concurrent recovery being bypassed.
	var valid bool
	if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM staff_sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp())", session.hash).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return staffUnauth()
	}
	if s.notices != nil {
		if _, err = tx.Exec(r.Context(), "UPDATE staff_email_recovery SET consumed_at=COALESCE(consumed_at,clock_timestamp()) WHERE account_id=$1", session.ID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(r.Context(), "UPDATE staff_accounts SET password_hash=$1 WHERE id=$2", string(hash), session.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), "UPDATE staff_sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND token_hash<>$2 AND revoked_at IS NULL", session.ID, session.hash); err != nil {
		return err
	}
	if err = staffAudit(r.Context(), tx, session.ID, "", "PASSWORD_CHANGED"); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	w.WriteHeader(204)
	return nil
}
