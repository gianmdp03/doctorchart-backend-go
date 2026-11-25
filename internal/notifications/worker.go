package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type job struct {
	id               int64
	key, kind, lease string
	payload          []byte
	attempts         int
	booking          *int64
	version          *int64
	access           *string
	recovery         *string
	portal           *string
}

func (s *Service) claim(ctx context.Context) (job, error) {
	j := job{lease: uuid()}
	err := s.pool.QueryRow(ctx, `WITH next AS (SELECT id FROM notification_outbox WHERE (state='PENDING' AND (due_at<=clock_timestamp() OR (booking_id IS NOT NULL AND dedup_key NOT LIKE 'booking-email:%'))) OR (state='PROCESSING' AND lease_until<clock_timestamp()) ORDER BY due_at,id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE notification_outbox o SET state='PROCESSING',attempts=attempts+1,lease_id=$1,lease_until=clock_timestamp()+interval '2 minutes',last_code=NULL FROM next WHERE o.id=next.id RETURNING o.id,o.dedup_key,o.kind,o.payload,o.attempts,o.booking_id,o.booking_version,o.access_id::text,o.recovery_id::text,o.portal_code_id::text`, j.lease).Scan(&j.id, &j.key, &j.kind, &j.payload, &j.attempts, &j.booking, &j.version, &j.access, &j.recovery, &j.portal)
	return j, err
}
func (s *Service) current(ctx context.Context, j job, m Message) (bool, error) {
	var valid bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notification_outbox WHERE id=$1 AND state='PROCESSING' AND lease_id=$2 AND lease_until>clock_timestamp())", j.id, j.lease).Scan(&valid)
	if err != nil || !valid {
		return false, err
	}
	if j.booking != nil {
		status := "CONFIRMED"
		if j.kind == "BOOKING_CANCELLED" {
			status = "CANCELLED"
		}
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bookings b WHERE b.id=$1 AND b.version>=$2 AND b.status=$3
			AND ($3='CANCELLED' OR b.start_at>clock_timestamp())
			AND NOT EXISTS(SELECT 1 FROM booking_events e WHERE e.booking_id=b.id AND e.version>$2 AND e.kind<>'PATIENT_LINKED'))`, *j.booking, *j.version, status).Scan(&valid)
	} else if j.kind == "STAFF_INVITATION" && j.access != nil {
		hash := Hash(m.Code)
		if j.key != staffInvitationKeyPrefix+hash {
			return false, nil
		}
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM staff_invitations i JOIN staff_access a ON a.id=i.access_id
			WHERE a.id=$1 AND a.active AND a.email=$2 AND i.token_hash=$3
				AND i.consumed_at IS NULL AND i.expires_at>clock_timestamp()
		)`, *j.access, m.To, hash).Scan(&valid)
	} else if j.recovery != nil {
		err = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM staff_email_recovery WHERE id=$1 AND consumed_at IS NULL AND expires_at>clock_timestamp() AND token_hash=$2)", *j.recovery, s.recoveryCodeHash(*j.recovery, m.Code)).Scan(&valid)
	} else if j.portal != nil {
		err = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM booking_portal_codes WHERE id=$1 AND expires_at>clock_timestamp() AND attempts<5 AND session_hash IS NULL)", *j.portal).Scan(&valid)
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

// ProcessOne delivers current messages and retires jobs from the discontinued booking email flow.
func (s *Service) ProcessOne(ctx context.Context) (bool, error) {
	j, err := s.claim(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if j.booking != nil && !strings.HasPrefix(j.key, bookingEmailKeyPrefix) {
		return true, s.finish(ctx, j, "OBSOLETE", "BOOKING_EMAIL_DISABLED")
	}
	// Retire invitations queued by older builds without contacting the mail service.
	if j.kind == "STAFF_INVITATION" && !strings.HasPrefix(j.key, staffInvitationKeyPrefix) {
		return true, s.finish(ctx, j, "OBSOLETE", "INVITATION_EMAIL_DISABLED")
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
		message, e := s.render(j.kind, j.key, m)
		if e != nil {
			state, code = "DEAD", "MESSAGE_INVALID"
		} else {
			valid, e := s.current(ctx, j, m)
			if e != nil {
				state, code = "PENDING", "STATE_UNAVAILABLE"
			} else if !valid {
				state, code = "OBSOLETE", "STATE_CHANGED"
			} else {
				sendCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
				_, e = s.sender.Send(sendCtx, message)
				cancel()
				if e != nil {
					state, code = "PENDING", "MAIL_SERVICE_UNAVAILABLE"
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
