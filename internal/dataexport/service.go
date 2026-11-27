package dataexport

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/storage"
)

const (
	preparationTimeout = 5 * time.Minute
	downloadLifetime   = 20 * time.Minute
	maxJobs            = 64
	maxPreparing       = 2
	maxZipBytes        = 512 << 20
	maxRawBytes        = 1 << 30
	maxEntries         = 40000
	maxRetainedBytes   = 2 << 30
)

type exportState struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	Password   string     `json:"password,omitempty"`
	PreparedAt *time.Time `json:"preparedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	Bytes      int64      `json:"bytes,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type job struct {
	exportState
	owner     string
	digest    string
	contexts  []int64
	downloads int
	delivered bool
	file      *os.File
	namedPath string
}

type Service struct {
	pool                      *pgxpool.Pool
	files                     *storage.Store
	billingURL, billingSecret string
	client                    *http.Client
	dir                       string
	ctx                       context.Context
	cancel                    context.CancelFunc
	mu                        sync.Mutex
	wg                        sync.WaitGroup
	jobs                      map[string]*job
	preparing                 int
}

func New(ctx context.Context, pool *pgxpool.Pool, files *storage.Store, billingURL, billingSecret string) (*Service, error) {
	dir, err := os.MkdirTemp("", "doctorchart-export-")
	if err != nil {
		return nil, fmt.Errorf("create export storage: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{
		pool: pool, files: files, billingURL: strings.TrimRight(billingURL, "/"), billingSecret: billingSecret,
		client: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		dir:    dir, ctx: ctx, cancel: cancel, jobs: make(map[string]*job),
	}
	s.wg.Go(func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				s.mu.Lock()
				s.prune(now)
				s.mu.Unlock()
			}
		}
	})
	return s, nil
}

func (s *Service) Close() {
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	for _, job := range s.jobs {
		_ = job.discard()
	}
	s.mu.Unlock()
	if err := os.RemoveAll(s.dir); err != nil {
		slog.Warn("doctor export cleanup failed")
	}
}

func (s *Service) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST "+api.DoctorExportsPath, s.start)
	mux.HandleFunc("GET "+api.DoctorExportsPath, s.current)
	mux.HandleFunc("GET "+api.DoctorExportsPath+"/{id}", s.status)
	mux.HandleFunc("GET "+api.DoctorExportsPath+"/{id}/file", s.download)
}

func exportOwner(w http.ResponseWriter, r *http.Request) string {
	id := api.User(r.Context())
	if !api.DoctorExportRoute(r.Method, r.URL.Path) || r.URL.RawPath != "" {
		api.WriteError(w, missing())
		return ""
	}
	if id.UserID == "" || id.HasRole("STAFF") || id.HasRole("SECRETARY") {
		api.WriteError(w, api.Failure(http.StatusForbidden, "EXPORT_FORBIDDEN", "La copia sólo está disponible para el titular."))
		return ""
	}
	return id.UserID
}

func missing() error {
	return api.Failure(http.StatusNotFound, "EXPORT_NOT_FOUND", "La copia no está disponible o venció.")
}

func (j *job) discard() error {
	if j.file != nil {
		_ = j.file.Close()
		j.file = nil
	}
	if j.namedPath != "" {
		if err := os.Remove(j.namedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		j.namedPath = ""
	}
	return nil
}

func (s *Service) prune(now time.Time) {
	for id, job := range s.jobs {
		if job.ExpiresAt != nil && !now.Before(*job.ExpiresAt) && job.downloads == 0 {
			if err := job.discard(); err == nil {
				delete(s.jobs, id)
			}
		}
	}
}

// Reserve the worst-case space for a new preparation. Copies waiting for
// their first download stay available until expiry, subject to owner actions.
func (s *Service) reserveRoom() bool {
	for {
		total := int64(s.preparing+1) * maxZipBytes
		var recyclable *job
		for _, current := range s.jobs {
			total += current.Bytes
			if current.downloads == 0 && (current.delivered || current.Status == "failed") &&
				(recyclable == nil || current.ExpiresAt.Before(*recyclable.ExpiresAt)) {
				recyclable = current
			}
		}
		if len(s.jobs) < maxJobs && total <= maxRetainedBytes {
			return true
		}
		if recyclable == nil || recyclable.discard() != nil {
			return false
		}
		delete(s.jobs, recyclable.ID)
	}
}

func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	owner := exportOwner(w, r)
	if owner == "" {
		return
	}
	prefs, err := readPreferences(w, r)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	profile, err := readProfile(r.Header.Get("X-Doctor-Export-Profile"))
	if err != nil {
		api.WriteError(w, err)
		return
	}
	id := hex.EncodeToString(randBytes(16))
	password := base64.RawURLEncoding.EncodeToString(randBytes(24))
	state := exportState{ID: id, Status: "preparing"}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(time.Now())
	if s.ctx.Err() != nil || s.preparing >= maxPreparing {
		api.WriteError(w, api.Failure(http.StatusServiceUnavailable, "EXPORT_BUSY", "La preparación de copias está ocupada. Reintentá en unos minutos."))
		return
	}
	for previousID, previous := range s.jobs {
		if previous.owner != owner {
			continue
		}
		if previous.Status == "preparing" || previous.downloads > 0 {
			api.WriteError(w, api.Failure(http.StatusConflict, "EXPORT_IN_PROGRESS", "Ya hay una copia en preparación o descarga."))
			return
		}
		if err := previous.discard(); err != nil {
			api.WriteError(w, err)
			return
		}
		delete(s.jobs, previousID)
	}
	if !s.reserveRoom() {
		api.WriteError(w, api.Failure(http.StatusServiceUnavailable, "EXPORT_BUSY", "La preparación de copias está ocupada. Reintentá en unos minutos."))
		return
	}
	j := &job{exportState: state, owner: owner}
	s.jobs[id] = j
	s.preparing++
	s.wg.Go(func() { s.prepareJob(j, password, prefs, profile) })
	state.Password = password
	api.WriteJSON(w, http.StatusAccepted, state)
}

func randBytes(size int) []byte {
	raw := make([]byte, size)
	rand.Read(raw)
	return raw
}

func (s *Service) prepareJob(j *job, password string, prefs browserPreferences, profile map[string]string) {
	ctx, cancel := context.WithTimeout(s.ctx, preparationTimeout)
	defer cancel()

	result, err := s.build(ctx, j.ID, j.owner, password, prefs, profile)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preparing--
	expires := time.Now().UTC().Add(downloadLifetime)
	j.ExpiresAt = &expires
	if err != nil {
		j.Status = "failed"
		failure := new(api.Error)
		j.Error = "No se pudo preparar la copia. Volvé a intentar."
		if errors.As(err, &failure) {
			j.Error = failure.Message
		}
		slog.Warn("doctor export failed", "export_id", j.ID, "error_type", fmt.Sprintf("%T", err))
		return
	}
	j.Status = "ready"
	j.PreparedAt = &result.cut
	j.Bytes = result.bytes
	j.digest = result.digest
	j.contexts = result.contexts
	j.file = result.file
	j.namedPath = result.namedPath
}

func (s *Service) ownerJob(owner, id string) *job {
	j := s.jobs[id]
	if j == nil || j.owner != owner || j.ExpiresAt != nil && !time.Now().Before(*j.ExpiresAt) {
		return nil
	}
	return j
}

func (s *Service) status(w http.ResponseWriter, r *http.Request) {
	owner := exportOwner(w, r)
	if owner == "" {
		return
	}
	s.mu.Lock()
	j := s.ownerJob(owner, r.PathValue("id"))
	if j == nil {
		s.mu.Unlock()
		api.WriteError(w, missing())
		return
	}
	state := j.exportState
	s.mu.Unlock()
	api.WriteJSON(w, http.StatusOK, state)
}

func (s *Service) current(w http.ResponseWriter, r *http.Request) {
	owner := exportOwner(w, r)
	if owner == "" {
		return
	}
	s.mu.Lock()
	var state exportState
	for id := range s.jobs {
		if j := s.ownerJob(owner, id); j != nil {
			state = j.exportState
			break
		}
	}
	s.mu.Unlock()
	if state.ID == "" {
		api.WriteError(w, missing())
		return
	}
	api.WriteJSON(w, http.StatusOK, state)
}

func (s *Service) download(w http.ResponseWriter, r *http.Request) {
	owner := exportOwner(w, r)
	if owner == "" {
		return
	}
	s.mu.Lock()
	j := s.ownerJob(owner, r.PathValue("id"))
	if j == nil || j.Status != "ready" || j.file == nil {
		s.mu.Unlock()
		api.WriteError(w, missing())
		return
	}
	j.downloads++
	cut, digest := *j.PreparedAt, j.digest
	content := io.NewSectionReader(j.file, 0, j.Bytes)
	s.mu.Unlock()
	response := &downloadResponse{ResponseWriter: w}
	defer func() {
		s.mu.Lock()
		j.downloads--
		j.delivered = j.delivered || response.status == http.StatusOK && response.bytes == j.Bytes
		s.mu.Unlock()
	}()
	if err := s.audit(r.Context(), j.ID, owner, j.contexts, "PRACTICE_EXPORT_DOWNLOAD", "AUTHORIZED"); err != nil {
		api.WriteError(w, err)
		return
	}
	// Apply a dedicated response budget only to this large, bounded download.
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		api.WriteError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="doctorchart-export.zip"`)
	w.Header().Set("Repr-Digest", "sha-256=:"+digest+":")
	if r.Header.Get("Range") == "" {
		w.Header().Set("Content-Digest", "sha-256=:"+digest+":")
	}
	http.ServeContent(response, r, "doctorchart-export.zip", cut, content)
}

type downloadResponse struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *downloadResponse) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *downloadResponse) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (s *Service) audit(ctx context.Context, exportID, owner string, contexts []int64, operation, outcome string) error {
	if len(contexts) == 0 {
		// No clinical data is present. A context is mandatory for a successful
		// clinical audit row, so retain a service event without account data.
		slog.Info("doctor export audit", "export_id", exportID, "operation", operation, "outcome", outcome)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err == nil {
		defer tx.Rollback(ctx)
		err = writeAudit(ctx, tx, owner, contexts, operation, outcome)
		if err == nil {
			err = tx.Commit(ctx)
		}
	}
	if err != nil {
		return api.Failure(http.StatusServiceUnavailable, "EXPORT_AUDIT_UNAVAILABLE", "La auditoría no está disponible. Volvé a intentar.")
	}
	return nil
}

func writeAudit(ctx context.Context, tx pgx.Tx, owner string, contexts []int64, operation, outcome string) error {
	batch := &pgx.Batch{}
	for _, contextID := range contexts {
		batch.Queue(`INSERT INTO clinical_access_events
			(actor_user_id,actor_kind,operation,owner_user_id,specialty_context_id,outcome,request_id)
			VALUES($1,'DOCTOR',$2,$1,$3,$4,$5)`,
			owner, operation, contextID, outcome, hex.EncodeToString(randBytes(16)))
	}
	return tx.SendBatch(ctx, batch).Close()
}

type scheduleDraft struct {
	ClinicID string          `json:"clinicId"`
	Version  string          `json:"version"`
	Draft    json.RawMessage `json:"draft"`
}

type browserPreferences struct {
	Theme             string          `json:"theme,omitempty"`
	FontSize          string          `json:"fontSize,omitempty"`
	SelectedContextID string          `json:"selectedContextId,omitempty"`
	ScheduleDrafts    []scheduleDraft `json:"scheduleDrafts,omitempty"`
}

var localID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
var localVersion = regexp.MustCompile(`^(0|[1-9][0-9]{0,18})$`)

func readPreferences(w http.ResponseWriter, r *http.Request) (browserPreferences, error) {
	var body struct {
		BrowserPreferences browserPreferences `json:"browserPreferences"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		return browserPreferences{}, api.Invalid("Las preferencias del navegador no son válidas.")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return browserPreferences{}, api.Invalid("Las preferencias del navegador no son válidas.")
	}
	prefs := body.BrowserPreferences
	if !slices.Contains([]string{"", "light", "dark"}, prefs.Theme) ||
		!slices.Contains([]string{"", "normal", "large", "xlarge"}, prefs.FontSize) ||
		prefs.SelectedContextID != "" && !localID.MatchString(prefs.SelectedContextID) ||
		len(prefs.ScheduleDrafts) > 100 {
		return browserPreferences{}, api.Invalid("Las preferencias del navegador no son válidas.")
	}
	seen := make(map[string]bool)
	for _, draft := range prefs.ScheduleDrafts {
		if !localID.MatchString(draft.ClinicID) || !localVersion.MatchString(draft.Version) || seen[draft.ClinicID] || !validScheduleDraft(draft.Draft) {
			return browserPreferences{}, api.Invalid("Los borradores de agenda no son válidos.")
		}
		seen[draft.ClinicID] = true
	}
	return prefs, nil
}

func validScheduleDraft(raw json.RawMessage) bool {
	type slot struct {
		ID          string `json:"id,omitempty"`
		Start       string `json:"start"`
		End         string `json:"end"`
		SlotMinutes *int   `json:"slotMinutes,omitempty"`
		ContextID   string `json:"specialtyContextId,omitempty"`
	}
	var draft struct {
		MinNotice  *int `json:"minNoticeMinutes"`
		MaxAdvance *int `json:"maxAdvanceDays"`
		Weekly     []struct {
			Weekday int    `json:"weekday"`
			Ranges  []slot `json:"ranges"`
		} `json:"weekly"`
		Exceptions []struct {
			ID      string `json:"id,omitempty"`
			From    string `json:"from"`
			Through string `json:"through"`
			Label   string `json:"label"`
			Ranges  []slot `json:"ranges"`
		} `json:"exceptions"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	return len(raw) > 0 && raw[0] == '{' && decoder.Decode(&draft) == nil
}

func readProfile(encoded string) (map[string]string, error) {
	output := make(map[string]string)
	if encoded == "" {
		return output, nil
	}
	invalid := api.Failure(http.StatusServiceUnavailable, "EXPORT_PROFILE_UNAVAILABLE", "El perfil de cuenta no está disponible. Volvé a intentar.")
	if len(encoded) > 8192 {
		return nil, invalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, invalid
	}
	var profile map[string]string
	if err = json.Unmarshal(raw, &profile); err != nil {
		return nil, invalid
	}
	for _, field := range []string{"full_name", "first_name", "last_name", "dni", "license", "clinic_name", "clinic_address", "clinic_phone", "phone", "email"} {
		if value := profile[field]; value != "" {
			output[field] = value
		}
	}
	return output, nil
}
