// Package notifications owns administrative delivery, not booking or clinical rules.
package notifications

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/syntah/doctorchart-backend-go/internal/mailclient"
)

type Config struct {
	Enabled                           bool
	Key, PublicOrigin                 string
	MailServiceURL, MailServiceAPIKey string
}
type Sender interface {
	Send(context.Context, mailclient.Message) (mailclient.Result, error)
}
type Service struct {
	pool        *pgxpool.Pool
	cipher      cipher.AEAD
	sender      Sender
	origin      string
	recoveryKey []byte
}
type Message struct {
	Date        time.Time      `json:"date"`
	To          string         `json:"to"`
	Code        string         `json:"code,omitempty"`
	Expires     time.Time      `json:"expires,omitempty"`
	StaffOrigin string         `json:"staffOrigin,omitempty"`
	Start       time.Time      `json:"start,omitempty"`
	Timezone    string         `json:"timezone,omitempty"`
	Change      *bookingChange `json:"change,omitempty"`
}

type bookingChange struct {
	Reason   string `json:"reason,omitempty"`
	PublicID string `json:"publicId,omitempty"`
}

const staffInvitationKeyPrefix = "staff-invitation:"
const bookingEmailKeyPrefix = "booking-email:"

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
	sender, err := mailclient.New(c.MailServiceURL, c.MailServiceAPIKey, nil)
	if err != nil {
		return nil, err
	}
	s, err := NewWithSender(pool, c.Key, sender)
	if err == nil {
		s.origin = c.PublicOrigin
	}
	return s, err
}
func NewWithSender(pool *pgxpool.Pool, key string, sender Sender) (*Service, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 || sender == nil {
		return nil, errors.New("invalid notification encryption or sender configuration")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, errors.New("invalid notification key")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, raw)
	mac.Write([]byte("doctorchart/staff-email-recovery/v1"))
	return &Service{pool: pool, cipher: aead, sender: sender, recoveryKey: mac.Sum(nil)}, nil
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

func (s *Service) PublicOrigin() string { return s.origin }

func (s *Service) SealPortalSession(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(s.seal(value, "booking-portal-cookie"))
}

func (s *Service) OpenPortalSession(value string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	return s.open(raw, "booking-portal-cookie")
}

// ValidateKey refuses to start with a key that cannot read existing delivery state.
func (s *Service) ValidateKey(ctx context.Context) error {
	var data []byte
	var aad string
	err := s.pool.QueryRow(ctx, "SELECT payload,dedup_key FROM notification_outbox WHERE booking_id IS NULL ORDER BY id LIMIT 1").Scan(&data, &aad)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		_, err = s.open(data, aad)
		return err
	}
	return nil
}
func (s *Service) enqueue(ctx context.Context, tx pgx.Tx, key, kind string, access, recovery any, m Message) error {
	if m.Date.IsZero() {
		m.Date = time.Now().UTC()
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO notification_outbox(dedup_key,kind,access_id,recovery_id,payload,due_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(dedup_key) DO NOTHING", key, kind, access, recovery, s.seal(raw, key), m.Date)
	return err
}

func (s *Service) BookingPortalCode(ctx context.Context, tx pgx.Tx, id, email, code string) error {
	key := "booking-portal:" + id
	m := Message{To: email, Code: code, Date: time.Now().UTC()}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO notification_outbox(dedup_key,kind,portal_code_id,payload,due_at) VALUES($1,'BOOKING_PORTAL_CODE',$2,$3,$4)",
		key, id, s.seal(raw, key), m.Date)
	return err
}

// BookingChanged schedules email for the address explicitly provided with an online reservation.
func (s *Service) BookingChanged(ctx context.Context, tx pgx.Tx, id, version, event, status string, reason *string) error {
	kind := ""
	switch {
	case event == "CREATED":
		kind = "BOOKING_CONFIRMED"
	case event == "RESCHEDULED":
		kind = "BOOKING_RESCHEDULED"
	case event == "STATUS_CHANGED" && status == "CANCELLED":
		kind = "BOOKING_CANCELLED"
	}
	if kind == "" {
		return nil
	}
	var m Message
	var publicID string
	err := tx.QueryRow(ctx, `SELECT p.email,b.start_at,b.clinic_snapshot->>'timezone',coalesce(profile.public_id::text,'')
		FROM booking_public_details p JOIN bookings b ON b.id=p.booking_id
		LEFT JOIN booking_publications profile ON profile.workspace_id=b.workspace_id AND profile.published
		WHERE b.id=$1`, id).Scan(&m.To, &m.Start, &m.Timezone, &publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	m.Date = time.Now().UTC()
	if kind == "BOOKING_RESCHEDULED" || kind == "BOOKING_CANCELLED" {
		m.Change = &bookingChange{PublicID: publicID}
		if reason != nil {
			m.Change.Reason = *reason
		}
	}
	enqueue := func(kind string, due time.Time) error {
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		key := bookingEmailKeyPrefix + id + ":" + version + ":" + kind
		_, err = tx.Exec(ctx, "INSERT INTO notification_outbox(dedup_key,kind,booking_id,booking_version,payload,due_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(dedup_key) DO NOTHING",
			key, kind, id, version, s.seal(raw, key), due)
		return err
	}
	if err = enqueue(kind, m.Date); err != nil {
		return err
	}
	if due := m.Start.Add(-24 * time.Hour); status == "CONFIRMED" && due.After(m.Date) {
		m.Change = nil
		return enqueue("BOOKING_REMINDER", due)
	}
	return nil
}
func (s *Service) Invitation(ctx context.Context, tx pgx.Tx, access, email, code, origin string, expires time.Time) error {
	return s.enqueue(ctx, tx, staffInvitationKeyPrefix+Hash(code), "STAFF_INVITATION", access, nil,
		Message{To: email, Code: code, StaffOrigin: origin, Expires: expires})
}

func (s *Service) Recovery(ctx context.Context, tx pgx.Tx, account, email string) error {
	var previousID, previousHash string
	err := tx.QueryRow(ctx, `SELECT e.id::text,e.token_hash FROM staff_email_recovery e
		JOIN notification_outbox o ON o.recovery_id=e.id WHERE e.account_id=$1 ORDER BY o.id DESC LIMIT 1`, account).Scan(&previousID, &previousHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var code string
	for {
		n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			return err
		}
		code = fmt.Sprintf("%06d", n.Int64())
		if previousID == "" || !s.MatchesRecoveryCode(previousID, code, previousHash) {
			break
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE staff_email_recovery SET consumed_at=clock_timestamp() WHERE account_id=$1 AND consumed_at IS NULL", account); err != nil {
		return err
	}
	id := uuid()
	expires := time.Now().UTC().Add(15 * time.Minute)
	if _, err := tx.Exec(ctx, "INSERT INTO staff_email_recovery(id,account_id,token_hash,expires_at) VALUES($1,$2,$3,$4)", id, account, s.recoveryCodeHash(id, code), expires); err != nil {
		return err
	}
	return s.enqueue(ctx, tx, "recovery:"+id, "STAFF_RECOVERY", nil, id, Message{To: email, Code: code, Expires: expires})
}

func (s *Service) recoveryCodeHash(id, code string) string {
	mac := hmac.New(sha256.New, s.recoveryKey)
	mac.Write([]byte(id + ":" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) MatchesRecoveryCode(id, code, hash string) bool {
	return hmac.Equal([]byte(s.recoveryCodeHash(id, code)), []byte(hash))
}
