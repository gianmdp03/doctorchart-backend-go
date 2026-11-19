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
	var email, name, doctorName string
	var expires time.Time
	var existing bool
	var agendas []string
	err := s.pool.QueryRow(r.Context(), `
		SELECT a.email, a.name, i.expires_at,
		       EXISTS (SELECT 1 FROM staff_accounts u WHERE u.email = a.email),
		       COALESCE(p.name, ''),
		       ARRAY(
		           SELECT CASE WHEN c.modality = 'TELECONSULTATION' THEN c.name || ' · Teleconsulta' ELSE c.name END
		           FROM staff_access_clinics x
		           JOIN clinics c ON c.id = x.clinic_id AND c.workspace_id = a.workspace_id
		           WHERE x.access_id = a.id AND c.status <> 'ARCHIVED'
		           ORDER BY c.name, c.id
		       )
		FROM staff_invitations i
		JOIN staff_access a ON a.id = i.access_id
		LEFT JOIN booking_publications p ON p.workspace_id = a.workspace_id
		WHERE i.token_hash = $1 AND a.active AND i.consumed_at IS NULL AND i.expires_at > clock_timestamp()
	`, staffHash(req.Code)).Scan(&email, &name, &expires, &existing, &doctorName, &agendas)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffCredentials()
	}
	if err != nil {
		return err
	}
	api.WriteJSON(w, 200, map[string]any{
		"email": email, "name": name, "expiresAt": expires, "existingAccount": existing,
		"doctorName": doctorName, "agendas": agendas,
	})
	return nil
}
