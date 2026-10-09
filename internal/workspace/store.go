package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

type Store struct {
	pool      *pgxpool.Pool
	validator *wizard.Validator
}

func New(pool *pgxpool.Pool, validator *wizard.Validator) *Store { return &Store{pool, validator} }

type Specialty struct {
	ID          int64    `json:"id"`
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description *string  `json:"description"`
	Active      bool     `json:"active"`
	Version     int64    `json:"version"`
	Navigation  []string `json:"-"`
	Tabs        []string `json:"-"`
	Actions     []string `json:"-"`
}
type Summary struct {
	ID          int64   `json:"id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

func (s Specialty) summary() Summary { return Summary{s.ID, s.Code, s.Name, s.Description} }

type Experience struct {
	SpecialtyID int64    `json:"specialtyId"`
	Version     int64    `json:"specialtyVersion"`
	Navigation  []string `json:"navigation"`
	Tabs        []string `json:"patientRecordTabs"`
	Actions     []string `json:"patientRecordActions"`
}

func (s Specialty) experience() Experience {
	return Experience{s.ID, s.Version, s.Navigation, s.Tabs, s.Actions}
}

type Default struct {
	SpecialtyID      int64           `json:"specialtyId"`
	Version          int64           `json:"version"`
	PublishedVersion int64           `json:"publishedVersion"`
	Draft            json.RawMessage `json:"draftDefinition"`
	Published        json.RawMessage `json:"publishedDefinition"`
}
type Setup struct {
	ID                 int64           `json:"id"`
	Version            int64           `json:"version"`
	Specialty          Summary         `json:"specialty"`
	SourceVersion      *int64          `json:"sourceWizardVersion"`
	DraftSourceVersion *int64          `json:"draftSourceWizardVersion"`
	Completed          bool            `json:"onboardingCompleted"`
	Step               string          `json:"currentStepKey"`
	Definition         json.RawMessage `json:"definition"`
	Draft              json.RawMessage `json:"draftDefinition"`
	Experience         Experience      `json:"experience"`
}
type Preferences struct {
	Version int64           `json:"version"`
	Columns json.RawMessage `json:"patientColumnsConfig"`
	Notes   *string         `json:"quickNotes"`
}
type workspace struct {
	Setup
	Preferences Preferences `json:"-"`
	UserID      string
	Created     time.Time
}

const specialtyColumns = "id,code,display_name,description,active,version,navigation,patient_record_tabs,patient_record_actions"

func scanSpecialty(row pgx.Row) (Specialty, error) {
	var s Specialty
	err := row.Scan(&s.ID, &s.Code, &s.Name, &s.Description, &s.Active, &s.Version, &s.Navigation, &s.Tabs, &s.Actions)
	return s, err
}
func specialty(ctx context.Context, tx pgx.Tx, id int64) (Specialty, error) {
	s, err := scanSpecialty(tx.QueryRow(ctx, "SELECT "+specialtyColumns+" FROM specialties WHERE id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return s, api.Failure(404, "SPECIALTY_NOT_FOUND", "La especialidad no está disponible.")
	}
	return s, err
}
func defaultWizard(ctx context.Context, tx pgx.Tx, id int64) (Default, error) {
	var d Default
	err := tx.QueryRow(ctx, "SELECT specialty_id,version,published_version,draft_definition,published_definition FROM wizard_defaults WHERE specialty_id=$1", id).Scan(&d.SpecialtyID, &d.Version, &d.PublishedVersion, &d.Draft, &d.Published)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, api.Failure(404, "SPECIALTY_NOT_FOUND", "La especialidad no está disponible.")
	}
	return d, err
}
func readWorkspace(ctx context.Context, tx pgx.Tx, userID string) (*workspace, error) {
	var w workspace
	var specialtyID int64
	err := tx.QueryRow(ctx, `SELECT id,version,specialty_id,source_wizard_version,draft_source_wizard_version,onboarding_completed,current_step_key,definition,draft_definition,patient_columns_config,quick_notes,created_at FROM doctor_workspaces WHERE user_id=$1`, userID).Scan(&w.ID, &w.Setup.Version, &specialtyID, &w.SourceVersion, &w.DraftSourceVersion, &w.Completed, &w.Step, &w.Definition, &w.Draft, &w.Preferences.Columns, &w.Preferences.Notes, &w.Created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s, err := specialty(ctx, tx, specialtyID)
	if err != nil {
		return nil, err
	}
	w.Specialty = s.summary()
	w.Experience = s.experience()
	w.Preferences.Version = w.Setup.Version
	w.UserID = userID
	return &w, nil
}
func requireWorkspace(w *workspace) error {
	if w == nil {
		return api.Failure(404, "WORKSPACE_NOT_FOUND", "No existe una configuración para este usuario.")
	}
	return nil
}
func version(actual int64, expected *int64, message string) error {
	if expected == nil || actual != *expected {
		return api.Failure(409, "VERSION_CONFLICT", message)
	}
	return nil
}

const workspaceConflict = "La configuración cambió. Volvé a cargarla antes de guardar."

func (s *Store) transaction(ctx context.Context, readOnly bool, operation func(pgx.Tx) error) error {
	options := pgx.TxOptions{}
	if readOnly {
		options.AccessMode = pgx.ReadOnly
	}
	tx, err := s.pool.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin workspace transaction: %w", err)
	}
	defer func() {
		err := tx.Rollback(ctx)
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) { /* Connection disposal is handled by pgx. */
			slog.Warn("workspace transaction rollback failed")
		}
	}()
	if err := operation(tx); err != nil {
		return databaseError(err)
	}
	return databaseError(tx.Commit(ctx))
}
func databaseError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return api.Failure(409, "VERSION_CONFLICT", "La configuración cambió. Actualizá los datos antes de guardar.")
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23503" || pg.Code == "23514" || pg.Code == "23502") {
		return api.Failure(409, "VERSION_CONFLICT", "La configuración cambió. Actualizá los datos antes de guardar.")
	}
	return err
}
func catalogLock(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(17209,41001)")
	return err
}

func (s *Store) Specialties(ctx context.Context, admin bool) ([]Specialty, error) {
	result := []Specialty{}
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		query := "SELECT " + specialtyColumns + " FROM specialties"
		if !admin {
			query += " WHERE active=true AND EXISTS(SELECT 1 FROM wizard_defaults w WHERE w.specialty_id=specialties.id AND w.published_definition IS NOT NULL)"
		}
		query += " ORDER BY display_name,id"
		rows, err := tx.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			specialty, err := scanSpecialty(rows)
			if err != nil {
				return err
			}
			result = append(result, specialty)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !admin && len(result) == 0 {
			return api.Failure(409, "SPECIALTY_NOT_AVAILABLE", "No hay especialidades publicadas disponibles.")
		}
		return nil
	})
	return result, err
}
func (s *Store) Wizard(ctx context.Context, id int64, published bool) (any, error) {
	var result any
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		d, err := defaultWizard(ctx, tx, id)
		if err != nil {
			return err
		}
		if !published {
			result = d
			return nil
		}
		specialty, err := specialty(ctx, tx, id)
		if err != nil {
			return err
		}
		if !specialty.Active || len(d.Published) == 0 {
			return api.Failure(404, "SPECIALTY_NOT_FOUND", "La especialidad no está disponible.")
		}
		result = struct {
			Specialty  Summary         `json:"specialty"`
			Version    int64           `json:"publishedVersion"`
			Definition json.RawMessage `json:"definition"`
		}{specialty.summary(), d.PublishedVersion, d.Published}
		return nil
	})
	return result, err
}
func (s *Store) Setup(ctx context.Context, userID string) (*Setup, error) {
	var result *Setup
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if w != nil {
			result = &w.Setup
		}
		return nil
	})
	return result, err
}
func (s *Store) Preferences(ctx context.Context, userID string) (Preferences, error) {
	var result Preferences
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if err := requireWorkspace(w); err != nil {
			return err
		}
		result = w.Preferences
		return nil
	})
	return result, err
}
func (s *Store) Profile(ctx context.Context, userID string) (any, error) {
	var result any
	err := s.transaction(ctx, true, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		if w == nil {
			return nil
		}
		result = struct {
			ID        int64   `json:"id"`
			UserID    string  `json:"userId"`
			Specialty Summary `json:"specialty"`
			Completed bool    `json:"isSpecialtyLocked"`
			Created   string  `json:"createdAt"`
		}{w.ID, userID, w.Specialty, w.Completed, api.FormatInstant(w.Created)}
		return nil
	})
	return result, err
}

func saveWorkspace(ctx context.Context, tx pgx.Tx, w *workspace) error {
	var row pgx.Row
	if w.ID == 0 {
		row = tx.QueryRow(ctx, `INSERT INTO doctor_workspaces(user_id,specialty_id,definition,draft_definition,source_wizard_version,draft_source_wizard_version,current_step_key,onboarding_completed,patient_columns_config,quick_notes,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,0,clock_timestamp(),clock_timestamp()) RETURNING id,version,created_at`, w.UserID, w.Specialty.ID, w.Definition, w.Draft, w.SourceVersion, w.DraftSourceVersion, w.Step, w.Completed, w.Preferences.Columns, w.Preferences.Notes)
	} else {
		row = tx.QueryRow(ctx, `UPDATE doctor_workspaces SET specialty_id=$2,definition=$3,draft_definition=$4,source_wizard_version=$5,draft_source_wizard_version=$6,current_step_key=$7,onboarding_completed=$8,patient_columns_config=$9,quick_notes=$10,version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND version=$11 RETURNING id,version,created_at`, w.ID, w.Specialty.ID, w.Definition, w.Draft, w.SourceVersion, w.DraftSourceVersion, w.Step, w.Completed, w.Preferences.Columns, w.Preferences.Notes, w.Setup.Version)
	}
	if err := row.Scan(&w.ID, &w.Setup.Version, &w.Created); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return api.Failure(409, "VERSION_CONFLICT", "La configuración cambió. Actualizá los datos antes de guardar.")
		}
		return err
	}
	w.Preferences.Version = w.Setup.Version
	return nil
}

func (s *Store) RequireFeature(ctx context.Context, userID, feature string) error {
	return s.transaction(ctx, true, func(tx pgx.Tx) error {
		w, err := readWorkspace(ctx, tx, userID)
		if err != nil {
			return err
		}
		return featureEnabled(w, feature)
	})
}

// Effective reads the personal snapshot under the same transaction as record creation.
func (s *Store) Effective(ctx context.Context, tx pgx.Tx, userID string, expected int64) (*Setup, error) {
	var id int64
	err := tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE user_id=$1 FOR SHARE", userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, api.Failure(409, "ONBOARDING_REQUIRED", "Completá la configuración antes de crear registros.")
	}
	if err != nil {
		return nil, err
	}
	w, err := readWorkspace(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if !w.Completed || len(w.Definition) == 0 {
		return nil, api.Failure(409, "ONBOARDING_REQUIRED", "Completá la configuración antes de crear registros.")
	}
	if expected < 0 || expected != w.Setup.Version {
		return nil, api.Failure(409, "WORKSPACE_VERSION_CONFLICT", "La configuración cambió. Actualizala antes de guardar.")
	}
	return &w.Setup, nil
}
func (s *Store) Feature(ctx context.Context, tx pgx.Tx, userID, feature string) error {
	w, err := readWorkspace(ctx, tx, userID)
	if err != nil {
		return err
	}
	return featureEnabled(w, feature)
}
func featureEnabled(w *workspace, feature string) error {
	if w == nil || !w.Completed || len(w.Definition) == 0 {
		return api.Failure(409, "ONBOARDING_REQUIRED", "Completá la configuración antes de usar funciones clínicas.")
	}
	definition, err := wizard.Decode(w.Definition)
	if err != nil {
		return err
	}
	modules, _ := definition["modulesConfig"].(map[string]any)
	enabled := false
	switch feature {
	case "AI_ASSISTANT", "MEDICATION_CATALOG":
		enabled = modules[feature] == true
	case "GROWTH":
		enabled = contains(w.Experience.Tabs, "GROWTH") && modules["GROWTH_CHARTS"] == true
	case "CALCULATORS":
		enabled = contains(w.Experience.Navigation, "CALCULATORS")
	case "VACCINATION_CALENDAR":
		enabled = contains(w.Experience.Navigation, "VACCINATION")
	case "PATIENT_VACCINATION":
		enabled = contains(w.Experience.Tabs, "VACCINATION")
	case "STUDIES", "PRESCRIPTIONS":
		enabled = contains(w.Experience.Tabs, feature)
	case "EDIT_PATIENT", "NEW_ENCOUNTER", "PRINT_DOCUMENT":
		enabled = contains(w.Experience.Actions, feature)
	}
	if !enabled {
		return api.Failure(403, "FEATURE_UNAVAILABLE", "Esta función no está habilitada en tu configuración clínica.")
	}
	return nil
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
