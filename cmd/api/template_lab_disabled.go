//go:build !doctorchart_template_lab

package main

import (
	"errors"
	"os"

	"github.com/jackc/pgx/v5"
)

func configureTemplateLab(_ *pgx.ConnConfig) error {
	if os.Getenv("DOCTORCHART_SYNTHETIC_TEMPLATE_REVIEWER") != "" {
		return errors.New("synthetic template reviewers require the isolated lab build")
	}
	return nil
}
