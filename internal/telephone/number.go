package telephone

import (
	"regexp"
	"strings"

	"github.com/nyaruka/phonenumbers/v2"
)

var input = regexp.MustCompile(`^\+?[\p{Nd}\s().-]+$`)
var e164 = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)

// Normalize accepts international numbers and national numbers from the legacy
// Argentine clients. An empty optional value is returned unchanged.
func Normalize(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	if len(value) > 80 || !input.MatchString(value) {
		return "", false
	}
	parsed, err := phonenumbers.Parse(value, "AR")
	if err != nil || parsed.GetExtension() != "" || !phonenumbers.IsValidNumber(parsed) {
		return "", false
	}
	number := phonenumbers.Format(parsed, phonenumbers.E164)
	return number, e164.MatchString(number)
}
