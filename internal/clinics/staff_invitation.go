package clinics

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

// Inspecting a link never consumes it: email scanners must not activate accounts.
func (s *Service) staffInvitation(w http.ResponseWriter, r *http.Request) error {
	if err := s.staffOriginCheck(r); err != nil {
		return err
	}
	var req struct {
		Code string `json:"invitationCode"`
	}
	if err := decodePublic(w, r, &req); err != nil {
		return err
	}
	if err := s.staffRate(r, "invitation:"+staffHash(req.Code)); err != nil {
		return err
	}
	if len(req.Code) != 43 {
		return staffCredentials()
	}
	var email, name string
	var expires time.Time
	var existing bool
	err := s.pool.QueryRow(r.Context(), `SELECT a.email,a.name,i.expires_at,EXISTS(SELECT 1 FROM staff_accounts u WHERE u.email=a.email) FROM staff_invitations i JOIN staff_access a ON a.id=i.access_id WHERE i.token_hash=$1 AND a.active AND i.consumed_at IS NULL AND i.expires_at>clock_timestamp()`, staffHash(req.Code)).Scan(&email, &name, &expires, &existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffCredentials()
	}
	if err != nil {
		return err
	}
	api.WriteJSON(w, 200, map[string]any{"email": email, "name": name, "expiresAt": expires, "existingAccount": existing})
	return nil
}
