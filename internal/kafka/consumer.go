package kafka

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

// We only act on a request entering the appraisal state; everything else on
// request.events is ignored.
const (
	eventTypeRequestStatusChanged = "request.status_changed"
	statusAppraisal               = "appraisal"
)

// Deduplicator skips already-processed event ids. Seen checks, Mark records —
// marking happens only after successful processing (see internal/dedup).
type Deduplicator interface {
	Seen(ctx context.Context, eventID string) (bool, error)
	Mark(ctx context.Context, eventID string) error
}

// AppraisalCreator creates the appraisal for a request entering appraisal.
type AppraisalCreator interface {
	CreateFromRequest(ctx context.Context, requestID uuid.UUID) (bool, error)
}

// inboundEnvelope is the subset of request-service's event envelope we read.
type inboundEnvelope struct {
	EventID   uuid.UUID       `json:"event_id"`
	EventType string          `json:"event_type"`
	RequestID uuid.UUID       `json:"request_id"`
	Data      json.RawMessage `json:"data"`
}

type statusChangedData struct {
	NewStatus string `json:"new_status"`
}

// Consumer reads request.events and creates appraisals for requests entering the
// appraisal status. Delivery is at-least-once: it dedups by event_id (Redis) and
// commits offsets only after a message is fully handled.
type Consumer struct {
	reader *kafka.Reader
	dedup  Deduplicator
	svc    AppraisalCreator
}

func NewConsumer(brokers []string, groupID, topic string, dedup Deduplicator, svc AppraisalCreator) *Consumer {
	return &Consumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers: brokers,
			GroupID: groupID,
			Topic:   topic,
		}),
		dedup: dedup,
		svc:   svc,
	}
}

// Run consumes until ctx is cancelled. A processing failure (Redis/DB down) stops
// the loop with the error so the uncommitted offset redelivers on the next start.
// Blocking; call it in a goroutine.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		m, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil // shutting down
			}
			return err
		}
		if err := c.process(ctx, m); err != nil {
			slog.ErrorContext(ctx, "consumer stopping after processing error", "error", err)
			return err
		}
		if err := c.reader.CommitMessages(ctx, m); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

// process returns a non-nil error only for transient failures that must NOT be
// committed. Malformed or irrelevant messages return nil so they are committed
// and skipped.
//
// Order matters: check-dup → create → mark. A crash before the mark redelivers
// the event and the create is a no-op (request_id UNIQUE); marking first would
// make a crash between mark and offset commit drop the event forever.
func (c *Consumer) process(ctx context.Context, m kafka.Message) error {
	var env inboundEnvelope
	if err := json.Unmarshal(m.Value, &env); err != nil {
		slog.WarnContext(ctx, "skipping malformed event", "error", err, "offset", m.Offset)
		return nil
	}
	if env.EventType != eventTypeRequestStatusChanged {
		return nil
	}
	if env.EventID == uuid.Nil {
		slog.WarnContext(ctx, "skipping event without event_id", "offset", m.Offset)
		return nil
	}

	var data statusChangedData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		slog.WarnContext(ctx, "skipping event with unreadable data", "error", err, "event_id", env.EventID)
		return nil
	}
	if data.NewStatus != statusAppraisal {
		return nil
	}

	dup, err := c.dedup.Seen(ctx, env.EventID.String())
	if err != nil {
		return err // transient — do not commit
	}
	if dup {
		slog.InfoContext(ctx, "duplicate event, skipping", "event_id", env.EventID)
		return nil
	}

	if _, err := c.svc.CreateFromRequest(ctx, env.RequestID); err != nil {
		return err // transient — no mark written, redelivery reprocesses
	}

	if err := c.dedup.Mark(ctx, env.EventID.String()); err != nil {
		// Redis hiccup after a successful create: fail without committing; the
		// redelivered event re-runs the idempotent create and retries the mark.
		return err
	}
	return nil
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}
