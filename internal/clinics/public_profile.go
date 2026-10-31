package clinics

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type PublicQuestion struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}
type PublicClinic struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Country  string `json:"country"`
	Province string `json:"province"`
	City     string `json:"city"`
	Address  string `json:"address"`
	Timezone string `json:"timezone"`
}
type PublicForm struct {
	Version   string           `json:"version"`
	Questions []PublicQuestion `json:"questions"`
}
type PublicDoctor struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	SpecialtyCode string         `json:"specialtyCode"`
	Specialty     string         `json:"specialty"`
	Clinics       []PublicClinic `json:"clinics"`
	Form          PublicForm     `json:"form"`
	owner         string
	workspace     int64
	published     bool
}
type PublishedClinic struct {
	ClinicID string `json:"clinicId"`
	Enabled  bool   `json:"enabled"`
	Address  string `json:"address"`
}
type Publication struct {
	ExpectedVersion string            `json:"expectedVersion,omitempty"`
	PublicID        string            `json:"publicId,omitempty"`
	Version         string            `json:"version,omitempty"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Published       bool              `json:"published"`
	Clinics         []PublishedClinic `json:"clinics"`
	Questions       []PublicQuestion  `json:"questions"`
}

func (s *Service) readPublicDoctor(ctx context.Context, q scheduleReader, id string) (PublicDoctor, error) {
	var d PublicDoctor
	var questions []byte
	if !opaqueID.MatchString(id) {
		return d, publicMissing()
	}
	err := q.QueryRow(ctx, "SELECT p.public_id::text,p.name,p.description,s.code,s.display_name,p.version::text,p.questions,w.user_id,w.id,p.published FROM booking_publications p JOIN doctor_workspaces w ON w.id=p.workspace_id JOIN specialties s ON s.id=w.specialty_id WHERE p.public_id=$1", id).Scan(&d.ID, &d.Name, &d.Description, &d.SpecialtyCode, &d.Specialty, &d.Form.Version, &questions, &d.owner, &d.workspace, &d.published)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, publicMissing()
	}
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal(questions, &d.Form.Questions); err != nil {
		return d, err
	}
	d.Clinics = []PublicClinic{}
	rows, err := q.Query(ctx, "SELECT p.public_id::text,c.name,c.country,c.province,c.city,p.address,c.timezone FROM clinic_publications p JOIN clinics c ON c.id=p.clinic_id WHERE p.workspace_id=$1 AND p.enabled AND c.status='ACTIVE' ORDER BY c.name,c.id", d.workspace)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var c PublicClinic
		if err = rows.Scan(&c.ID, &c.Name, &c.Country, &c.Province, &c.City, &c.Address, &c.Timezone); err != nil {
			return d, err
		}
		d.Clinics = append(d.Clinics, c)
	}
	return d, rows.Err()
}
func (s *Service) onlineRights(ctx context.Context, owner string) error {
	snap, err := s.publicRights.Resolve(ctx, owner)
	if err != nil {
		return unavailable()
	}
	if !snap.ClinicManagement || snap.OnlineBooking == nil || !*snap.OnlineBooking {
		return publicDenied()
	}
	if snap.MaxClinics == nil {
		return unavailable()
	}
	var count int64
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM clinics c JOIN doctor_workspaces w ON c.workspace_id=w.id WHERE w.user_id=$1 AND c.status<>'ARCHIVED'", owner).Scan(&count); err != nil {
		return err
	}
	if *snap.MaxClinics != -1 && count > *snap.MaxClinics {
		return publicDenied()
	}
	return nil
}
func (s *Service) publicProfile(w http.ResponseWriter, r *http.Request) error {
	d, err := s.readPublicDoctor(r.Context(), s.pool, r.PathValue("publicId"))
	if err != nil {
		return err
	}
	if !d.published || len(d.Clinics) == 0 {
		return publicMissing()
	}
	if err = s.onlineRights(r.Context(), d.owner); err != nil {
		return err
	}
	api.WriteJSON(w, 200, d)
	return nil
}
func (s *Service) publicDirectory(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	for k, v := range q {
		if len(v) != 1 || len(v[0]) > 160 {
			return api.Invalid("Filtros inválidos.")
		}
		switch k {
		case "country", "province", "city", "specialty", "name", "page", "size":
		default:
			return api.Invalid("Filtro no admitido.")
		}
	}
	page, size := 0, 20
	var err error
	if q.Has("page") {
		page, err = strconv.Atoi(q.Get("page"))
		if err != nil || page < 0 || page > 10000 {
			return api.Invalid("Página inválida.")
		}
	}
	if q.Has("size") {
		size, err = strconv.Atoi(q.Get("size"))
		if err != nil || size < 1 || size > 20 {
			return api.Invalid("Tamaño inválido.")
		}
	}
	// Filter locations on the same enabled public clinic; never return private rows.
	rows, err := s.pool.Query(r.Context(), "SELECT p.public_id::text FROM booking_publications p JOIN doctor_workspaces w ON w.id=p.workspace_id JOIN specialties s ON s.id=w.specialty_id WHERE p.published AND strpos(lower(p.name),lower($1))>0 AND (strpos(lower(s.display_name),lower($2))>0 OR lower(s.code)=lower($2)) AND EXISTS(SELECT 1 FROM clinic_publications cp JOIN clinics c ON c.id=cp.clinic_id WHERE cp.workspace_id=p.workspace_id AND cp.enabled AND c.status='ACTIVE' AND strpos(lower(c.country),lower($3))>0 AND strpos(lower(c.province),lower($4))>0 AND strpos(lower(c.city),lower($5))>0) ORDER BY lower(p.name),p.public_id LIMIT $6 OFFSET $7", q.Get("name"), q.Get("specialty"), q.Get("country"), q.Get("province"), q.Get("city"), size+1, page*size)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	result := []PublicDoctor{}
	for _, id := range ids {
		d, e := s.readPublicDoctor(r.Context(), s.pool, id)
		if e != nil {
			return e
		}
		if e = s.onlineRights(r.Context(), d.owner); e != nil {
			if failure, ok := e.(*api.Error); ok && failure.Status == 403 {
				continue
			}
			return e
		}
		if d.published && len(d.Clinics) > 0 {
			result = append(result, d)
		}
	}
	more := len(ids) > size
	if len(result) > size {
		result = result[:size]
	}
	api.WriteJSON(w, 200, map[string]any{"items": result, "page": page, "size": size, "hasMore": more})
	return nil
}
func (s *Service) getPublication(w http.ResponseWriter, r *http.Request, user string) error {
	workspace, err := s.workspace(r.Context(), user)
	if err != nil {
		return err
	}
	p := Publication{Version: "0", Clinics: []PublishedClinic{}, Questions: []PublicQuestion{}}
	var questions []byte
	err = s.pool.QueryRow(r.Context(), "SELECT public_id::text,version::text,name,description,published,questions FROM booking_publications WHERE workspace_id=$1", workspace).Scan(&p.PublicID, &p.Version, &p.Name, &p.Description, &p.Published, &questions)
	if errors.Is(err, pgx.ErrNoRows) {
		api.WriteJSON(w, 200, p)
		return nil
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal(questions, &p.Questions); err != nil {
		return err
	}
	rows, err := s.pool.Query(r.Context(), "SELECT clinic_id::text,enabled,address FROM clinic_publications WHERE workspace_id=$1 ORDER BY clinic_id", workspace)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c PublishedClinic
		if err = rows.Scan(&c.ClinicID, &c.Enabled, &c.Address); err != nil {
			return err
		}
		p.Clinics = append(p.Clinics, c)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	api.WriteJSON(w, 200, p)
	return nil
}

var questionKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

func (s *Service) putPublication(w http.ResponseWriter, r *http.Request, user string) error {
	var p Publication
	if err := decodePublic(w, r, &p); err != nil {
		return err
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Description = strings.TrimSpace(p.Description)
	if _, err := decimal(p.ExpectedVersion, false); err != nil {
		return err
	}
	if len([]rune(p.Name)) < 1 || len([]rune(p.Name)) > 160 || len([]rune(p.Description)) > 1200 || len(p.Questions) > 8 || len(p.Clinics) > 1000 {
		return api.Invalid("Revisá el perfil público y sus límites.")
	}
	keys := map[string]bool{}
	for _, q := range p.Questions {
		if !questionKey.MatchString(q.Key) || keys[q.Key] || len([]rune(strings.TrimSpace(q.Label))) < 1 || len([]rune(q.Label)) > 120 {
			return api.Invalid("Preguntas administrativas inválidas.")
		}
		keys[q.Key] = true
	}
	if p.Published {
		if err := s.onlineRights(r.Context(), user); err != nil {
			return err
		}
	}
	workspace, err := s.workspace(r.Context(), user)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	ctx := r.Context()
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='10s'"); err != nil {
		return err
	}
	if p.Published {
		snap, e := s.publicRights.Resolve(ctx, user)
		if e != nil {
			return unavailable()
		}
		if e = s.publicRights.Fence(ctx, tx, snap); e != nil {
			return unavailable()
		}
		if snap.OnlineBooking == nil || !*snap.OnlineBooking {
			return publicDenied()
		}
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE id=$1 AND user_id=$2 FOR UPDATE", workspace, user).Scan(&workspace); err != nil {
		return err
	}
	version := "0"
	err = tx.QueryRow(ctx, "SELECT version::text FROM booking_publications WHERE workspace_id=$1", workspace).Scan(&version)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if version != p.ExpectedVersion {
		return api.Failure(409, "VERSION_CONFLICT", "La publicación cambió. Recargá la versión vigente.")
	}
	// Resolve/lock every clinic before publishing; another doctor's ID is always 404.
	enabled := 0
	seen := map[string]bool{}
	for _, c := range p.Clinics {
		if _, err = decimal(c.ClinicID, true); err != nil {
			return err
		}
		if seen[c.ClinicID] || len([]rune(strings.TrimSpace(c.Address))) > 255 || c.Enabled && strings.TrimSpace(c.Address) == "" {
			return api.Invalid("Consultorios públicos inválidos.")
		}
		seen[c.ClinicID] = true
		var state string
		if err = tx.QueryRow(ctx, "SELECT status FROM clinics WHERE id=$1 AND workspace_id=$2 FOR UPDATE", c.ClinicID, workspace).Scan(&state); errors.Is(err, pgx.ErrNoRows) {
			return missing()
		} else if err != nil {
			return err
		}
		if c.Enabled && state == "ACTIVE" {
			enabled++
		}
	}
	if p.Published && enabled == 0 {
		return api.Invalid("Elegí al menos un consultorio activo con dirección pública.")
	}
	raw, _ := json.Marshal(p.Questions)
	if p.Questions == nil {
		raw = []byte("[]")
	}
	if _, err = tx.Exec(ctx, "INSERT INTO booking_publications(workspace_id,public_id,name,description,published,questions) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(workspace_id) DO UPDATE SET name=excluded.name,description=excluded.description,published=excluded.published,questions=excluded.questions,version=booking_publications.version+1", workspace, newOpaqueID(), p.Name, p.Description, p.Published, raw); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE clinic_publications SET enabled=false WHERE workspace_id=$1", workspace); err != nil {
		return err
	}
	for _, c := range p.Clinics {
		if _, err = tx.Exec(ctx, "INSERT INTO clinic_publications(clinic_id,workspace_id,public_id,enabled,address) VALUES($1,$2,$3,$4,$5) ON CONFLICT(clinic_id) DO UPDATE SET enabled=excluded.enabled,address=excluded.address", c.ClinicID, workspace, newOpaqueID(), c.Enabled, strings.TrimSpace(c.Address)); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return s.getPublication(w, r, user)
}
