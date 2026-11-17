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
	if cfg.Host != "pg18" && cfg.Host != "pg18-restore" || cfg.Database != "doctorchart_m6_e2e" || len(cfg.Fallbacks) != 0 {
		return errors.New("synthetic template reviews require the isolated M6 lab database")
	}
	cfg.RuntimeParams["doctorchart.template_lab"] = "on"
	return nil
}
