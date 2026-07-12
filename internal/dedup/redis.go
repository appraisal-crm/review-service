package dedup

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "review:dedup:"

// Deduplicator gives the Kafka consumer idempotency. Seen only checks; Mark is
// called AFTER the event is fully processed, so a crash mid-processing leaves no
// mark and the redelivery is reprocessed (processing itself is idempotent).
// Marking before processing would permanently drop the event on a crash in the
// window between the mark and the offset commit. No DB inbox table —
// at-least-once delivery + this.
type Deduplicator struct {
	rdb *redis.Client
	ttl time.Duration
}

func New(rdb *redis.Client, ttl time.Duration) *Deduplicator {
	return &Deduplicator{rdb: rdb, ttl: ttl}
}

// Seen reports whether eventID was already processed. It does not claim the id.
func (d *Deduplicator) Seen(ctx context.Context, eventID string) (bool, error) {
	n, err := d.rdb.Exists(ctx, keyPrefix+eventID).Result()
	if err != nil {
		return false, fmt.Errorf("check dedup key: %w", err)
	}
	return n == 1, nil
}

// Mark records eventID as processed. Call it only after processing succeeded.
func (d *Deduplicator) Mark(ctx context.Context, eventID string) error {
	if err := d.rdb.Set(ctx, keyPrefix+eventID, 1, d.ttl).Err(); err != nil {
		return fmt.Errorf("set dedup key: %w", err)
	}
	return nil
}
