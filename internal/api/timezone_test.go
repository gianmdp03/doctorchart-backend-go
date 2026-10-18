package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestTimeZoneRequestValidation(t *testing.T) {
	for _, name := range []string{"Europe/Madrid", "America/Argentina/Buenos_Aires", "America/Buenos_Aires", "UTC", "Asia/Kathmandu"} {
		called := false
		handler := WithTimeZone(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			if TimeZone(r.Context()) == nil || TimeZone(r.Context()).String() != name {
				t.Fatal("lost request zone")
			}
		}))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/?timeZone="+url.QueryEscape(name), nil))
		if response.Code != 200 || !called {
			t.Fatalf("valid zone %s rejected", name)
		}
	}
	for _, query := range []string{"timeZone=bad-zone", "timeZone=Local", "timeZone=", "timeZone=UTC&timeZone=Europe%2FMadrid", "timeZone=..%2F..%2Fetc%2Fpasswd"} {
		response := httptest.NewRecorder()
		WithTimeZone(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid zone reached handler") })).ServeHTTP(response, httptest.NewRequest("GET", "/?"+query, nil))
		if response.Code != 400 {
			t.Fatalf("invalid zone status=%d", response.Code)
		}
	}
}

func TestLocalDayUTCIntervalHandlesDST(t *testing.T) {
	zone, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range []struct {
		month      time.Month
		day, hours int
	}{{time.March, 29, 23}, {time.October, 25, 25}} {
		start := time.Date(2026, example.month, example.day, 0, 0, 0, 0, zone)
		end := start.AddDate(0, 0, 1)
		if end.Sub(start) != time.Duration(example.hours)*time.Hour {
			t.Fatal("calendar day assumed to be 24 hours")
		}
		if start.UTC().In(zone).Day() != example.day {
			t.Fatal("UTC boundary changed the local day")
		}
	}
}
