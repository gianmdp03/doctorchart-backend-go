package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Rules struct {
	mu       sync.RWMutex
	reload   sync.Mutex
	tiers    map[string]map[string]any
	url      string
	client   *http.Client
	loadedAt time.Time
}

const rulesRefreshInterval = time.Minute
const rulesMaxAge = 10 * time.Minute

func New(url string) *Rules {
	return &Rules{url: strings.TrimSuffix(url, "/") + "/api/billing/limits/DOCTORCHART", client: &http.Client{Timeout: 6 * time.Second}, tiers: map[string]map[string]any{
		"BASIC":     {"max_patients": 50, "max_records_per_patient": 100},
		"PRO":       {"max_patients": 500, "max_records_per_patient": 1000},
		"UNLIMITED": {"max_patients": -1, "max_records_per_patient": -1},
	}}
}

func (r *Rules) Load(ctx context.Context) {
	for attempt := 0; attempt < 3; attempt++ {
		err := r.Reload(ctx)
		if err == nil {
			return
		}
		if err != nil && attempt < 2 {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

// Reload publishes a validated snapshot atomically, preserving the last good one on error.
func (r *Rules) Reload(ctx context.Context) error {
	r.reload.Lock()
	defer r.reload.Unlock()
	return r.fetch(ctx)
}

// Available excludes startup defaults and snapshots older than the bounded cache lifetime.
func (r *Rules) Available() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return !r.loadedAt.IsZero() && time.Since(r.loadedAt) < rulesMaxAge
}

// EnsureFresh recovers an unavailable cache before a quota-dependent operation.
// Concurrent callers share a recovery fetch; callbacks can still force Reload.
func (r *Rules) EnsureFresh(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.Available() {
		return nil
	}
	r.reload.Lock()
	defer r.reload.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.Available() {
		return nil
	}
	return r.fetch(ctx)
}

// RunRefresh keeps every replica current and returns when its owner cancels ctx.
func (r *Rules) RunRefresh(ctx context.Context) {
	r.runRefresh(ctx, rulesRefreshInterval)
}

func (r *Rules) runRefresh(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Failed delivery preserves the previous snapshot; Available bounds its use.
			if err := r.Reload(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("billing refresh unavailable; previous snapshot retained")
			}
		}
	}
}

func (r *Rules) fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return err
	}
	response, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch billing: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("billing status %d", response.StatusCode)
	}
	var tiers map[string]map[string]any
	decoder := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 4<<20))
	decoder.UseNumber()
	if err := decoder.Decode(&tiers); err != nil {
		return fmt.Errorf("decode billing: %w", err)
	}
	if len(tiers) == 0 {
		return fmt.Errorf("billing returned no rules")
	}
	for tier, values := range tiers {
		if (tier != "BASIC" && tier != "PRO" && tier != "UNLIMITED") || values == nil {
			return fmt.Errorf("invalid billing tier")
		}
		for key, value := range values {
			if key != "max_patients" && key != "max_records_per_patient" {
				return fmt.Errorf("unsupported billing restriction")
			}
			if _, err := limitInteger(value); err != nil {
				return fmt.Errorf("invalid billing limit %s: %w", key, err)
			}
		}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("invalid trailing billing response")
	}
	r.mu.Lock()
	r.tiers = tiers
	r.loadedAt = time.Now()
	r.mu.Unlock()
	return nil
}

func limitInteger(value any) (int, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("limit must be numeric")
	}
	// Bound exact arithmetic on the remote payload before constructing a rational.
	literal := number.String()
	if len(literal) > 4096 {
		return 0, fmt.Errorf("invalid integer limit")
	}
	if index := strings.IndexAny(literal, "eE"); index >= 0 {
		exponent, err := strconv.ParseInt(literal[index+1:], 10, 32)
		if err != nil || exponent < -4096 || exponent > 4096 {
			return 0, fmt.Errorf("invalid integer limit")
		}
	}
	exact, ok := new(big.Rat).SetString(literal)
	if !ok || !exact.IsInt() || !exact.Num().IsInt64() {
		return 0, fmt.Errorf("invalid integer limit")
	}
	n := exact.Num().Int64()
	if n < -1 || n > 2147483647 {
		return 0, fmt.Errorf("invalid integer limit")
	}
	return int(n), nil
}

func (r *Rules) All() map[string]map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copy := make(map[string]map[string]any, len(r.tiers))
	for tier, values := range r.tiers {
		copy[tier] = make(map[string]any, len(values))
		for k, v := range values {
			copy[tier][k] = v
		}
	}
	return copy
}

func (r *Rules) Int(tier, key string, fallback int) int {
	tier = strings.ToUpper(strings.TrimSpace(tier))
	if tier == "FREE_TRIAL" {
		if key == "max_patients" {
			return 20
		}
		if key == "max_records_per_patient" {
			return 50
		}
		return fallback
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := r.tiers[tier]
	if values == nil {
		values = r.tiers["BASIC"]
	}
	value, ok := values[key]
	if !ok {
		return fallback
	}
	switch value := value.(type) {
	case int:
		return value
	case json.Number:
		n, err := limitInteger(value)
		if err == nil {
			return n
		}
	}
	return fallback
}

func Plan(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch value {
	case "FREE_TRIAL", "BASIC", "PRO", "UNLIMITED", "NONE":
		return value
	case "ENTERPRISE":
		return "UNLIMITED"
	default:
		return "NONE"
	}
}
