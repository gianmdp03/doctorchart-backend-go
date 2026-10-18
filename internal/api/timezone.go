package api

import (
	"context"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"
)

type timeZoneKey struct{}

func TimeZone(ctx context.Context) *time.Location {
	zone, _ := ctx.Value(timeZoneKey{}).(*time.Location)
	return zone
}

// A display zone is request context, never authority over server timestamps.
func WithTimeZone(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values, present := r.URL.Query()["timeZone"]
		if !present {
			next.ServeHTTP(w, r)
			return
		}
		if len(values) != 1 || len(values[0]) > 100 || values[0] == "" || values[0] == "Local" || strings.ContainsAny(values[0], "\\\x00") {
			WriteError(w, Invalid("La zona horaria no es válida."))
			return
		}
		zone, err := time.LoadLocation(values[0])
		if err != nil {
			WriteError(w, Invalid("La zona horaria no es válida."))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), timeZoneKey{}, zone)))
	})
}
