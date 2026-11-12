package relationships

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/relationships/store"
)

const (
	minuteRequests = 10
	dayRequests    = 100
	requestDay     = 24 * time.Hour
)

// chargeRequest serializes and commits the rate window before the target
// lookup, so rejections and hidden blocks cost the same as normal requests.
func (s *Service) chargeRequest(ctx context.Context, id snowflake.ID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.q.EnsureRequestWindow(ctx, tx, int64(id)); err != nil {
			return fmt.Errorf("relationships: initialize request window: %w", err)
		}
		attempts, err := s.q.LockRequestWindow(ctx, tx, int64(id))
		if err != nil {
			return fmt.Errorf("relationships: lock request window: %w", err)
		}
		now := s.now()
		if n := len(attempts); n > 0 && attempts[n-1].After(now) {
			now = attempts[n-1]
		}
		cutoff := now.Add(-requestDay)
		for len(attempts) > 0 && !attempts[0].After(cutoff) {
			attempts = attempts[1:]
		}
		var retry time.Duration
		if len(attempts) >= dayRequests {
			retry = attempts[len(attempts)-dayRequests].Add(requestDay).Sub(now)
		}
		if len(attempts) >= minuteRequests {
			retry = max(retry, attempts[len(attempts)-minuteRequests].Add(time.Minute).Sub(now))
		}
		if retry > 0 {
			return httpx.TooManyRequests(retry)
		}
		if err = s.q.SaveRequestWindow(ctx, tx, store.SaveRequestWindowParams{
			UserID: int64(id), Attempts: append(attempts, now),
		}); err != nil {
			return fmt.Errorf("relationships: save request window: %w", err)
		}
		return nil
	})
}
