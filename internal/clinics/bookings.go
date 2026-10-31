package clinics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/syntah/doctorchart-backend-go/internal/api"
)

type Booking struct {
	ID           string    `json:"id"`
	ClinicID     string    `json:"clinicId"`
	PatientID    *string   `json:"patientId"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	Status       string    `json:"status"`
	Version      string    `json:"version"`
	AttendeeName string    `json:"attendeeName"`
	ContactName  string    `json:"contactName"`
	ContactPhone string    `json:"contactPhone"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

const bookingColumns = "b.id::text,b.clinic_id::text,b.patient_id::text,b.start_at,b.end_at,b.status,b.version::text,COALESCE(b.attendee_name,b.contact_name),b.contact_name,b.contact_phone,b.created_at,b.updated_at"

func scanBooking(row scanner) (Booking, error) {
	var b Booking
	err := row.Scan(&b.ID, &b.ClinicID, &b.PatientID, &b.Start, &b.End, &b.Status, &b.Version, &b.AttendeeName, &b.ContactName, &b.ContactPhone, &b.CreatedAt, &b.UpdatedAt)
	b.Start, b.End, b.CreatedAt, b.UpdatedAt = b.Start.UTC(), b.End.UTC(), b.CreatedAt.UTC(), b.UpdatedAt.UTC()
	return b, err
}
func bookingMissing() error {
	return api.Failure(404, "BOOKING_NOT_FOUND", "No se encontró la reserva.")
}
func slotUnavailable() error {
	return api.Failure(409, "BOOKING_SLOT_UNAVAILABLE", "Ese horario ya no está disponible. Consultá la disponibilidad nuevamente.")
}
func bookingState() error {
	return api.Failure(409, "BOOKING_STATE_CONFLICT", "La reserva no admite esa operación en su estado actual.")
}
func bookingDBError(err error) error {
	var failure *pgconn.PgError
	if errors.As(err, &failure) && failure.Code == "23P01" {
		return slotUnavailable()
	}
	return err
}
func ownedBooking(ctx context.Context, q scheduleReader, user, id string) (Booking, error) {
	if _, err := decimal(id, true); err != nil {
		return Booking{}, err
	}
	b, err := scanBooking(q.QueryRow(ctx, "SELECT "+bookingColumns+" FROM bookings b JOIN doctor_workspaces w ON w.id=b.workspace_id WHERE b.id=$1 AND w.user_id=$2", id, user))
	if errors.Is(err, pgx.ErrNoRows) {
		return b, bookingMissing()
	}
	return b, err
}
func (s *Service) bookingRoutes(mux *http.ServeMux, wrap func(func(http.ResponseWriter, *http.Request, string) error) http.HandlerFunc) {
	mux.HandleFunc("POST /api/doctorchart/bookings", wrap(func(w http.ResponseWriter, r *http.Request, u string) error {
		return s.mutateBooking(w, r, u, "create")
	}))
	mux.HandleFunc("PATCH /api/doctorchart/bookings/{id}/status", wrap(func(w http.ResponseWriter, r *http.Request, u string) error {
		return s.mutateBooking(w, r, u, "status")
	}))
	mux.HandleFunc("POST /api/doctorchart/bookings/{id}/reschedule", wrap(func(w http.ResponseWriter, r *http.Request, u string) error {
		return s.mutateBooking(w, r, u, "reschedule")
	}))
	mux.HandleFunc("GET /api/doctorchart/bookings", wrap(s.listBookings))
	mux.HandleFunc("GET /api/doctorchart/bookings/{id}", wrap(func(w http.ResponseWriter, r *http.Request, u string) error {
		b, err := ownedBooking(r.Context(), s.pool, u, r.PathValue("id"))
		if err != nil {
			return err
		}
		api.WriteJSON(w, 200, b)
		return nil
	}))
	mux.HandleFunc("GET /api/doctorchart/bookings/{id}/events", wrap(s.bookingEvents))
}
func (s *Service) bookingEvents(w http.ResponseWriter, r *http.Request, user string) error {
	b, err := ownedBooking(r.Context(), s.pool, user, r.PathValue("id"))
	if err != nil {
		return err
	}
	rows, err := s.pool.Query(r.Context(), "SELECT version::text,kind,actor_id,occurred_at,before_document,after_document FROM booking_events WHERE booking_id=$1 ORDER BY version", b.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type event struct {
		Version    string          `json:"version"`
		Kind       string          `json:"kind"`
		ActorID    string          `json:"actorId"`
		OccurredAt time.Time       `json:"occurredAt"`
		Before     json.RawMessage `json:"before"`
		After      json.RawMessage `json:"after"`
	}
	items := []event{}
	for rows.Next() {
		var e event
		if err = rows.Scan(&e.Version, &e.Kind, &e.ActorID, &e.OccurredAt, &e.Before, &e.After); err != nil {
			return err
		}
		e.OccurredAt = e.OccurredAt.UTC()
		if e.Before == nil {
			e.Before = json.RawMessage("null")
		}
		items = append(items, e)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	api.WriteJSON(w, 200, map[string]any{"items": items})
	return nil
}
func bookingInstant(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || t.Year() < 1 || t.Year() > 9999 {
		return time.Time{}, field("start", "INVALID_TIMESTAMP")
	}
	return t.UTC(), nil
}
func (s *Service) listBookings(w http.ResponseWriter, r *http.Request, user string) error {
	q := r.URL.Query()
	for key, values := range q {
		if (key != "from" && key != "to" && key != "page" && key != "size") || len(values) != 1 {
			return api.Invalid("Parámetros de reservas inválidos.")
		}
	}
	from, err := bookingInstant(q.Get("from"))
	if err != nil {
		return field("from", "INVALID_TIMESTAMP")
	}
	to, err := bookingInstant(q.Get("to"))
	if err != nil || !to.After(from) || to.Sub(from) > 93*24*time.Hour {
		return field("to", "INVALID_RANGE")
	}
	page, size := int64(0), int64(20)
	if q.Has("page") {
		page, err = decimal(q.Get("page"), false)
		if err != nil {
			return err
		}
	}
	if q.Has("size") {
		size, err = decimal(q.Get("size"), true)
		if err != nil {
			return err
		}
	}
	if size > 100 || page > 9223372036854775807/size {
		return api.Invalid("Paginación inválida.")
	}
	workspace, err := s.workspace(r.Context(), user)
	if err != nil {
		return err
	}
	var total int64
	if err = s.pool.QueryRow(r.Context(), "SELECT count(*) FROM bookings WHERE workspace_id=$1 AND start_at<$3 AND end_at>$2", workspace, from, to).Scan(&total); err != nil {
		return err
	}
	rows, err := s.pool.Query(r.Context(), "SELECT "+bookingColumns+" FROM bookings b WHERE workspace_id=$1 AND start_at<$3 AND end_at>$2 ORDER BY start_at,id LIMIT $4 OFFSET $5", workspace, from, to, size, page*size)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []Booking{}
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return err
		}
		items = append(items, b)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	api.WriteJSON(w, 200, map[string]any{"items": items, "page": page, "size": size, "totalElements": total})
	return nil
}

type bookingRequest struct {
	ExpectedVersion string  `json:"expectedVersion"`
	ClinicID        string  `json:"clinicId"`
	Start           string  `json:"start"`
	PatientID       *string `json:"patientId"`
	AttendeeName    *string `json:"attendeeName"`
	ContactName     string  `json:"contactName"`
	ContactPhone    string  `json:"contactPhone"`
	Status          string  `json:"status"`
}

var idempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{16,128}$`)

func decodeBooking(w http.ResponseWriter, r *http.Request, op string) (bookingRequest, string, string, error) {
	key := r.Header.Get("Idempotency-Key")
	if len(r.Header.Values("Idempotency-Key")) != 1 || !idempotencyPattern.MatchString(key) {
		return bookingRequest{}, "", "", field("Idempotency-Key", "INVALID_KEY")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	body, err := api.Decode(w, r)
	if err != nil {
		return bookingRequest{}, "", "", err
	}
	allowed := []string{"expectedVersion", "status"}
	required := []string{"expectedVersion", "status"}
	if op == "create" {
		allowed = []string{"clinicId", "start", "patientId", "contactName", "contactPhone", "attendeeName"}
		required = []string{"clinicId", "start", "contactName"}
	}
	if op == "reschedule" {
		allowed = []string{"expectedVersion", "clinicId", "start"}
		required = allowed
	}
	if err = api.Only(body, allowed...); err != nil {
		return bookingRequest{}, "", "", err
	}
	for _, name := range allowed {
		v, present := body[name]
		if present && v == nil && name != "patientId" {
			return bookingRequest{}, "", "", field(name, "REQUIRED")
		}
	}
	for _, name := range required {
		if body[name] == nil {
			return bookingRequest{}, "", "", field(name, "REQUIRED")
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return bookingRequest{}, "", "", err
	}
	var request bookingRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&request); err != nil {
		return request, "", "", api.Invalid("Revisá los tipos de los campos de la reserva.")
	}
	if op != "create" {
		if _, err = decimal(request.ExpectedVersion, false); err != nil {
			return request, "", "", field("expectedVersion", "INVALID_VERSION")
		}
	}
	if op != "status" {
		if _, err = decimal(request.ClinicID, true); err != nil {
			return request, "", "", field("clinicId", "INVALID_ID")
		}
		if _, err = bookingInstant(request.Start); err != nil {
			return request, "", "", err
		}
	}
	if op == "create" {
		if request.AttendeeName != nil {
			value := strings.TrimSpace(*request.AttendeeName)
			if len([]rune(value)) < 1 || len([]rune(value)) > 160 {
				return request, "", "", field("attendeeName", "INVALID_LENGTH")
			}
			request.AttendeeName = &value
		}
		request.ContactName = strings.TrimSpace(request.ContactName)
		request.ContactPhone = strings.TrimSpace(request.ContactPhone)
		if len([]rune(request.ContactName)) < 1 || len([]rune(request.ContactName)) > 160 {
			return request, "", "", field("contactName", "INVALID_LENGTH")
		}
		if len([]rune(request.ContactPhone)) > 80 {
			return request, "", "", field("contactPhone", "TOO_LONG")
		}
		if request.PatientID != nil {
			if _, err = decimal(*request.PatientID, true); err != nil {
				return request, "", "", field("patientId", "INVALID_ID")
			}
		}
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(append([]byte(r.Method+"\n"+r.URL.Path+"\n"), raw...)))
	return request, key, hash, nil
}
func replayBooking(ctx context.Context, q scheduleReader, workspace int64, key, hash string) (int, json.RawMessage, error) {
	var stored string
	var status int
	var response json.RawMessage
	err := q.QueryRow(ctx, "SELECT request_sha256,response_status,response_document FROM booking_idempotency WHERE workspace_id=$1 AND key=$2", workspace, key).Scan(&stored, &status, &response)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	if stored != hash {
		return 0, nil, api.Failure(409, "IDEMPOTENCY_CONFLICT", "Esa clave de reintento ya se usó para otra solicitud.")
	}
	return status, response, nil
}
func bookingTransition(old, target string) bool {
	return old == target || (old == "CONFIRMED" && (target == "CHECKED_IN" || target == "NO_SHOW" || target == "CANCELLED")) || (old == "CHECKED_IN" && (target == "COMPLETED" || target == "CANCELLED"))
}
func (s *Service) mutateBooking(w http.ResponseWriter, r *http.Request, user, op string) error {
	var previous Booking
	if op != "create" {
		b, err := ownedBooking(r.Context(), s.pool, user, r.PathValue("id"))
		if err != nil {
			return err
		}
		previous = b
	}
	request, key, hash, err := decodeBooking(w, r, op)
	if err != nil {
		return err
	}
	if op != "status" {
		id, _ := strconv.ParseInt(request.ClinicID, 10, 64)
		if _, err = s.owned(r.Context(), user, id); err != nil {
			return err
		}
	}
	return s.applyBooking(w, r, user, op, previous, request, key, hash, nil)
}
func (s *Service) applyBooking(w http.ResponseWriter, r *http.Request, user, op string, previous Booking, request bookingRequest, key, hash string, public *publicAdmission) error {
	workspace, err := s.workspace(r.Context(), user)
	if err != nil {
		return err
	}
	staff := staffFrom(r.Context())
	var status int
	var response json.RawMessage
	if staff == nil {
		status, response, err = replayBooking(r.Context(), s.pool, workspace, key, hash)
	}
	if err != nil {
		return err
	}
	if status != 0 {
		return s.writeBookingResult(w, r, public, status, response)
	}
	rights := s.rights
	if staff != nil {
		rights = s.staffRights
	}
	if public != nil {
		rights = s.publicRights
	}
	snapshot, err := rights.Resolve(r.Context(), user)
	if err != nil {
		return unavailable()
	}
	ctx := r.Context()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(clean)
	}()
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='10s'"); err != nil {
		return err
	}
	if err = rights.Fence(ctx, tx, snapshot); err != nil {
		return unavailable()
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM doctor_workspaces WHERE id=$1 AND user_id=$2 FOR UPDATE", workspace, user).Scan(&workspace); err != nil {
		return err
	}
	if staff != nil {
		if op != "create" {
			previous, err = ownedBooking(ctx, tx, user, previous.ID)
			if err != nil {
				return err
			}
		}
		if err = staff.check(ctx, tx, workspace, snapshot, previous, request); err != nil {
			return err
		}
	}
	status, response, err = replayBooking(ctx, tx, workspace, key, hash)
	if err != nil {
		return err
	}
	if status != 0 {
		return s.writeBookingResult(w, r, public, status, response)
	}
	if err = rights.Fence(ctx, tx, snapshot); err != nil {
		return unavailable()
	}
	if public != nil {
		if err = public.check(ctx, tx, s, workspace, snapshot); err != nil {
			return err
		}
	}
	if !snapshot.ClinicManagement {
		return api.Failure(403, "CLINIC_MANAGEMENT_DISABLED", "Tu cobertura no habilita la gestión de reservas.")
	}
	var count int64
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM clinics WHERE workspace_id=$1 AND status<>'ARCHIVED'", workspace).Scan(&count); err != nil {
		return err
	}
	if snapshot.MaxClinics == nil {
		return unavailable()
	}
	// Cancelling or closing an existing booking must remain possible during a downgrade.
	if op != "status" && *snapshot.MaxClinics != -1 && count > *snapshot.MaxClinics {
		if public != nil {
			return publicDenied()
		}
		return api.Failure(409, "CLINIC_LIMIT_REACHED", "Regularizá el cupo de consultorios antes de crear o reprogramar reservas.")
	}
	if op != "create" {
		previous, err = ownedBooking(ctx, tx, user, previous.ID)
		if err != nil {
			return err
		}
		if previous.Version != request.ExpectedVersion {
			return api.Failure(409, "VERSION_CONFLICT", "La reserva cambió. Recargá su versión antes de modificarla.")
		}
	}
	if request.PatientID != nil {
		var valid bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM patients WHERE id=$1 AND user_id=$2)", *request.PatientID, user).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return api.Failure(404, "PATIENT_NOT_FOUND", "No se encontró el paciente.")
		}
	}
	var result Booking
	kind := "CREATED"
	status = 200
	if op == "status" {
		if !bookingTransition(previous.Status, request.Status) {
			return bookingState()
		}
		result = previous
		if previous.Status != request.Status {
			result, err = scanBooking(tx.QueryRow(ctx, "UPDATE bookings b SET status=$1,version=version+1,updated_at=clock_timestamp() WHERE id=$2 RETURNING "+bookingColumns, request.Status, previous.ID))
		}
		kind = "STATUS_CHANGED"
	} else {
		if op == "reschedule" && previous.Status != "CONFIRMED" {
			return bookingState()
		}
		var clinic Clinic
		clinic, err = scan(tx.QueryRow(ctx, "SELECT "+columns+" FROM clinics WHERE id=$1 AND workspace_id=$2 FOR UPDATE", request.ClinicID, workspace))
		if errors.Is(err, pgx.ErrNoRows) {
			return missing()
		}
		if err != nil {
			return err
		}
		if clinic.Status != "ACTIVE" {
			return api.Failure(409, "CLINIC_STATE_CONFLICT", "El consultorio no está activo.")
		}
		start, _ := bookingInstant(request.Start)
		// Reuse M3's cross-clinic guard for the exact local dates being booked,
		// including zone/date-line margins if tzdata changed after configuration.
		location, zoneErr := time.LoadLocation(clinic.Timezone)
		if zoneErr != nil {
			return zoneErr
		}
		local := start.In(location)
		date := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
		agendas, agendaErr := readAgendas(ctx, tx, workspace)
		if agendaErr != nil {
			return agendaErr
		}
		if agendaErr = checkAgendaConflicts(ctx, agendas, date.AddDate(0, 0, -2), 5); agendaErr != nil {
			if failure, ok := agendaErr.(*api.Error); ok && failure.Code == "SCHEDULE_CONFLICT" {
				return slotUnavailable()
			}
			return agendaErr
		}
		var end time.Time
		end, err = scheduleSlotEnd(ctx, tx, clinic, start, time.Now().UTC(), false)
		if err != nil {
			return err
		}
		var occupied bool
		excluded := "0"
		if op == "reschedule" {
			excluded = previous.ID
		}
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bookings WHERE workspace_id=$1 AND status<>'CANCELLED' AND id<>$4 AND start_at<$3 AND end_at>$2)", workspace, start, end, excluded).Scan(&occupied); err != nil {
			return err
		}
		if occupied {
			return slotUnavailable()
		}
		if op == "create" {
			status = 201
			result, err = scanBooking(tx.QueryRow(ctx, "INSERT INTO bookings AS b(workspace_id,clinic_id,patient_id,start_at,end_at,contact_name,contact_phone,attendee_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING "+bookingColumns, workspace, clinic.ID, request.PatientID, start, end, request.ContactName, request.ContactPhone, request.AttendeeName))
		} else {
			kind = "RESCHEDULED"
			result, err = scanBooking(tx.QueryRow(ctx, "UPDATE bookings b SET clinic_id=$1,start_at=$2,end_at=$3,version=version+1,updated_at=clock_timestamp() WHERE id=$4 RETURNING "+bookingColumns, clinic.ID, start, end, previous.ID))
		}
	}
	if err != nil {
		return bookingDBError(err)
	}
	if public != nil {
		if err = public.record(ctx, tx, result); err != nil {
			return err
		}
	}
	response, err = json.Marshal(result)
	if err != nil {
		return err
	}
	if op == "create" || result.Version != previous.Version {
		actor := user
		if staff != nil {
			actor = "STAFF:" + staff.session.ID
		}
		if public != nil {
			actor = "PUBLIC_BOOKING"
		}
		var before any
		if op != "create" {
			raw, e := json.Marshal(previous)
			if e != nil {
				return e
			}
			before = json.RawMessage(raw)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO booking_events(booking_id,version,kind,actor_id,before_document,after_document) VALUES($1,$2,$3,$4,$5,$6)", result.ID, result.Version, kind, actor, before, response); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO booking_idempotency(workspace_id,key,request_sha256,booking_id,response_status,response_document) VALUES($1,$2,$3,$4,$5,$6)", workspace, key, hash, result.ID, status, response); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return bookingDBError(err)
	}
	return s.writeBookingResult(w, r, public, status, response)
}
