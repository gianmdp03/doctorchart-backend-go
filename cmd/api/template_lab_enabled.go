//go:build doctorchart_template_lab

package main

import (
	"errors"
	"os"

	"github.com/jackc/pgx/v5"
)

func configureTemplateLab(cfg *pgx.ConnConfig) error {
	if os.Getenv("DOCTORCHART_SYNTHETIC_TEMPLATE_REVIEWER") == "" {
		return nil
	}
	m6 := (cfg.Host == "pg18" || cfg.Host == "pg18-restore") && cfg.Database == "doctorchart_m6_e2e"
	local := cfg.Host == "doctorchart-preproduccion-db" && cfg.Database == "doctorchart"
	integratedQA := cfg.Host == "postgres" && cfg.Database == "doctorchart_local_current" &&
		os.Getenv("DOCTORCHART_LOCAL_TEMPLATE_REVIEW") == "1" &&
		os.Getenv("NOTIFICATIONS_PUBLIC_ORIGIN") == "https://localhost:4202" &&
		os.Getenv("MEDIA_ENVIRONMENT") == "development"
	if (!m6 && !local && !integratedQA) || len(cfg.Fallbacks) != 0 {
		return errors.New("synthetic template reviews require an isolated DoctorChart test database")
	}
	cfg.RuntimeParams["doctorchart.template_lab"] = "on"
	return nil
}
