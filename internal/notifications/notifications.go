// Package notifications owns administrative delivery, not booking or clinical rules.
package notifications

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	PublicOrigin                      string
	Enabled                           bool
	Key                               string
	Host                              string
	Port                              int
	From, Username, Password, TLSMode string
	ReminderMinutes                   int
}
type Sender interface {
	Send(context.Context, string, []byte) error
}
type Service struct {
	pool   *pgxpool.Pool
	cipher cipher.AEAD
	sender Sender
	from   string
	lead   time.Duration
	origin string
}
type Message struct {
	Date      time.Time `json:"date"`
	To        string    `json:"to"`
	Doctor    string    `json:"doctor,omitempty"`
	Clinic    string    `json:"clinic,omitempty"`
	Address   string    `json:"address,omitempty"`
	Timezone  string    `json:"timezone,omitempty"`
	Start     time.Time `json:"start,omitempty"`
	Reference string    `json:"reference,omitempty"`
	Code      string    `json:"code,omitempty"`
	Expires   time.Time `json:"expires,omitempty"`
}

func Token() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("secure random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}
func Hash(value string) string { h := sha256.Sum256([]byte(value)); return hex.EncodeToString(h[:]) }
func uuid() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("secure random unavailable")
	}
	raw[6] = raw[6]&15 | 64
	raw[8] = raw[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
}
func New(pool *pgxpool.Pool, c Config) (*Service, error) {
	if !c.Enabled {
		return nil, nil
	}
	u, err := url.Parse(c.PublicOrigin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("notifications require an exact HTTPS public origin")
	}
	sender, err := NewSMTP(c)
	if err != nil {
		return nil, err
	}
	s, err := NewWithSender(pool, c.Key, c.From, c.ReminderMinutes, sender)
	if err == nil {
		s.origin = c.PublicOrigin
	}
	return s, err
}
func NewWithSender(pool *pgxpool.Pool, key, from string, minutes int, sender Sender) (*Service, error) {
	fromAddress, err := mail.ParseAddress(from)
	if err != nil || fromAddress.Address != from {
		return nil, errors.New("invalid notification sender")
	}
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 || minutes < 1 || minutes > 10080 || sender == nil {
		return nil, errors.New("invalid notification encryption or reminder configuration")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, errors.New("invalid notification key")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, cipher: aead, sender: sender, from: from, lead: time.Duration(minutes) * time.Minute}, nil
}
func (s *Service) seal(value []byte, aad string) []byte {
	nonce := make([]byte, s.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic("secure random unavailable")
	}
	return s.cipher.Seal(nonce, nonce, value, []byte(aad))
}
func (s *Service) open(value []byte, aad string) ([]byte, error) {
	n := s.cipher.NonceSize()
	if len(value) < n {
		return nil, errors.New("notification ciphertext invalid")
	}
	plain, err := s.cipher.Open(nil, value[:n], value[n:], []byte(aad))
	if err != nil {
		return nil, errors.New("notification ciphertext invalid")
	}
	return plain, nil
}

// ValidateKey refuses to start with a key that cannot read existing delivery state.
func (s *Service) ValidateKey(ctx context.Context) error {
	var data []byte
	var aad string
	err := s.pool.QueryRow(ctx, "SELECT payload,dedup_key FROM notification_outbox ORDER BY id LIMIT 1").Scan(&data, &aad)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		if _, err = s.open(data, aad); err != nil {
			return err
		}
	}
	err = s.pool.QueryRow(ctx, "SELECT encrypted_code,'booking:'||booking_id::text FROM booking_confirmation_access LIMIT 1").Scan(&data, &aad)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		_, err = s.open(data, aad)
		return err
	}
	return nil
}
func (s *Service) enqueue(ctx context.Context, tx pgx.Tx, key, kind string, booking, version, access, recovery any, due time.Time, m Message) error {
	if m.Date.IsZero() {
		m.Date = time.Now().UTC()
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO notification_outbox(dedup_key,kind,booking_id,booking_version,access_id,recovery_id,payload,due_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(dedup_key) DO NOTHING", key, kind, booking, version, access, recovery, s.seal(raw, key), due)
	return err
}
func (s *Service) BookingChanged(ctx context.Context, tx pgx.Tx, id, version, kind string) error {
	var m Message
	var status string
	var end time.Time
	err := tx.QueryRow(ctx, `SELECT p.email,p.doctor_name,c.name,COALESCE(cp.address,''),c.timezone,b.start_at,b.end_at,p.reference::text,b.status FROM bookings b JOIN booking_public_details p ON p.booking_id=b.id JOIN clinics c ON c.id=b.clinic_id LEFT JOIN clinic_publications cp ON cp.clinic_id=c.id AND cp.enabled WHERE b.id=$1`, id).Scan(&m.To, &m.Doctor, &m.Clinic, &m.Address, &m.Timezone, &m.Start, &end, &m.Reference, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var data []byte
	var revoked *time.Time
	err = tx.QueryRow(ctx, "SELECT encrypted_code,revoked_at FROM booking_confirmation_access WHERE booking_id=$1", id).Scan(&data, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		m.Code = Token()
		m.Expires = end.AddDate(0, 0, 7)
		_, err = tx.Exec(ctx, "INSERT INTO booking_confirmation_access(booking_id,token_hash,encrypted_code,expires_at) VALUES($1,$2,$3,$4)", id, Hash(m.Code), s.seal([]byte(m.Code), "booking:"+id), m.Expires)
	} else if err == nil && revoked == nil {
		var raw []byte
		raw, err = s.open(data, "booking:"+id)
		m.Code = string(raw)
		m.Expires = end.AddDate(0, 0, 7)
		if err == nil {
			_, err = tx.Exec(ctx, "UPDATE booking_confirmation_access SET expires_at=$1 WHERE booking_id=$2", m.Expires, id)
		}
	}
	if err != nil {
		return err
	}
	event := ""
	switch kind {
	case "CREATED":
		event = "BOOKING_CONFIRMED"
	case "RESCHEDULED":
		event = "BOOKING_RESCHEDULED"
	case "STATUS_CHANGED":
		if status == "CANCELLED" {
			event = "BOOKING_CANCELLED"
		}
	}
	if event != "" {
		if err = s.enqueue(ctx, tx, "booking:"+id+":"+version+":"+event, event, id, version, nil, nil, time.Now().UTC(), m); err != nil {
			return err
		}
	}
	due := m.Start.Add(-s.lead)
	if status == "CONFIRMED" && due.After(time.Now()) {
		return s.enqueue(ctx, tx, "booking:"+id+":"+version+":reminder", "BOOKING_REMINDER", id, version, nil, nil, due, m)
	}
	return nil
}
func (s *Service) Code(ctx context.Context, id string) (string, time.Time, error) {
	var data []byte
	var expires time.Time
	err := s.pool.QueryRow(ctx, "SELECT encrypted_code,expires_at FROM booking_confirmation_access WHERE booking_id=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp()", id).Scan(&data, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", expires, nil
	}
	if err != nil {
		return "", expires, err
	}
	raw, err := s.open(data, "booking:"+id)
	return string(raw), expires, err
}
func (s *Service) Invitation(ctx context.Context, tx pgx.Tx, access, email, code string, expires time.Time) error {
	return s.enqueue(ctx, tx, "invitation:"+Hash(code), "STAFF_INVITATION", nil, nil, access, nil, time.Now().UTC(), Message{To: email, Code: code, Expires: expires})
}
func (s *Service) Recovery(ctx context.Context, tx pgx.Tx, account, email string) error {
	id, code := uuid(), Token()
	expires := time.Now().UTC().Add(15 * time.Minute)
	if _, err := tx.Exec(ctx, "INSERT INTO staff_email_recovery(id,account_id,token_hash,expires_at) VALUES($1,$2,$3,$4)", id, account, Hash(code), expires); err != nil {
		return err
	}
	return s.enqueue(ctx, tx, "recovery:"+id, "STAFF_RECOVERY", nil, nil, nil, id, time.Now().UTC(), Message{To: email, Code: code, Expires: expires})
}
