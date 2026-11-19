//go:build doctorchart_template_lab

package main

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestIntegratedTemplateReviewRequiresLocalConfiguration(t *testing.T) {
	t.Setenv("DOCTORCHART_SYNTHETIC_TEMPLATE_REVIEWER", "local-qa-reviewer")
	t.Setenv("DOCTORCHART_LOCAL_TEMPLATE_REVIEW", "1")
	t.Setenv("NOTIFICATIONS_PUBLIC_ORIGIN", "https://localhost:4202")
	t.Setenv("MEDIA_ENVIRONMENT", "development")

	for _, tc := range []struct {
		name, host, database, env, value string
		allow                            bool
	}{
		{name: "local opt-in", host: "postgres", database: "doctorchart_local_current", allow: true},
		{name: "different database", host: "postgres", database: "doctorchart"},
		{name: "different host", host: "example.invalid", database: "doctorchart_local_current"},
		{name: "disabled", host: "postgres", database: "doctorchart_local_current", env: "DOCTORCHART_LOCAL_TEMPLATE_REVIEW"},
		{name: "public origin", host: "postgres", database: "doctorchart_local_current", env: "NOTIFICATIONS_PUBLIC_ORIGIN", value: "https://example.invalid"},
		{name: "production storage", host: "postgres", database: "doctorchart_local_current", env: "MEDIA_ENVIRONMENT", value: "production"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv(tc.env, tc.value)
			}
			cfg := &pgx.ConnConfig{}
			cfg.Host, cfg.Database = tc.host, tc.database
			cfg.RuntimeParams = map[string]string{}
			err := configureTemplateLab(cfg)
			if (err == nil) != tc.allow || (cfg.RuntimeParams["doctorchart.template_lab"] == "on") != tc.allow {
				t.Fatalf("allow=%t, setting=%q, err=%v", tc.allow, cfg.RuntimeParams["doctorchart.template_lab"], err)
			}
		})
	}
}
