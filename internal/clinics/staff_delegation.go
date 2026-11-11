package clinics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
)

type staffKey struct{}
type staffAdmission struct {
	validUntil                       time.Time
	session                          staffSession
	owner, access, operation, clinic string
}

func staffFrom(ctx context.Context) *staffAdmission {
	a, _ := ctx.Value(staffKey{}).(*staffAdmission)
	return a
}

// Runs under the M4 workspace lock, after the commercial fence; permission
// removal and booking mutations therefore cannot pass one another unnoticed.
func (a *staffAdmission) check(ctx context.Context, tx pgx.Tx, workspace int64, snapshot billing.Snapshot, previous Booking, request bookingRequest) error {
	if err := staffRight(snapshot); err != nil {
		return err
	}
	var valid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM staff_sessions WHERE token_hash=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp())`, a.session.hash, a.session.ID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return staffUnauth()
	}
	// Hold the account row so recovery/password change cannot revoke a session
	// concurrently between its check and committing this operation.
	if _, err := tx.Exec(ctx, "SELECT id FROM staff_accounts WHERE id=$1 FOR SHARE", a.session.ID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM staff_sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp())`, a.session.hash).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return staffUnauth()
	}
	var permissions []string
	err := tx.QueryRow(ctx, "SELECT permissions FROM staff_access WHERE id=$1 AND workspace_id=$2 AND account_id=$3 AND active", a.access, workspace, a.session.ID).Scan(&permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return staffForbidden()
	}
	if err != nil {
		return err
	}
	allowed := false
	for _, p := range permissions {
		if p == a.operation {
			allowed = true
		}
	}
	if !allowed {
		return staffForbidden()
	}
	clinics := []string{}
	if a.clinic != "" {
		clinics = append(clinics, a.clinic)
	}
	if previous.ID != "" {
		clinics = append(clinics, previous.ClinicID)
	}
	if request.ClinicID != "" {
		clinics = append(clinics, request.ClinicID)
	}
	for _, clinic := range clinics {
		if _, err = decimal(clinic, true); err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM staff_access_clinics a JOIN clinics c ON c.id=a.clinic_id WHERE a.access_id=$1 AND a.clinic_id=$2 AND c.workspace_id=$3 AND c.status<>'ARCHIVED')", a.access, clinic, workspace).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return missing()
		}
	}
	if request.PatientID != nil {
		return staffForbidden()
	}
	return nil
}
func staffBookingDocument(raw json.RawMessage) (map[string]any, error) {
	var b map[string]any
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	delete(b, "patientId")
	delete(b, "provenance")
	if clinic, ok := b["clinicSnapshot"].(map[string]any); ok {
		delete(clinic, "address")
		delete(clinic, "publicAddress")
	}
	return b, nil
}
func (s *Service) staffReadTx(r *http.Request, a *staffAdmission) (pgx.Tx, error) {
	snapshot, err := s.staffRights.Resolve(r.Context(), a.owner)
	if err != nil {
		return nil, unavailable()
	}
	a.validUntil = snapshot.ValidUntil
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return nil, err
	}
	fail := func(err error) (pgx.Tx, error) { _ = tx.Rollback(context.Background()); return nil, err }
	if err = s.staffRights.Fence(r.Context(), tx, snapshot); err != nil {
		return fail(unavailable())
	}
	var workspace int64
	if err = tx.QueryRow(r.Context(), "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR SHARE", a.owner).Scan(&workspace); err != nil {
		return fail(err)
	}
	if err = a.check(r.Context(), tx, workspace, snapshot, Booking{}, bookingRequest{}); err != nil {
		return fail(err)
	}
	return tx, nil
}
func (s *Service) staffDelegatedRoutes(mux *http.ServeMux, wrap func(func(http.ResponseWriter, *http.Request) error) http.HandlerFunc) {
	mux.HandleFunc("GET "+staffBase+"/assignments", wrap(s.staffAssignments))
	base := staffBase + "/owners/{ownerId}"
	mux.HandleFunc("GET "+base+"/clinics/{clinicId}/bookings", wrap(s.staffBookings))
	mux.HandleFunc("GET "+base+"/clinics/{clinicId}/availability", wrap(func(w http.ResponseWriter, r *http.Request) error {
		a, err := s.staffAdmission(r, "view")
		if err != nil {
			return err
		}
		a.clinic = r.PathValue("clinicId")
		r.SetPathValue("id", a.clinic)
		// availabilityFor performs the authorization again inside its read lock.
		return s.availability(w, r.WithContext(context.WithValue(r.Context(), staffKey{}, a)), a.owner)
	}))
	for _, route := range []struct{ pattern, op string }{{"POST " + base + "/bookings", "create"}, {"PATCH " + base + "/bookings/{id}/status", "status"}, {"POST " + base + "/bookings/{id}/reschedule", "reschedule"}} {
		mux.HandleFunc(route.pattern, wrap(func(w http.ResponseWriter, r *http.Request) error { return s.staffMutate(w, r, route.op) }))
	}
}
func (s *Service) staffAdmission(r *http.Request, op string) (*staffAdmission, error) {
	session, err := s.staffSession(r, r.Method != "GET")
	if err != nil {
		return nil, err
	}
	owner := r.PathValue("ownerId")
	if owner == "" || len(owner) > 100 {
		return nil, staffForbidden()
	}
	var access string
	err = s.pool.QueryRow(r.Context(), "SELECT a.id::text FROM staff_access a JOIN doctor_workspaces w ON w.id=a.workspace_id WHERE a.account_id=$1 AND w.user_id=$2 AND a.active", session.ID, owner).Scan(&access)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, staffForbidden()
	}
	if err != nil {
		return nil, err
	}
	return &staffAdmission{session: session, owner: owner, access: access, operation: op}, nil
}
func (s *Service) staffMutate(w http.ResponseWriter, r *http.Request, op string) error {
	a, err := s.staffAdmission(r, op)
	if err != nil {
		return err
	}
	var previous Booking
	if op != "create" {
		previous, err = ownedBooking(r.Context(), s.pool, a.owner, r.PathValue("id"))
		if err != nil {
			return err
		}
	}
	request, key, hash, err := decodeBooking(w, r, op)
	if err != nil {
		return err
	}
	if op == "status" && request.Status == "CANCELLED" {
		a.operation = "cancel"
	}
	if request.PatientID != nil {
		return staffForbidden()
	}
	// Do not expose a clinical ID carried by an existing administrative booking.
	// Engine checks/records it internally but response projection removes it.
	key = "staff:" + staffHash(a.session.ID+":"+a.access+":"+key)
	r = r.WithContext(context.WithValue(r.Context(), staffKey{}, a))
	return s.applyBooking(w, r, a.owner, op, previous, request, key, hash, nil)
}
func (s *Service) staffBookings(w http.ResponseWriter, r *http.Request) error {
	a, err := s.staffAdmission(r, "view")
	if err != nil {
		return err
	}
	a.clinic = r.PathValue("clinicId")
	q := r.URL.Query()
	for key, values := range q {
		if len(values) != 1 || key != "from" && key != "to" && key != "page" && key != "size" {
			return api.Invalid("Consulta inválida.")
		}
	}
	from, err := bookingInstant(q.Get("from"))
	if err != nil {
		return err
	}
	to, err := bookingInstant(q.Get("to"))
	if err != nil || !to.After(from) || to.Sub(from) > 93*24*time.Hour {
		return api.Invalid("Rango inválido.")
	}
	page, size := int64(0), int64(100)
	if q.Has("page") {
		page, err = decimal(q.Get("page"), false)
		if err != nil {
			return err
		}
	}
	if q.Has("size") {
		size, err = decimal(q.Get("size"), true)
		if err != nil {
			return err
		}
	}
	if size > 100 || page > 9223372036854775807/size {
		return api.Invalid("Paginación inválida.")
	}
	tx, err := s.staffReadTx(r, a)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(r.Context(), "SELECT "+bookingColumns+" FROM bookings b WHERE clinic_id=$1 AND start_at<$3 AND end_at>$2 ORDER BY start_at,id LIMIT $4 OFFSET $5", a.clinic, from, to, size+1, page*size)
	if err != nil {
		return err
	}
	items := []map[string]any{}
	for rows.Next() {
		b, e := scanBooking(rows)
		if e != nil {
			rows.Close()
			return e
		}
		raw, _ := json.Marshal(b)
		doc, e := staffBookingDocument(raw)
		if e != nil {
			rows.Close()
			return e
		}
		items = append(items, doc)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	more := len(items) > int(size)
	if more {
		items = items[:size]
	}
	api.WriteJSON(w, 200, map[string]any{"items": items, "page": page, "size": size, "hasMore": more})
	return nil
}
func (s *Service) staffAssignments(w http.ResponseWriter, r *http.Request) error {
	session, err := s.staffSession(r, false)
	if err != nil {
		return err
	}
	rows, err := s.pool.Query(r.Context(), "SELECT w.user_id,a.id::text FROM staff_access a JOIN doctor_workspaces w ON w.id=a.workspace_id WHERE a.account_id=$1 AND a.active ORDER BY a.id LIMIT 100", session.ID)
	if err != nil {
		return err
	}
	type relation struct{ owner, id string }
	relations := []relation{}
	for rows.Next() {
		var rel relation
		if err = rows.Scan(&rel.owner, &rel.id); err != nil {
			rows.Close()
			return err
		}
		relations = append(relations, rel)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	items := []map[string]any{}
	for _, rel := range relations {
		a := &staffAdmission{session: session, owner: rel.owner, access: rel.id, operation: "view"}
		tx, e := s.staffReadTx(r, a)
		if e != nil {
			if failure, ok := e.(*api.Error); ok && (failure.Status == 403 || failure.Status == 401) {
				continue
			}
			return e
		}
		clinics, e := tx.Query(r.Context(), "SELECT c.id::text,c.name,c.timezone,a.permissions,COALESCE(p.name,'Profesional asignado') FROM staff_access_clinics x JOIN clinics c ON c.id=x.clinic_id JOIN staff_access a ON a.id=x.access_id LEFT JOIN booking_publications p ON p.workspace_id=a.workspace_id WHERE x.access_id=$1 AND c.status<>'ARCHIVED' ORDER BY c.id", rel.id)
		if e != nil {
			tx.Rollback(context.Background())
			return e
		}
		for clinics.Next() {
			var id, name, zone, doctorName string
			var permissions []string
			if e = clinics.Scan(&id, &name, &zone, &permissions, &doctorName); e != nil {
				break
			}
			items = append(items, map[string]any{"ownerId": rel.owner, "accessId": rel.id, "clinicId": id, "clinicName": name, "doctorName": doctorName, "timezone": zone, "permissions": permissions, "validUntil": a.validUntil})
		}
		if e == nil {
			e = clinics.Err()
		}
		clinics.Close()
		tx.Rollback(context.Background())
		if e != nil {
			return e
		}
	}
	api.WriteJSON(w, 200, map[string]any{"items": items})
	return nil
}
