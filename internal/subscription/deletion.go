package subscription

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Delete revokes access and queues cleanup in one Redis transaction. Staged
// uploads already have cleanup jobs and cannot commit after the record is gone.
func (s *Store) Delete(ctx context.Context, id string) error {
	recordKey := subscriptionKey(id)
	return s.watch(ctx, []string{recordKey}, func(tx *redis.Tx) error {
		sub, err := readSubscription(ctx, tx, id)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Del(ctx, recordKey)
			pipe.SRem(ctx, catalogKey, id)
			if sub.Publication != nil {
				queueCleanup(ctx, pipe, sub.Publication.ObjectKey, cleanupRetired, time.Now())
			}
			return nil
		})
		return err
	})
}
