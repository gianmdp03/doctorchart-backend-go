// Package clinics owns administrative places, never patient ownership.
package clinics

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/billing"
)

type Clinic struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Country   string    `json:"country"`
	Province  string    `json:"province"`
	City      string    `json:"city"`
	Address   string    `json:"address"`
	Timezone  string    `json:"timezone"`
	Status    string    `json:"status"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Service struct {
	pool   *pgxpool.Pool
	rights *billing.Entitlements
}

func New(pool *pgxpool.Pool, rights *billing.Entitlements) *Service { return &Service{pool, rights} }

const columns = "id::text,name,country,province,city,address,timezone,status,version::text,created_at,updated_at"

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Clinic, error) {
	var c Clinic
	err := row.Scan(&c.ID, &c.Name, &c.Country, &c.Province, &c.City, &c.Address, &c.Timezone, &c.Status, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	c.CreatedAt, c.UpdatedAt = c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	return c, err
}
func missing() error { return api.Failure(404, "CLINIC_NOT_FOUND", "No se encontró el consultorio.") }
func unavailable() error {
	return api.Failure(503, "ENTITLEMENTS_UNAVAILABLE", "No se pudieron verificar los derechos comerciales. Reintentá más tarde.")
}
func field(path, code string) error {
	return &api.Error{Status: 400, Code: "INVALID_REQUEST", Message: "Revisá los datos del consultorio.", Errors: []api.FieldError{{Path: path, Code: code}}}
}
func decimal(raw string, positive bool) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 || positive && n == 0 || strconv.FormatInt(n, 10) != raw {
		return 0, api.Invalid("El identificador o la versión no es válido.")
	}
	return n, nil
}
func validStatus(status string) bool {
	return status == "ACTIVE" || status == "INACTIVE" || status == "ARCHIVED"
}

func (s *Service) workspace(ctx context.Context, user string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1", user).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, api.Failure(409, "ONBOARDING_REQUIRED", "Completá la configuración del médico.")
	}
	return id, err
}
func (s *Service) owned(ctx context.Context, user string, id int64) (Clinic, error) {
	c, err := scan(s.pool.QueryRow(ctx, "SELECT "+columns+" FROM clinics WHERE id=$1 AND workspace_id=(SELECT id FROM doctor_workspaces WHERE user_id=$2)", id, user))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, missing()
	}
	return c, err
}

func (s *Service) Routes(mux *http.ServeMux) {
	wrap := func(fn func(http.ResponseWriter, *http.Request, string) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			identity := api.User(r.Context())
			if identity.UserID == "" {
				api.WriteError(w, api.Failure(401, "UNAUTHENTICATED", "Se requiere autenticación."))
				return
			}
			if !identity.HasRole("COMPANY") && !identity.HasRole("SUPER_ADMIN") {
				api.WriteError(w, api.Failure(403, "FORBIDDEN", "Acceso denegado."))
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			if err := fn(w, r.WithContext(ctx), identity.UserID); err != nil {
				api.WriteError(w, err)
			}
		}
	}
	mux.HandleFunc("GET /api/doctorchart/clinics", wrap(s.list))
	mux.HandleFunc("GET /api/doctorchart/clinics/capabilities", wrap(s.capabilities))
	mux.HandleFunc("GET /api/doctorchart/clinics/{id}", wrap(func(w http.ResponseWriter, r *http.Request, user string) error {
		id, err := decimal(r.PathValue("id"), true)
		if err != nil {
			return err
		}
		c, err := s.owned(r.Context(), user, id)
		if err != nil {
			return err
		}
		api.WriteJSON(w, 200, c)
		return nil
	}))
	for _, route := range []struct{ pattern, op string }{{"POST /api/doctorchart/clinics", "create"}, {"PATCH /api/doctorchart/clinics/{id}", "edit"}, {"PATCH /api/doctorchart/clinics/{id}/status", "status"}} {
		mux.HandleFunc(route.pattern, wrap(func(w http.ResponseWriter, r *http.Request, user string) error { return s.mutate(w, r, user, route.op) }))
	}
}

func (s *Service) list(w http.ResponseWriter, r *http.Request, user string) error {
	q := r.URL.Query()
	page, size := int64(0), int64(20)
	for key, values := range q {
		if len(values) != 1 {
			return api.Invalid("Parámetro duplicado.")
		}
		switch key {
		case "page", "size":
			n, err := decimal(values[0], false)
			if err != nil {
				return err
			}
			if key == "page" {
				page = n
			} else {
				size = n
			}
		case "status":
			if !validStatus(values[0]) {
				return field("status", "INVALID_STATUS")
			}
		default:
			return api.Invalid("Parámetro no reconocido.")
		}
	}
	if size < 1 || size > 100 || page > 9223372036854775807/size {
		return api.Invalid("La paginación no es válida.")
	}
	workspace, err := s.workspace(r.Context(), user)
	if err != nil {
		return err
	}
	status := q.Get("status")
	var total int64
	if err := s.pool.QueryRow(r.Context(), "SELECT count(*) FROM clinics WHERE workspace_id=$1 AND ($2='' OR status=$2)", workspace, status).Scan(&total); err != nil {
		return err
	}
	rows, err := s.pool.Query(r.Context(), "SELECT "+columns+" FROM clinics WHERE workspace_id=$1 AND ($2='' OR status=$2) ORDER BY id LIMIT $3 OFFSET $4", workspace, status, size, page*size)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Clinic{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return err
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	api.WriteJSON(w, 200, map[string]any{"items": items, "page": page, "size": size, "totalElements": total})
	return nil
}

func (s *Service) capabilities(w http.ResponseWriter, r *http.Request, user string) error {
	workspace, err := s.workspace(r.Context(), user)
	if err != nil {
		return err
	}
	var count int64
	if err := s.pool.QueryRow(r.Context(), "SELECT count(*) FROM clinics WHERE workspace_id=$1 AND status<>'ARCHIVED'", workspace).Scan(&count); err != nil {
		return err
	}
	actions := map[string]bool{"create": false, "edit": false, "activate": false, "deactivate": false, "archive": false, "restore": false}
	ent := map[string]any{"status": "UNAVAILABLE", "revision": nil, "validUntil": nil, "clinicManagement": nil, "max_clinics": nil}
	var reason any = "ENTITLEMENTS_UNAVAILABLE"
	snapshot, err := s.rights.Resolve(r.Context(), user)
	if err == nil {
		ent["revision"], ent["validUntil"], ent["clinicManagement"], ent["max_clinics"] = snapshot.Revision, snapshot.ValidUntil, snapshot.ClinicManagement, snapshot.MaxClinics
		ent["status"], reason = "DENIED", "CLINIC_MANAGEMENT_DISABLED"
		if snapshot.ClinicManagement {
			ent["status"], reason = "VERIFIED", nil
			max := *snapshot.MaxClinics
			over := max != -1 && count > max
			full := max != -1 && count >= max
			actions["edit"], actions["activate"], actions["deactivate"], actions["archive"] = !over, !over, true, true
			actions["create"], actions["restore"] = !full, !full
			if full {
				reason = "CLINIC_LIMIT_REACHED"
			}
		}
	}
	api.WriteJSON(w, 200, map[string]any{"entitlements": ent, "usage": map[string]int64{"counted": count}, "actions": actions, "reason": reason})
	return nil
}

var fieldLimits = map[string]int{"name": 160, "country": 100, "province": 160, "city": 160, "address": 255, "timezone": 100}

func values(body map[string]any, op string) (map[string]string, int64, error) {
	fields := map[string]string{}
	var version int64
	for key, value := range body {
		if key == "expectedVersion" && op != "create" {
			raw, ok := value.(string)
			if !ok {
				return nil, 0, field(key, "INVALID_VERSION")
			}
			v, err := decimal(raw, false)
			if err != nil {
				return nil, 0, field(key, "INVALID_VERSION")
			}
			version = v
			continue
		}
		max, ok := fieldLimits[key]
		if op == "status" {
			max, ok = 16, key == "status"
		}
		if !ok {
			return nil, 0, field(key, "UNKNOWN_FIELD")
		}
		text, ok := value.(string)
		text = strings.TrimSpace(text)
		if !ok || text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > max || strings.ContainsRune(text, 0) {
			return nil, 0, field(key, "INVALID_VALUE")
		}
		if key == "timezone" {
			_, err := time.LoadLocation(text)
			if err != nil || (text != "UTC" && !strings.Contains(text, "/")) || strings.HasPrefix(text, "posix/") || strings.HasPrefix(text, "right/") || strings.HasPrefix(text, "Etc/") || strings.ContainsAny(text, "\\\x00") {
				return nil, 0, field(key, "INVALID_TIMEZONE")
			}
		}
		if key == "status" && !validStatus(text) {
			return nil, 0, field(key, "INVALID_STATUS")
		}
		fields[key] = text
	}
	if op == "create" {
		for key := range fieldLimits {
			if _, ok := fields[key]; !ok {
				return nil, 0, field(key, "REQUIRED")
			}
		}
	} else {
		if _, ok := body["expectedVersion"]; !ok {
			return nil, 0, field("expectedVersion", "REQUIRED")
		}
		if len(fields) == 0 {
			return nil, 0, api.Invalid("Indicá los campos a modificar.")
		}
	}
	return fields, version, nil
}

func (s *Service) mutate(w http.ResponseWriter, r *http.Request, user, op string) error {
	var id int64
	if op != "create" {
		var err error
		id, err = decimal(r.PathValue("id"), true)
		if err != nil {
			return err
		}
		if _, err = s.owned(r.Context(), user, id); err != nil {
			return err
		}
	}
	if _, err := s.workspace(r.Context(), user); err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	body, err := api.Decode(w, r)
	if err != nil {
		return err
	}
	fields, expected, err := values(body, op)
	if err != nil {
		return err
	}
	snapshot, err := s.rights.Resolve(r.Context(), user)
	if err != nil {
		return unavailable()
	}
	ctx := r.Context()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='10s'"); err != nil {
		return err
	}
	if err := s.rights.Fence(ctx, tx, snapshot); err != nil {
		return unavailable()
	}
	var workspace int64
	if err := tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR UPDATE", user).Scan(&workspace); err != nil {
		return err
	}
	var current Clinic
	if op != "create" {
		current, err = scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM clinics WHERE id=$1 AND workspace_id=$2 FOR UPDATE", id, workspace))
		if errors.Is(err, pgx.ErrNoRows) {
			return missing()
		}
		if err != nil {
			return err
		}
		if current.Version != strconv.FormatInt(expected, 10) {
			return api.Failure(409, "VERSION_CONFLICT", "El consultorio cambió. Actualizá los datos antes de guardar.")
		}
	}
	// The owner lock may have waited; check the lease again at admission.
	if err := s.rights.Fence(ctx, tx, snapshot); err != nil {
		return unavailable()
	}
	if !snapshot.ClinicManagement {
		return api.Failure(403, "CLINIC_MANAGEMENT_DISABLED", "Tu cobertura no habilita la gestión de consultorios.")
	}
	var count int64
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM clinics WHERE workspace_id=$1 AND status<>'ARCHIVED'", workspace).Scan(&count); err != nil {
		return err
	}
	target := fields["status"]
	if op == "edit" && current.Status == "ARCHIVED" {
		return api.Failure(409, "CLINIC_STATE_CONFLICT", "Restaurá el consultorio antes de editarlo.")
	}
	if op == "status" && current.Status == "ARCHIVED" && target != "INACTIVE" && target != "ARCHIVED" {
		return api.Failure(409, "CLINIC_STATE_CONFLICT", "Restaurá el consultorio como inactivo.")
	}
	max := *snapshot.MaxClinics
	adds := op == "create" || op == "status" && current.Status == "ARCHIVED" && target == "INACTIVE"
	if max != -1 && (adds && count >= max || (op == "edit" || op == "status" && target == "ACTIVE") && count > max) {
		return api.Failure(409, "CLINIC_LIMIT_REACHED", "Se alcanzó el límite de consultorios de tu cobertura.")
	}
	var result Clinic
	switch op {
	case "create":
		result, err = scan(tx.QueryRow(ctx, "INSERT INTO clinics(workspace_id,name,country,province,city,address,timezone) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING "+columns, workspace, fields["name"], fields["country"], fields["province"], fields["city"], fields["address"], fields["timezone"]))
	case "status":
		if current.Status == target {
			result = current
		} else {
			result, err = scan(tx.QueryRow(ctx, "UPDATE clinics SET status=$3,version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND workspace_id=$2 RETURNING "+columns, id, workspace, target))
		}
	case "edit":
		merged := map[string]string{"name": current.Name, "country": current.Country, "province": current.Province, "city": current.City, "address": current.Address, "timezone": current.Timezone}
		for key, value := range fields {
			merged[key] = value
		}
		result, err = scan(tx.QueryRow(ctx, "UPDATE clinics SET name=$3,country=$4,province=$5,city=$6,address=$7,timezone=$8,version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND workspace_id=$2 RETURNING "+columns, id, workspace, merged["name"], merged["country"], merged["province"], merged["city"], merged["address"], merged["timezone"]))
	}
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23514" && pg.ConstraintName == "clinics_timezone_check" {
			return field("timezone", "INVALID_TIMEZONE")
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	status := 200
	if op == "create" {
		status = 201
		w.Header().Set("Location", "/api/doctorchart/clinics/"+result.ID)
	}
	api.WriteJSON(w, status, result)
	return nil
}
