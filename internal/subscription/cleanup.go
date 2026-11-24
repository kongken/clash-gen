package subscription

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	cleanupIndexKey = "clash-gen:{subscriptions}:cleanup"
	cleanupStaged   = "staged"
	cleanupRetired  = "retired"

	cleanupPoll  = time.Second
	cleanupRetry = 5 * time.Second
	cleanupBatch = 32
)

// RunCleanup retries pending S3 deletions until ctx is canceled.
func (p *Publisher) RunCleanup(ctx context.Context) {
	ticker := time.NewTicker(cleanupPoll)
	defer ticker.Stop()
	for {
		if err := p.cleanDue(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("could not process subscription object cleanup")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *Publisher) cleanDue(ctx context.Context) error {
	now := time.Now()
	keys, err := p.store.redis.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key: cleanupIndexKey, Start: "-inf", Stop: now.UnixMilli(), ByScore: true, Count: cleanupBatch,
	}).Result()
	if err != nil {
		return err
	}

	for _, key := range keys {
		due, err := p.store.claimCleanup(ctx, key, now)
		if err != nil {
			return err
		}
		if !due {
			continue
		}

		deleteCtx, cancel := context.WithTimeout(ctx, ObjectTimeout)
		err = p.objects.delete(deleteCtx, key)
		cancel()
		if err != nil {
			// A second worker may already have acknowledged the deletion.
			if err := p.store.redis.ZAddXX(ctx, cleanupIndexKey, cleanupScore(key, time.Now().Add(cleanupRetry))).Err(); err != nil {
				return err
			}
			continue
		}
		if _, err := p.store.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			acknowledgeCleanup(ctx, pipe, key)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) claimCleanup(ctx context.Context, objectKey string, now time.Time) (bool, error) {
	jobKey := cleanupJobKey(objectKey)
	var due bool
	err := s.watch(ctx, []string{jobKey}, func(tx *redis.Tx) error {
		due = false
		state, err := tx.Get(ctx, jobKey).Result()
		if errors.Is(err, redis.Nil) {
			return nil
		}
		if err != nil {
			return err
		}
		score, err := tx.ZScore(ctx, cleanupIndexKey, objectKey).Result()
		if errors.Is(err, redis.Nil) || (err == nil && score > float64(now.UnixMilli())) {
			return nil
		}
		if err != nil {
			return err
		}
		if state == cleanupStaged {
			// Invalidate the candidate before touching its object. A publisher
			// attempting to commit uses the same watched job key.
			if _, err := tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, jobKey, cleanupRetired, 0)
				return nil
			}); err != nil {
				return err
			}
		}
		due = true
		return nil
	})
	return due, err
}

func (s *Store) expediteCleanup(ctx context.Context, objectKey string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	// XX preserves the decision of any commit or cleanup that already won.
	if err := s.redis.ZAddXX(ctx, cleanupIndexKey, cleanupScore(objectKey, time.Now())).Err(); err != nil {
		slog.Warn("could not expedite subscription object cleanup")
	}
}

func cleanupJobKey(objectKey string) string { return "clash-gen:{subscriptions}:object:" + objectKey }

func cleanupScore(objectKey string, at time.Time) redis.Z {
	return redis.Z{Score: float64(at.UnixMilli()), Member: objectKey}
}

func queueCleanup(ctx context.Context, pipe redis.Pipeliner, objectKey, state string, at time.Time) {
	pipe.Set(ctx, cleanupJobKey(objectKey), state, 0)
	pipe.ZAdd(ctx, cleanupIndexKey, cleanupScore(objectKey, at))
}

func acknowledgeCleanup(ctx context.Context, pipe redis.Pipeliner, objectKey string) {
	pipe.Del(ctx, cleanupJobKey(objectKey))
	pipe.ZRem(ctx, cleanupIndexKey, objectKey)
}
