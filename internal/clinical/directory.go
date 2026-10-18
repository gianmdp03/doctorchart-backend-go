package clinical

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type PatientList struct {
	ID         int64   `json:"id"`
	Name       string  `json:"fullName"`
	Birth      string  `json:"birthDate"`
	NationalID *string `json:"nationalId"`
	Phone      *string `json:"phone"`
	Gender     *string `json:"gender"`
	Encounters int64   `json:"totalEncounters"`
	LastDate   *string `json:"lastEncounterDate"`
	Months     *int    `json:"ageInMonths"`
	Age        string  `json:"ageDisplay"`
	Group      string  `json:"ageGroup"`
	Pending    bool    `json:"hasPendingVaccine"`
}
type pagination struct {
	number, size int64
	order        string
}

func listError(message string) error { return api.Failure(400, "INVALID_LIST_QUERY", message) }
func listingPage(query url.Values) (pagination, error) {
	return pageFor(query, map[string]string{"fullName": "full_name", "birthDate": "birth_date", "id": "id"}, "p", "full_name", "ASC")
}
func pageFor(query url.Values, fields map[string]string, prefix, defaultColumn, defaultDirection string) (pagination, error) {
	page := pagination{number: 0, size: 20}
	if n, err := strconv.ParseInt(query.Get("page"), 10, 32); err == nil && n >= 0 {
		page.number = n
	}
	if n, err := strconv.ParseInt(query.Get("size"), 10, 32); err == nil && n > 0 {
		page.size = n
	}
	if page.size > 2000 {
		page.size = 2000
	}
	if page.size > 100 || page.number*page.size > math.MaxInt32 {
		return page, listError("La página debe tener entre 1 y 100 registros.")
	}
	orders := []string{}
	hasID := false
	firstDirection := defaultDirection
	for _, raw := range query["sort"] {
		parts := strings.Split(raw, ",")
		direction := "ASC"
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			if strings.EqualFold(last, "ignorecase") {
				return page, listError("El orden solicitado no está disponible.")
			}
			if strings.EqualFold(last, "asc") || strings.EqualFold(last, "desc") {
				direction = strings.ToUpper(last)
				parts = parts[:len(parts)-1]
			}
		}
		for _, property := range parts {
			if property == "" {
				continue
			}
			column, ok := fields[property]
			if !ok {
				return page, listError("El orden solicitado no está disponible.")
			}
			if property == "id" {
				hasID = true
			}
			if len(orders) == 0 {
				firstDirection = direction
			}
			orders = append(orders, prefix+"."+column+" "+direction)
		}
	}
	if len(orders) == 0 {
		orders = append(orders, prefix+"."+defaultColumn+" "+defaultDirection)
	}
	if !hasID {
		orders = append(orders, prefix+".id "+firstDirection)
	}
	page.order = strings.Join(orders, ",")
	return page, nil
}
func ageMonths(birth, today time.Time) *int {
	if birth.After(today) {
		return nil
	}
	months := (today.Year()-birth.Year())*12 + int(today.Month()) - int(birth.Month())
	if today.Day() < birth.Day() {
		months--
	}
	return &months
}
func directoryAge(birth, today time.Time, months *int) string {
	if months == nil {
		return "—"
	}
	if *months == 0 {
		days := int(today.Sub(birth).Hours() / 24)
		if days < 1 {
			days = 1
		}
		return fmt.Sprintf("%d días", days)
	}
	if *months == 1 {
		return "1 mes"
	}
	if *months < 24 {
		return fmt.Sprintf("%d meses", *months)
	}
	label := fmt.Sprintf("%d años", *months/12)
	if *months%12 > 0 {
		label += fmt.Sprintf(" %dm", *months%12)
	}
	return label
}
func ageGroup(months *int) string {
	if months == nil {
		return "UNKNOWN"
	}
	switch {
	case *months >= 216:
		return "ADULT"
	case *months <= 1:
		return "NEONATAL"
	case *months <= 12:
		return "INFANT"
	case *months <= 144:
		return "CHILD"
	default:
		return "ADOLESCENT"
	}
}

func (s *Service) ListPatients(ctx context.Context, userID string, query url.Values) (any, error) {
	page, err := listingPage(query)
	if err != nil {
		return nil, err
	}
	search := strings.TrimSpace(query.Get("search"))
	if api.JavaLength(search) > 200 {
		return nil, listError("La búsqueda admite hasta 200 caracteres.")
	}
	pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search)) + "%"
	filter := query.Get("filter")
	if filter == "" {
		filter = "ALL"
	}
	category := "true"
	switch filter {
	case "ALL":
	case "ADULT":
		category = "p.birth_date<=($3::date-interval '216 months')::date"
	case "MINOR":
		category = "p.birth_date>($3::date-interval '216 months')::date AND p.birth_date<=$3::date"
	case "INFANT":
		category = "p.birth_date>($3::date-interval '13 months')::date AND p.birth_date<=$3::date"
	case "TODDLER":
		category = "p.birth_date<=($3::date-interval '13 months')::date AND p.birth_date>($3::date-interval '73 months')::date"
	case "VACCINE_PENDING":
		category = "pending"
	default:
		return nil, api.Invalid("La solicitud contiene datos inválidos.")
	}
	todayText := time.Now().In(s.viewZone(ctx)).Format("2006-01-02")
	today, err := time.Parse("2006-01-02", todayText)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Warn("directory rollback failed")
		}
	}()
	var calendar json.RawMessage
	err = tx.QueryRow(ctx, "SELECT definition FROM vaccination_calendars WHERE id=$1", "argentina-2026-v1").Scan(&calendar)
	if err == pgx.ErrNoRows {
		return nil, api.Failure(503, "VACCINATION_CALENDAR_UNAVAILABLE", "No se pudo cargar el calendario de vacunación.")
	}
	if err != nil {
		return nil, err
	}
	pending := `EXISTS(SELECT 1 FROM vaccination_calendars c CROSS JOIN LATERAL jsonb_array_elements(c.definition->'vacunas') v CROSS JOIN LATERAL jsonb_array_elements(v->'aplicaciones') a WHERE c.id='argentina-2026-v1' AND a->'ageRule'<>'null'::jsonb AND p.birth_date<=($3::date-make_interval(months=>(a->'ageRule'->>'toMonths')::int+(a->'ageRule'->>'graceMonths')::int+1))::date AND NOT EXISTS(SELECT 1 FROM patient_vaccinations applied WHERE applied.patient_id=p.id AND applied.dose_id=a->>'id'))`
	if category == "pending" {
		category = pending
	}
	where := `$3::date IS NOT NULL AND p.user_id=$1 AND (lower(p.full_name) LIKE $2 ESCAPE '!' OR lower(p.national_id) LIKE $2 ESCAPE '!' OR lower(p.phone) LIKE $2 ESCAPE '!') AND (` + category + `)`
	var total int64
	err = tx.QueryRow(ctx, "SELECT count(*) FROM patients p WHERE "+where, userID, pattern, todayText).Scan(&total)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT p.id,p.full_name,p.birth_date::text,p.national_id,p.phone,p.gender,(SELECT count(*) FROM clinical_encounters e WHERE e.patient_id=p.id),(SELECT max(e.encounter_date) FROM clinical_encounters e WHERE e.patient_id=p.id),`+pending+` FROM patients p WHERE `+where+` ORDER BY `+page.order+` LIMIT $4 OFFSET $5`, userID, pattern, todayText, page.size, page.number*page.size)
	if err != nil {
		return nil, err
	}
	content := []PatientList{}
	for rows.Next() {
		var item PatientList
		var last *time.Time
		if err := rows.Scan(&item.ID, &item.Name, &item.Birth, &item.NationalID, &item.Phone, &item.Gender, &item.Encounters, &last, &item.Pending); err != nil {
			rows.Close()
			return nil, err
		}
		birth, err := time.Parse("2006-01-02", item.Birth)
		if err != nil {
			rows.Close()
			return nil, err
		}
		item.Months = ageMonths(birth, today)
		item.Age = directoryAge(birth, today, item.Months)
		item.Group = ageGroup(item.Months)
		if last != nil {
			date := last.In(s.viewZone(ctx)).Format("2006-01-02")
			item.LastDate = &date
		}
		content = append(content, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	pages := (total + page.size - 1) / page.size
	return map[string]any{"content": content, "page": map[string]any{"size": page.size, "number": page.number, "totalElements": total, "totalPages": pages}}, nil
}
func (s *Service) DirectoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/doctorchart/patients", func(w http.ResponseWriter, r *http.Request) {
		value, err := s.ListPatients(r.Context(), api.User(r.Context()).UserID, r.URL.Query())
		if err != nil {
			api.WriteError(w, err)
			return
		}
		api.WriteJSON(w, 200, value)
	})
}
