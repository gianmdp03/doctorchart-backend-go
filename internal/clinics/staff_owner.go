package clinics

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
)

type staffAccess struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Active      bool     `json:"active"`
	Activated   bool     `json:"activated"`
	Permissions []string `json:"permissions"`
	ClinicIDs   []string `json:"clinicIds"`
}
type staffAccessRequest struct {
	Email           string   `json:"email"`
	Name            string   `json:"name"`
	ExpectedVersion string   `json:"expectedVersion"`
	Permissions     []string `json:"permissions"`
	ClinicIDs       []string `json:"clinicIds"`
}

func validStaffPermissions(permissions []string) bool {
	if len(permissions) < 1 || len(permissions) > 5 {
		return false
	}
	seen := map[string]bool{}
	for _, p := range permissions {
		if seen[p] {
			return false
		}
		switch p {
		case "view", "create", "cancel", "reschedule", "status":
		default:
			return false
		}
		seen[p] = true
	}
	return seen["view"]
}
func staffRight(snapshot billing.Snapshot) error {
	if snapshot.SecretaryManagement == nil || !*snapshot.SecretaryManagement || !snapshot.ClinicManagement {
		return api.Failure(403, "SECRETARY_MANAGEMENT_DISABLED", "La cobertura del médico no habilita secretaría.")
	}
	return nil
}
func (s *Service) staffOwnerRoutes(mux *http.ServeMux, wrap func(func(http.ResponseWriter, *http.Request, string) error) http.HandlerFunc) {
	mux.HandleFunc("GET /api/doctorchart/secretaries", wrap(s.staffOwnerList))
	mux.HandleFunc("POST /api/doctorchart/secretaries/invitations", wrap(func(w http.ResponseWriter, r *http.Request, u string) error {
		return s.staffOwnerWrite(w, r, u, "invite")
	}))
	mux.HandleFunc("PUT /api/doctorchart/secretaries/{id}", wrap(func(w http.ResponseWriter, r *http.Request, u string) error {
		return s.staffOwnerWrite(w, r, u, "edit")
	}))
	mux.HandleFunc("DELETE /api/doctorchart/secretaries/{id}", wrap(func(w http.ResponseWriter, r *http.Request, u string) error {
		return s.staffOwnerWrite(w, r, u, "revoke")
	}))
}
func readStaffAccess(ctx context.Context, q scheduleReader, workspace int64) ([]staffAccess, error) {
	rows, err := q.Query(ctx, `SELECT a.id::text,a.email,a.name,a.version::text,a.active,a.account_id IS NOT NULL,a.permissions,COALESCE(array_agg(c.clinic_id::text ORDER BY c.clinic_id) FILTER(WHERE c.clinic_id IS NOT NULL),ARRAY[]::text[]) FROM staff_access a LEFT JOIN staff_access_clinics c ON c.access_id=a.id WHERE a.workspace_id=$1 GROUP BY a.id ORDER BY a.email`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []staffAccess{}
	for rows.Next() {
		var a staffAccess
		if err = rows.Scan(&a.ID, &a.Email, &a.Name, &a.Version, &a.Active, &a.Activated, &a.Permissions, &a.ClinicIDs); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}
func (s *Service) staffOwnerList(w http.ResponseWriter, r *http.Request, user string) error {
	workspace, err := s.workspace(r.Context(), user)
	if err != nil {
		return err
	}
	items, err := readStaffAccess(r.Context(), s.pool, workspace)
	if err != nil {
		return err
	}
	capabilities := map[string]any{"manage": false, "reason": "ENTITLEMENTS_UNAVAILABLE", "validUntil": nil}
	snapshot, e := s.staffRights.Resolve(r.Context(), user)
	if e == nil {
		capabilities["validUntil"] = snapshot.ValidUntil
		if staffRight(snapshot) == nil {
			capabilities["manage"] = true
			capabilities["reason"] = nil
		} else {
			capabilities["reason"] = "SECRETARY_MANAGEMENT_DISABLED"
		}
	}
	api.WriteJSON(w, 200, map[string]any{"items": items, "capabilities": capabilities})
	return nil
}
func (s *Service) staffOwnerWrite(w http.ResponseWriter, r *http.Request, user, op string) error {
	workspace, err := s.workspace(r.Context(), user)
	if err != nil {
		return err
	}
	var req staffAccessRequest
	var snapshot billing.Snapshot
	if op != "revoke" {
		if err = decodePublic(w, r, &req); err != nil {
			return err
		}
		if !validStaffPermissions(req.Permissions) || len(req.ClinicIDs) < 1 || len(req.ClinicIDs) > 100 {
			return api.Invalid("Seleccioná consultorios y permisos válidos.")
		}
		snapshot, err = s.staffRights.Resolve(r.Context(), user)
		if err != nil {
			return unavailable()
		}
		if err = staffRight(snapshot); err != nil {
			return err
		}
		if op == "invite" {
			req.Email, err = staffEmail(req.Email)
			if err != nil {
				return err
			}
			req.Name = strings.TrimSpace(req.Name)
			if len([]rune(req.Name)) < 1 || len([]rune(req.Name)) > 160 {
				return api.Invalid("Revisá el nombre.")
			}
		}
	}
	if op != "invite" && !opaqueID.MatchString(r.PathValue("id")) {
		return staffForbidden()
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if op != "revoke" {
		if err = s.staffRights.Fence(r.Context(), tx, snapshot); err != nil {
			return unavailable()
		}
	}
	if _, err = tx.Exec(r.Context(), "SELECT id FROM doctor_workspaces WHERE id=$1 FOR UPDATE", workspace); err != nil {
		return err
	}
	id := r.PathValue("id")
	var version string
	if op == "invite" {
		id = newOpaqueID()

		var existing string
		var activated bool
		err = tx.QueryRow(r.Context(), "SELECT id::text,account_id IS NOT NULL FROM staff_access WHERE workspace_id=$1 AND email=$2", workspace, req.Email).Scan(&existing, &activated)
		if err == nil {
			if activated {
				return api.Failure(409, "STAFF_ACCESS_EXISTS", "La relación ya está activada. Editala desde la lista.")
			}
			id = existing
			if _, err = tx.Exec(r.Context(), "UPDATE staff_invitations SET consumed_at=COALESCE(consumed_at,clock_timestamp()) WHERE access_id=$1", id); err != nil {
				return err
			}
			_, err = tx.Exec(r.Context(), "UPDATE staff_access SET name=$1,permissions=$2,active=true,version=version+1 WHERE id=$3", req.Name, req.Permissions, id)
		} else if errors.Is(err, pgx.ErrNoRows) {
			_, err = tx.Exec(r.Context(), "INSERT INTO staff_access(id,workspace_id,email,name,permissions) VALUES($1,$2,$3,$4,$5)", id, workspace, req.Email, req.Name, req.Permissions)
		} else {
			return err
		}

	} else {
		err = tx.QueryRow(r.Context(), "SELECT version::text FROM staff_access WHERE id=$1 AND workspace_id=$2 FOR UPDATE", id, workspace).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return staffForbidden()
		}
		if err != nil {
			return err
		}
		if op == "edit" {
			if version != req.ExpectedVersion {
				return api.Failure(409, "VERSION_CONFLICT", "La asignación cambió. Recargá la lista.")
			}
			_, err = tx.Exec(r.Context(), "UPDATE staff_access SET permissions=$1,active=true,version=version+1 WHERE id=$2", req.Permissions, id)
		} else {
			_, err = tx.Exec(r.Context(), "UPDATE staff_access SET active=false,version=version+1 WHERE id=$1", id)
		}
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), "DELETE FROM staff_access_clinics WHERE access_id=$1", id); err != nil {
		return err
	}
	if op != "revoke" {
		seen := map[string]bool{}
		for _, clinic := range req.ClinicIDs {
			if _, err = decimal(clinic, true); err != nil {
				return err
			}
			if seen[clinic] {
				return api.Invalid("Consultorio duplicado.")
			}
			seen[clinic] = true
			var valid bool
			if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM clinics WHERE id=$1 AND workspace_id=$2 AND status<>'ARCHIVED')", clinic, workspace).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return missing()
			}
			if _, err = tx.Exec(r.Context(), "INSERT INTO staff_access_clinics(access_id,workspace_id,clinic_id) VALUES($1,$2,$3)", id, workspace, clinic); err != nil {
				return err
			}
		}
	}
	var code string
	expiry := time.Now().UTC().Add(24 * time.Hour)
	if op == "invite" {
		code = staffToken()
		_, err = tx.Exec(r.Context(), "INSERT INTO staff_invitations(token_hash,access_id,expires_at) VALUES($1,$2,$3)", staffHash(code), id, expiry)
	}
	if op == "revoke" {
		_, err = tx.Exec(r.Context(), "UPDATE staff_invitations SET consumed_at=COALESCE(consumed_at,clock_timestamp()) WHERE access_id=$1", id)
	}
	if err != nil {
		return err
	}
	if op == "invite" && s.notices != nil {
		if err = s.notices.Invitation(r.Context(), tx, id, req.Email, code, expiry); err != nil {
			return err
		}
	}
	if err = staffAudit(r.Context(), tx, "", id, "OWNER_"+strings.ToUpper(op)); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	if op == "invite" {
		api.WriteJSON(w, 201, map[string]any{"id": id, "invitationCode": code, "expiresAt": expiry})
	} else if op == "revoke" {
		w.WriteHeader(204)
	} else {
		items, e := readStaffAccess(r.Context(), s.pool, workspace)
		if e != nil {
			return e
		}
		for _, a := range items {
			if a.ID == id {
				api.WriteJSON(w, 200, a)
				break
			}
		}
	}
	return nil
}
