package notifications

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

func base64Text(raw []byte) string {
	value := base64.StdEncoding.EncodeToString(raw)
	out := ""
	for len(value) > 76 {
		out += value[:76] + "\r\n"
		value = value[76:]
	}
	return out + value + "\r\n"
}

type job struct {
	id               int64
	key, kind, lease string
	payload          []byte
	attempts         int
	booking          *int64
	version          *int64
	access, recovery *string
}

func (s *Service) claim(ctx context.Context) (job, error) {
	j := job{lease: uuid()}
	err := s.pool.QueryRow(ctx, `WITH next AS (SELECT id FROM notification_outbox WHERE (state='PENDING' AND due_at<=clock_timestamp()) OR (state='PROCESSING' AND lease_until<clock_timestamp()) ORDER BY due_at,id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE notification_outbox o SET state='PROCESSING',attempts=attempts+1,lease_id=$1,lease_until=clock_timestamp()+interval '2 minutes',last_code=NULL FROM next WHERE o.id=next.id RETURNING o.id,o.dedup_key,o.kind,o.payload,o.attempts,o.booking_id,o.booking_version,o.access_id::text,o.recovery_id::text`, j.lease).Scan(&j.id, &j.key, &j.kind, &j.payload, &j.attempts, &j.booking, &j.version, &j.access, &j.recovery)
	return j, err
}
func (s *Service) current(ctx context.Context, j job, m Message) (bool, error) {
	var valid bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notification_outbox WHERE id=$1 AND state='PROCESSING' AND lease_id=$2 AND lease_until>clock_timestamp())", j.id, j.lease).Scan(&valid)
	if err != nil || !valid {
		return false, err
	}
	if j.booking != nil {
		required := "CONFIRMED"
		switch j.kind {
		case "BOOKING_CANCELLED":
			required = "CANCELLED"
		}
		// Linking a clinical file changes no administrative message content. Keep
		// pending confirmations/reminders valid without enqueueing duplicates.
		err = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bookings b WHERE id=$1 AND version>=$2 AND status=$3 AND ($4<>'BOOKING_REMINDER' OR start_at>clock_timestamp()) AND (version=$2 OR ((SELECT count(*) FROM booking_events e WHERE e.booking_id=b.id AND e.version>$2)=b.version-$2 AND NOT EXISTS(SELECT 1 FROM booking_events e WHERE e.booking_id=b.id AND e.version>$2 AND e.kind<>'PATIENT_LINKED'))))", *j.booking, *j.version, required, j.kind).Scan(&valid)
	} else if j.access != nil {
		err = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM staff_invitations i JOIN staff_access a ON a.id=i.access_id WHERE a.id=$1 AND a.active AND i.token_hash=$2 AND i.consumed_at IS NULL AND i.expires_at>clock_timestamp())", *j.access, Hash(m.Code)).Scan(&valid)
	} else if j.recovery != nil {
		err = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM staff_email_recovery WHERE id=$1 AND consumed_at IS NULL AND expires_at>clock_timestamp() AND token_hash=$2)", *j.recovery, Hash(m.Code)).Scan(&valid)
	} else {
		return false, errors.New("notification association invalid")
	}
	return valid, err
}
func retry(attempt int) time.Duration {
	if attempt > 9 {
		return 5 * time.Minute
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second * time.Duration(1<<uint(attempt-1))
	return min(delay, 5*time.Minute)
}
func (s *Service) finish(ctx context.Context, j job, state, code string) error {
	_, err := s.pool.Exec(ctx, `UPDATE notification_outbox SET state=$1::varchar,last_code=NULLIF($2,''),due_at=CASE WHEN $1::varchar='PENDING' THEN clock_timestamp()+$3::interval ELSE due_at END,lease_until=NULL,lease_id=NULL,sent_at=CASE WHEN $1::varchar='SENT' THEN clock_timestamp() ELSE sent_at END WHERE id=$4 AND lease_id=$5 AND state='PROCESSING'`, state, code, retry(j.attempts).String(), j.id, j.lease)
	return err
}

// ProcessOne never holds a booking lock or a database transaction during SMTP.
func (s *Service) ProcessOne(ctx context.Context) (bool, error) {
	j, err := s.claim(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if j.attempts > 10 {
		return true, s.finish(ctx, j, "DEAD", "ATTEMPTS_EXHAUSTED")
	}
	state, code := "SENT", ""
	raw, err := s.open(j.payload, j.key)
	var m Message
	if err != nil || json.Unmarshal(raw, &m) != nil {
		state, code = "DEAD", "DECRYPTION_FAILED"
	} else {
		// Old encrypted outbox payloads predate the specialty field. Read the
		// reservation's frozen classification, never the current public profile.
		if j.booking != nil && m.Specialty == "" {
			if err = s.pool.QueryRow(ctx, "SELECT specialty_snapshot->>'name' FROM bookings WHERE id=$1", *j.booking).Scan(&m.Specialty); err != nil {
				return true, err
			}
		}
		if j.booking != nil && m.Code != "" {
			var usable bool
			if e := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM booking_confirmation_access WHERE booking_id=$1 AND token_hash=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp())", *j.booking, Hash(m.Code)).Scan(&usable); e != nil {
				return true, e
			}
			if !usable {
				m.Code = ""
			}
		}
		body, e := s.render(j.kind, j.key, m)
		if e != nil {
			state, code = "DEAD", "MESSAGE_INVALID"
		} else {
			valid, e := s.current(ctx, j, m)
			if e != nil {
				state, code = "PENDING", "STATE_UNAVAILABLE"
			} else if !valid {
				state, code = "OBSOLETE", "STATE_CHANGED"
			} else {
				sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				e = s.sender.Send(sendCtx, m.To, body)
				cancel()
				if e != nil {
					state, code = "PENDING", "SMTP_UNAVAILABLE"
				}
			}
		}
	}
	if state == "PENDING" && j.attempts >= 10 {
		state = "DEAD"
	}
	clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err = s.finish(clean, j, state, code); err != nil {
		return true, err
	}
	slog.Info("administrative notification processed", "job", j.id, "kind", j.kind, "state", state, "attempt", j.attempts, "code", code)
	return true, nil
}
func (s *Service) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for ctx.Err() == nil {
				job, err := s.ProcessOne(ctx)
				if err == nil && job {
					continue
				}
				if err != nil && ctx.Err() == nil {
					slog.Warn("notification queue temporarily unavailable")
				}
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		})
	}
	wg.Wait()
}
