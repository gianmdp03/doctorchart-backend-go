package api

import (
	"fmt"
	"time"
)

// FormatInstant matches the ISO Instant representation used by Jackson: fractional
// digits appear in groups of three, even when the last digit is zero.
func FormatInstant(t time.Time) string {
	base := t.UTC().Format("2006-01-02T15:04:05")
	n := t.Nanosecond()
	if n == 0 {
		return base + "Z"
	}
	fraction := fmt.Sprintf("%09d", n)
	if n%1000000 == 0 {
		fraction = fraction[:3]
	} else if n%1000 == 0 {
		fraction = fraction[:6]
	}
	return base + "." + fraction + "Z"
}
