package bootstrap

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var freshSchema string

var schemaTables = []string{
	"chart_definitions", "clinical_calculation_definitions", "clinical_encounters",
	"doctor_calculator_preferences", "doctor_calculator_selections", "doctor_chart_configs",
	"doctor_workspaces", "patient_calculation_logs", "patient_medical_studies",
	"patient_prescription_records", "patient_vaccinations", "patients",
	"prescription_diagnostic_orders", "prescription_medications", "specialties",
	"specialty_calculator_defaults", "study_file_deletions", "vaccination_calendars",
	"vaccination_doses", "wizard_defaults",
}

// EnsureSchema creates the baseline only when no DoctorChart relations exist.
// Existing tables are never altered, deleted or silently repaired.
func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin DoctorChart schema initialization: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL search_path = public; SET LOCAL lock_timeout = '10s'; SET LOCAL statement_timeout = '20s'"); err != nil {
		return fmt.Errorf("configure DoctorChart schema transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(17209,41000)"); err != nil {
		return fmt.Errorf("lock DoctorChart schema initialization: %w", err)
	}
	var relations, tables int
	if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE c.relkind IN ('r','p'))
		FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname='public' AND c.relname=ANY($1::text[])`, schemaTables).Scan(&relations, &tables); err != nil {
		return fmt.Errorf("inspect DoctorChart schema: %w", err)
	}
	if relations > 0 && (relations != len(schemaTables) || tables != len(schemaTables)) {
		return fmt.Errorf("esquema DoctorChart incompleto: hay %d de %d tablas; restaurá las tablas faltantes desde un respaldo. La inicialización automática requiere que no existan tablas de DoctorChart", tables, len(schemaTables))
	}
	created := relations == 0
	if created {
		var publicExists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname='public')").Scan(&publicExists); err != nil {
			return fmt.Errorf("inspect public schema: %w", err)
		}
		if !publicExists {
			if _, err := tx.Exec(ctx, "CREATE SCHEMA public"); err != nil {
				return fmt.Errorf("create public schema: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, freshSchema); err != nil {
			return fmt.Errorf("crear esquema DoctorChart: la base y el schema public necesitan permisos CREATE y nombres de objetos disponibles: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit DoctorChart schema initialization: %w", err)
	}
	if created {
		slog.Info("DoctorChart schema created", "tables", len(schemaTables))
	}
	return nil
}
