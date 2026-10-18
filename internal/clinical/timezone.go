package clinical

import (
	"context"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"time"
)

func (s *Service) viewZone(ctx context.Context) *time.Location {
	if zone := api.TimeZone(ctx); zone != nil {
		return zone
	}
	// Old clients retain their established day boundaries until they send a zone.
	if s.zone != nil {
		return s.zone
	}
	return time.UTC
}
func (s *Service) documentZone(name string) *time.Location {
	if name != "" && name != "Local" {
		if zone, err := time.LoadLocation(name); err == nil {
			return zone
		}
	}
	if s.zone != nil {
		return s.zone
	}
	return time.UTC
}

func (s *Service) calculateForZone(ctx context.Context, code string, input map[string]any, now time.Time) (calculationResult, error) {
	local := *s
	local.zone = s.viewZone(ctx)
	return local.calculate(code, input, now)
}
