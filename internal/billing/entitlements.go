package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrEntitlementsUnavailable = errors.New("effective entitlements unavailable")

type Snapshot struct {
	OwnerID           string     `json:"ownerId"`
	Module            string     `json:"module"`
	Revision          string     `json:"revision"`
	IssuedAt          time.Time  `json:"issuedAt"`
	ValidUntil        time.Time  `json:"validUntil"`
	CoverageExpiresAt *time.Time `json:"coverageExpiresAt"`
	ClinicManagement  bool       `json:"clinicManagement"`
	MaxClinics        *int64     `json:"max_clinics"`
	generation        int64
}

// Memory holds only signed, bounded leases. PostgreSQL fences invalidation
// across instances and requests already in flight. Restart means empty cache.
type Entitlements struct {
	pool         *pgxpool.Pool
	base, secret string
	client       *http.Client
	mu           sync.Mutex
	cache        map[string]Snapshot
	now          func() time.Time
}

func NewEntitlements(pool *pgxpool.Pool, base, secret string) *Entitlements {
	return &Entitlements{pool: pool, base: strings.TrimRight(base, "/"), secret: secret,
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		cache:  map[string]Snapshot{}, now: time.Now}
}

func (s Snapshot) valid(owner string, now time.Time) bool {
	if s.OwnerID != owner || s.Module != "DOCTORCHART" || len(s.Revision) != 64 || s.IssuedAt.After(now) || !s.ValidUntil.After(now) || s.ValidUntil.After(s.IssuedAt.Add(time.Minute)) {
		return false
	}
	if _, err := hex.DecodeString(s.Revision); err != nil {
		return false
	}
	if s.ClinicManagement {
		return s.CoverageExpiresAt != nil && !s.ValidUntil.After(*s.CoverageExpiresAt) && s.CoverageExpiresAt.After(now) && s.MaxClinics != nil && *s.MaxClinics >= -1 && *s.MaxClinics <= 2147483647
	}
	return s.MaxClinics == nil
}

func (e *Entitlements) Resolve(ctx context.Context, owner string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var generation int64
	if err := e.pool.QueryRow(ctx, "SELECT generation FROM clinic_entitlement_epoch WHERE id=1").Scan(&generation); err != nil {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	e.mu.Lock()
	cached, ok := e.cache[owner]
	e.mu.Unlock()
	if ok && cached.generation == generation && cached.valid(owner, e.now()) {
		return cached, nil
	}
	if e.secret == "" {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, "GET", e.base+"/internal/doctorchart/owners/"+url.PathEscape(owner)+"/entitlements", nil)
	if err != nil {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	req.Header.Set("X-Internal-Secret", e.secret)
	response, err := e.client.Do(req)
	if err != nil {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(body) > 16384 {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	signature, err := hex.DecodeString(response.Header.Get("X-Entitlements-Signature"))
	mac := hmac.New(sha256.New, []byte(e.secret))
	mac.Write(body)
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	var snapshot Snapshot
	var required map[string]json.RawMessage
	if json.Unmarshal(body, &required) != nil || len(required) != 8 {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	for _, key := range []string{"ownerId", "module", "revision", "issuedAt", "validUntil", "coverageExpiresAt", "clinicManagement", "max_clinics"} {
		if _, ok := required[key]; !ok {
			return Snapshot{}, ErrEntitlementsUnavailable
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	if !snapshot.valid(owner, e.now()) {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	snapshot.generation = generation
	var current int64
	if err := e.pool.QueryRow(ctx, "SELECT generation FROM clinic_entitlement_epoch WHERE id=1").Scan(&current); err != nil || current != generation {
		return Snapshot{}, ErrEntitlementsUnavailable
	}
	e.mu.Lock()
	if len(e.cache) >= 1024 {
		clear(e.cache)
	}
	e.cache[owner] = snapshot
	e.mu.Unlock()
	return snapshot, nil
}

// Fence is acquired before the owner quota lock and held through commit.
func (e *Entitlements) Fence(ctx context.Context, tx pgx.Tx, s Snapshot) error {
	var generation int64
	if err := tx.QueryRow(ctx, "SELECT generation FROM clinic_entitlement_epoch WHERE id=1 FOR SHARE").Scan(&generation); err != nil {
		return ErrEntitlementsUnavailable
	}
	if generation != s.generation || !s.valid(s.OwnerID, e.now()) {
		return ErrEntitlementsUnavailable
	}
	return nil
}

func (e *Entitlements) Invalidate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := e.pool.Exec(ctx, "UPDATE clinic_entitlement_epoch SET generation=generation+1 WHERE id=1")
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrEntitlementsUnavailable
	}
	return nil
}
