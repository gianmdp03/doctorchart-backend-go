package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Rules struct {
	mu     sync.RWMutex
	reload sync.Mutex
	tiers  map[string]map[string]any
	url    string
	client *http.Client
}

func New(url string) *Rules {
	return &Rules{url: strings.TrimSuffix(url, "/") + "/api/billing/limits/DOCTORCHART", client: &http.Client{Timeout: 6 * time.Second}, tiers: map[string]map[string]any{
		"BASIC":     {"max_patients": 50, "max_records_per_patient": 100},
		"PRO":       {"max_patients": 500, "max_records_per_patient": 1000},
		"UNLIMITED": {"max_patients": -1, "max_records_per_patient": -1},
	}}
}

func (r *Rules) Load(ctx context.Context) {
	for attempt := 0; attempt < 3; attempt++ {
		populated, err := r.fetch(ctx)
		if populated {
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

// Reload preserves current rules if Billing fails or returns an empty catalog.
func (r *Rules) Reload(ctx context.Context) error {
	r.reload.Lock()
	defer r.reload.Unlock()
	_, err := r.fetch(ctx)
	return err
}

func (r *Rules) fetch(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return false, err
	}
	response, err := r.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("fetch billing: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return false, fmt.Errorf("billing status %d", response.StatusCode)
	}
	var tiers map[string]map[string]any
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&tiers); err != nil {
		return false, fmt.Errorf("decode billing: %w", err)
	}
	if len(tiers) == 0 {
		return false, nil
	}
	r.mu.Lock()
	r.tiers = tiers
	r.mu.Unlock()
	return true, nil
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
		n, err := value.Float64()
		if err == nil {
			return int(int32(int64(n)))
		}
	case string:
		n, err := strconv.ParseInt(value, 10, 32)
		if err == nil {
			return int(n)
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
