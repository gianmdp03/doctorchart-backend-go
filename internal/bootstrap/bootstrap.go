// Package bootstrap initializes empty catalogs without repairing or overwriting
// persisted configuration. It never creates or changes the PostgreSQL schema.
package bootstrap

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/wizard"
)

// Captured from the pinned Spring startup in the owned empty reference database.
// Includes Jackson's explicit nulls and Hibernate's initial version values.
//
//go:embed seed.json
var seedJSON []byte

func Run(ctx context.Context, pool *pgxpool.Pool, validator *wizard.Validator) error {
	var seeds map[string][]map[string]any
	decoder := json.NewDecoder(bytes.NewReader(seedJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&seeds); err != nil {
		return fmt.Errorf("decode bootstrap definitions: %w", err)
	}
	transaction := func(run func(pgx.Tx) error) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if err = run(tx); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err := transaction(func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(17209,41001)"); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM specialties").Scan(&count); err != nil {
			return err
		}
		specialtyIDs := map[string]int64{}
		calculatorIDs := map[string]int64{}
		if count > 0 {
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM specialties s JOIN wizard_defaults w ON w.specialty_id=s.id WHERE s.active=true AND w.published_definition IS NOT NULL AND w.published_version>0").Scan(&count); err != nil {
				return err
			}
			if count == 0 {
				return errors.New("Specialty catalog exists but has no active published wizard")
			}
		} else {
			for _, row := range seeds["specialties"] {
				old := fmt.Sprint(row["id"])
				var definition map[string]any
				for _, w := range seeds["wizard_defaults"] {
					if fmt.Sprint(w["specialty_id"]) == old {
						definition = w
					}
				}
				if definition == nil {
					return errors.New("missing specialty bootstrap wizard")
				}
				if err := validator.Validate(definition["draft_definition"].(map[string]any)); err != nil {
					return fmt.Errorf("invalid specialty bootstrap wizard: %w", err)
				}
				now := time.Now().UTC().Truncate(time.Microsecond)
				row["created_at"] = now
				row["updated_at"] = now
				row["active"] = false
				row["version"] = int64(0)
				id, err := insert(ctx, tx, "specialties", []string{"code", "display_name", "description", "active", "navigation", "patient_record_tabs", "patient_record_actions", "created_at", "updated_at", "version"}, row)
				if err != nil {
					return err
				}
				specialtyIDs[old] = id
				definition["specialty_id"] = id
				definition["updated_at"] = time.Now().UTC().Truncate(time.Microsecond)
				if _, err = insert(ctx, tx, "wizard_defaults", []string{"specialty_id", "draft_definition", "published_definition", "published_version", "version", "updated_at"}, definition); err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, "UPDATE specialties SET active=true,version=1,updated_at=$2 WHERE id=$1", id, time.Now().UTC().Truncate(time.Microsecond)); err != nil {
					return err
				}
			}
		}
		for _, row := range seeds["chart_definitions"] {
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM chart_definitions WHERE chart_code=$1)", row["chart_code"]).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				if _, err := insert(ctx, tx, "chart_definitions", []string{"chart_code", "title", "chart_type", "description", "required_slots", "is_system"}, row); err != nil {
					return err
				}
			}
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM clinical_calculation_definitions").Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		for _, row := range seeds["clinical_calculation_definitions"] {
			id, err := insert(ctx, tx, "clinical_calculation_definitions", []string{"code", "name", "short_name", "category", "category_label", "description", "evidence_reference", "summary_formula", "required_parameters", "result_unit", "is_system", "display_order"}, row)
			if err != nil {
				return err
			}
			calculatorIDs[fmt.Sprint(row["id"])] = id
		}
		for _, row := range seeds["specialty_calculator_defaults"] {
			oldSpecialty := fmt.Sprint(row["specialty_id"])
			id := specialtyIDs[oldSpecialty]
			if id == 0 {
				var code any
				for _, specialty := range seeds["specialties"] {
					if fmt.Sprint(specialty["id"]) == oldSpecialty {
						code = specialty["code"]
					}
				}
				if err := tx.QueryRow(ctx, "SELECT id FROM specialties WHERE code=$1", code).Scan(&id); err != nil {
					return errors.New("Unknown specialty seed")
				}
			}
			row["specialty_id"] = id
			row["calculator_id"] = calculatorIDs[fmt.Sprint(row["calculator_id"])]
			if _, err := insert(ctx, tx, "specialty_calculator_defaults", []string{"specialty_id", "calculator_id", "position"}, row); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("initialize specialty/chart/calculator catalogs: %w", err)
	}
	// VaccinationConfiguration is a separate Spring runner/transaction.
	return transaction(func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(742010)"); err != nil {
			return err
		}
		calendar := seeds["vaccination_calendars"][0]
		var existing []byte
		err := tx.QueryRow(ctx, "SELECT definition FROM vaccination_calendars WHERE id=$1", calendar["id"]).Scan(&existing)
		if err == nil {
			expected, _ := json.Marshal(calendar["definition"])
			var actualDTO, expectedDTO calendarDTO
			calendarDecoder := json.NewDecoder(bytes.NewReader(existing))
			calendarDecoder.DisallowUnknownFields()
			if err = calendarDecoder.Decode(&actualDTO); err != nil {
				return err
			}
			if err = json.Unmarshal(expected, &expectedDTO); err != nil {
				return err
			}
			if !reflect.DeepEqual(actualDTO, expectedDTO) {
				return errors.New("La versión publicada del calendario es inmutable; publicá una versión nueva.")
			}
			for _, dose := range seeds["vaccination_doses"] {
				var present bool
				if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM vaccination_doses WHERE id=$1 AND calendar_id=$2)", dose["id"], calendar["id"]).Scan(&present); err != nil {
					return err
				}
				if !present {
					return errors.New("El catálogo de dosis está incompleto.")
				}
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		for _, dose := range seeds["vaccination_doses"] {
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM vaccination_doses WHERE id=$1)", dose["id"]).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return errors.New("El identificador de dosis pertenece a otra versión del calendario.")
			}
		}
		raw, err := json.Marshal(calendar["definition"])
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO vaccination_calendars(id,definition) VALUES($1,$2)", calendar["id"], raw); err != nil {
			return err
		}
		for _, dose := range seeds["vaccination_doses"] {
			if _, err = tx.Exec(ctx, "INSERT INTO vaccination_doses(id,calendar_id) VALUES($1,$2)", dose["id"], calendar["id"]); err != nil {
				return err
			}
		}
		return nil
	})
}

// Identifiers here are fixed call-site literals, never request/configuration data.
// Sequence values remain PostgreSQL-owned; captured reference IDs are remapped.
func insert(ctx context.Context, tx pgx.Tx, table string, columns []string, row map[string]any) (int64, error) {
	values := make([]any, len(columns))
	placeholders := make([]string, len(columns))
	names := make([]string, len(columns))
	for index, column := range columns {
		value := row[column]
		switch typed := value.(type) {
		case map[string]any, []any:
			raw, err := json.Marshal(typed)
			if err != nil {
				return 0, err
			}
			value = raw
		case json.Number:
			number, err := typed.Int64()
			if err != nil {
				return 0, err
			}
			value = number
		}
		values[index] = value
		placeholders[index] = fmt.Sprintf("$%d", index+1)
		names[index] = pgx.Identifier{column}.Sanitize()
	}
	var id int64
	err := tx.QueryRow(ctx, "INSERT INTO "+pgx.Identifier{table}.Sanitize()+"("+strings.Join(names, ",")+") VALUES("+strings.Join(placeholders, ",")+") RETURNING id", values...).Scan(&id)
	return id, err
}

// Hibernate's Jackson mapper rejects unknown record fields; map-valued metadata
// remains unrestricted. This differs from the HTTP mapper's configuration.
type calendarDTO struct {
	CalendarID string         `json:"calendarId"`
	Metadata   map[string]any `json:"metadata"`
	Groups     []struct {
		Group   *string `json:"grupo"`
		Columns []struct {
			ID       *string `json:"id"`
			Name     *string `json:"nombre"`
			Subtitle *string `json:"subtitulo"`
			Order    int     `json:"orden"`
		} `json:"columnas"`
	} `json:"grupos_etarios_columnas"`
	References map[string]*string `json:"referencias_patologias"`
	Notes      map[string]*string `json:"notas_aplicacion"`
	Vaccines   []struct {
		ID        *string `json:"id"`
		Name      *string `json:"nombre"`
		Category  *string `json:"categoria"`
		Reference *struct {
			Number      int     `json:"numero"`
			Description *string `json:"descripcion"`
		} `json:"referencia_patologia"`
		Applications []struct {
			ID      *string   `json:"id"`
			Columns []*string `json:"columnas"`
			Dose    *string   `json:"dosis"`
			Type    *string   `json:"tipo"`
			Notes   []struct {
				Letter    *string `json:"letra"`
				Condition *string `json:"condicion"`
			} `json:"notas"`
			Age *struct {
				From  int `json:"fromMonths"`
				To    int `json:"toMonths"`
				Grace int `json:"graceMonths"`
			} `json:"ageRule"`
		} `json:"aplicaciones"`
	} `json:"vacunas"`
}
