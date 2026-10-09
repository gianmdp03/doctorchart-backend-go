package clinical

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
	"golang.org/x/text/unicode/norm"
)

//go:embed medications.json
var medicationsJSON []byte

type Medication struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Generic      string `json:"genericName"`
	Presentation string `json:"presentation"`
	Dose         string `json:"defaultDose"`
	Route        string `json:"administrationRoute"`
	Frequency    string `json:"typicalFrequency"`
	Duration     int    `json:"typicalDurationDays"`
	Category     string `json:"category"`
}

func normalizeMedication(value string) string {
	var text strings.Builder
	for _, r := range norm.NFD.String(value) {
		if r < 0x0300 || r > 0x036f {
			text.WriteRune(r)
		}
	}
	return strings.ToLower(text.String())
}

type Chart struct {
	ID          int64           `json:"id"`
	Code        string          `json:"chartCode"`
	Title       string          `json:"title"`
	Type        string          `json:"chartType"`
	Description *string         `json:"description"`
	Slots       json.RawMessage `json:"requiredSlots"`
	System      bool            `json:"isSystem"`
}
type ChartConfig struct {
	ID       int64           `json:"id"`
	Code     string          `json:"chartCode"`
	Mappings json.RawMessage `json:"fieldMappings"`
	Active   bool            `json:"isActive"`
}

func (s *Service) CatalogRoutes(mux *http.ServeMux) error {
	var medications []Medication
	if err := json.Unmarshal(medicationsJSON, &medications); err != nil {
		return err
	}
	mux.HandleFunc("GET /api/doctorchart/medications", func(w http.ResponseWriter, r *http.Request) {
		if err := s.workspaces.RequireFeature(r.Context(), api.User(r.Context()).UserID, "MEDICATION_CATALOG"); err != nil {
			api.WriteError(w, err)
			return
		}
		api.WriteJSON(w, 200, medications)
	})
	mux.HandleFunc("GET /api/doctorchart/medications/search", func(w http.ResponseWriter, r *http.Request) {
		limit := 10
		if value := r.URL.Query().Get("limit"); value != "" {
			n, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				api.WriteError(w, api.Invalid("La solicitud contiene datos inválidos."))
				return
			}
			limit = int(n)
		}
		if err := s.workspaces.RequireFeature(r.Context(), api.User(r.Context()).UserID, "MEDICATION_CATALOG"); err != nil {
			api.WriteError(w, err)
			return
		}
		if limit < 1 {
			limit = 1
		}
		if limit > 50 {
			limit = 50
		}
		query := normalizeMedication(strings.TrimSpace(r.URL.Query().Get("query")))
		result := []Medication{}
		for _, medicine := range medications {
			if query == "" || strings.Contains(normalizeMedication(medicine.Name), query) || strings.Contains(normalizeMedication(medicine.Generic), query) || strings.Contains(normalizeMedication(medicine.Category), query) {
				result = append(result, medicine)
				if len(result) == limit {
					break
				}
			}
		}
		api.WriteJSON(w, 200, result)
	})
	mux.HandleFunc("GET /api/doctorchart/vaccination-calendar", func(w http.ResponseWriter, r *http.Request) {
		var result json.RawMessage
		err := s.transaction(r.Context(), true, func(tx pgx.Tx) error {
			if err := s.workspaces.Feature(r.Context(), tx, api.User(r.Context()).UserID, "VACCINATION_CALENDAR"); err != nil {
				return err
			}
			err := tx.QueryRow(r.Context(), "SELECT definition FROM vaccination_calendars WHERE id=$1", "argentina-2026-v1").Scan(&result)
			if errors.Is(err, pgx.ErrNoRows) {
				return api.Failure(503, "VACCINATION_CALENDAR_UNAVAILABLE", "No se pudo cargar el calendario de vacunación.")
			}
			return err
		})
		if err != nil {
			api.WriteError(w, err)
			return
		}
		api.WriteJSON(w, 200, result)
	})
	mux.HandleFunc("GET /api/doctorchart/charts", func(w http.ResponseWriter, r *http.Request) {
		result := []Chart{}
		err := s.transaction(r.Context(), true, func(tx pgx.Tx) error {
			if err := s.workspaces.Feature(r.Context(), tx, api.User(r.Context()).UserID, "GROWTH"); err != nil {
				return err
			}
			rows, err := tx.Query(r.Context(), "SELECT id,chart_code,title,chart_type,description,required_slots,is_system FROM chart_definitions WHERE is_system=true")
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var chart Chart
				if err := rows.Scan(&chart.ID, &chart.Code, &chart.Title, &chart.Type, &chart.Description, &chart.Slots, &chart.System); err != nil {
					return err
				}
				result = append(result, chart)
			}
			return rows.Err()
		})
		if err != nil {
			api.WriteError(w, err)
			return
		}
		api.WriteJSON(w, 200, result)
	})
	mux.HandleFunc("GET /api/doctorchart/workspace/charts", func(w http.ResponseWriter, r *http.Request) {
		result := []ChartConfig{}
		err := s.transaction(r.Context(), true, func(tx pgx.Tx) error {
			userID := api.User(r.Context()).UserID
			if err := s.workspaces.Feature(r.Context(), tx, userID, "GROWTH"); err != nil {
				return err
			}
			rows, err := tx.Query(r.Context(), "SELECT id,chart_code,field_mappings,is_active FROM doctor_chart_configs WHERE user_id=$1", userID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var chart ChartConfig
				if err := rows.Scan(&chart.ID, &chart.Code, &chart.Mappings, &chart.Active); err != nil {
					return err
				}
				result = append(result, chart)
			}
			return rows.Err()
		})
		if err != nil {
			api.WriteError(w, err)
			return
		}
		api.WriteJSON(w, 200, result)
	})
	mux.HandleFunc("PUT /api/doctorchart/workspace/charts", func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 26<<20))
		var requests []struct {
			Code     string             `json:"chartCode"`
			Mappings map[string]*string `json:"fieldMappings"`
			Active   bool               `json:"isActive"`
		}
		if err := decoder.Decode(&requests); err != nil {
			api.WriteError(w, api.Invalid("La solicitud contiene datos inválidos."))
			return
		}
		result := []ChartConfig{}
		err := s.transaction(r.Context(), false, func(tx pgx.Tx) error {
			userID := api.User(r.Context()).UserID
			if err := s.workspaces.Feature(r.Context(), tx, userID, "GROWTH"); err != nil {
				return err
			}
			for _, request := range requests {
				encoded, err := json.Marshal(request.Mappings)
				if err != nil {
					return err
				}
				var chart ChartConfig
				err = tx.QueryRow(r.Context(), "SELECT id FROM doctor_chart_configs WHERE user_id=$1 AND chart_code=$2", userID, request.Code).Scan(&chart.ID)
				if errors.Is(err, pgx.ErrNoRows) {
					err = tx.QueryRow(r.Context(), "INSERT INTO doctor_chart_configs(user_id,chart_code,field_mappings,is_active) VALUES($1,$2,$3,$4) RETURNING id,chart_code,field_mappings,is_active", userID, request.Code, encoded, request.Active).Scan(&chart.ID, &chart.Code, &chart.Mappings, &chart.Active)
				} else if err == nil {
					err = tx.QueryRow(r.Context(), "UPDATE doctor_chart_configs SET field_mappings=$2,is_active=$3 WHERE id=$1 RETURNING id,chart_code,field_mappings,is_active", chart.ID, encoded, request.Active).Scan(&chart.ID, &chart.Code, &chart.Mappings, &chart.Active)
				}
				if err != nil {
					return err
				}
				result = append(result, chart)
			}
			return nil
		})
		if err != nil {
			api.WriteError(w, err)
			return
		}
		api.WriteJSON(w, 200, result)
	})
	return nil
}
