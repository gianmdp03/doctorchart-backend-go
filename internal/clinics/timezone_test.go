package clinics

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/syntah/doctorchart-backend-go/internal/api"
)

var timezoneCases = []struct {
	zone  string
	valid bool
}{
	{"UTC", true}, {" UTC ", true},
	{"America/Argentina/Buenos_Aires", true}, {"America/New_York", true},
	{"Europe/Madrid", true}, {"Asia/Kolkata", true}, {"Pacific/Auckland", true},
	{"America/Coyhaique", true}, {"Africa/Abidjan", true},
	{"Local", false}, {"GMT", false}, {"ART", false}, {"EST", false},
	{"+03:00", false}, {"UTC-3", false}, {"Mars/Olympus", false},
	{"posix/America/New_York", false}, {"right/America/New_York", false},
	{"Etc/GMT+3", false}, {"Etc/GMT-3", false}, {"Etc/GMT+12", false},
	{"Etc/GMT-14", false}, {"Etc/GMT0", false}, {"Etc/UTC", false}, {"Etc/GMT", false},
	{"america/argentina/buenos_aires", false},
}

func TestClinicTimezoneValidation(t *testing.T) {
	for _, tc := range timezoneCases {
		t.Run(tc.zone, func(t *testing.T) {
			fields, _, err := values(map[string]any{"timezone": tc.zone, "expectedVersion": "0"}, "edit")
			if tc.valid {
				if err != nil || fields["timezone"] != strings.TrimSpace(tc.zone) {
					t.Fatal(fields, err)
				}
			} else if failure, ok := err.(*api.Error); !ok || failure.Status != 400 || len(failure.Errors) != 1 || failure.Errors[0].Path != "timezone" || failure.Errors[0].Code != "INVALID_TIMEZONE" {
				t.Fatal("expected INVALID_TIMEZONE field error", err)
			}
		})
	}
}

func TestClinicTimezonePolicyMatchesPostgreSQL(t *testing.T) {
	f := newFixture(t)
	f.limit.Store(-1)
	for _, tc := range timezoneCases {
		t.Run(tc.zone, func(t *testing.T) {
			var valid bool
			if err := f.pool.QueryRow(t.Context(), "SELECT public.is_iana_timezone($1)", strings.TrimSpace(tc.zone)).Scan(&valid); err != nil || valid != tc.valid {
				t.Fatal("SQL policy differs", valid, err)
			}
			body := strings.Replace(createBody, "America/Argentina/Buenos_Aires", tc.zone, 1)
			reply := f.request("POST", "/clinics", owner, body)
			if !tc.valid {
				assertCode(t, reply, 400, "INVALID_REQUEST")
				var failure api.Error
				if err := json.Unmarshal(reply.Body.Bytes(), &failure); err != nil || len(failure.Errors) != 1 || failure.Errors[0].Code != "INVALID_TIMEZONE" {
					t.Fatal("missing timezone field error", err)
				}
				return
			}
			assertCode(t, reply, 201, "")
			var created Clinic
			if err := json.Unmarshal(reply.Body.Bytes(), &created); err != nil || created.Timezone != strings.TrimSpace(tc.zone) {
				t.Fatal("timezone was reinterpreted", created, err)
			}
		})
	}
}

func TestClinicsListDoesNotNeedBrowserTimezone(t *testing.T) {
	f := newFixture(t)
	assertCode(t, f.request("POST", "/clinics", owner, strings.Replace(createBody, "America/Argentina/Buenos_Aires", "UTC", 1)), 201, "")
	reply := f.request("GET", "/clinics?page=0&size=20", owner, "")
	assertCode(t, reply, 200, "")
	var list struct {
		Items []Clinic `json:"items"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &list); err != nil || len(list.Items) != 1 || list.Items[0].Timezone != "UTC" || list.Items[0].CreatedAt.Location().String() != "UTC" {
		t.Fatal("listing requires device presentation context", err)
	}
	// The agreed fix belongs in Angular's interceptor, not an API extension.
	for _, query := range []string{"?timeZone=UTC", "?timeZone=Europe%2FMadrid", "?ownerId=other"} {
		assertCode(t, f.request("GET", "/clinics"+query, owner, ""), 400, "INVALID_REQUEST")
	}
}
