package repository

import (
	"context"
	"errors"
	"time"

	"github.com/appraisal-crm/review-service/internal/domain"
	"github.com/google/uuid"
)

// Repository-level errors. The service maps these to domain errors, which the
// handler maps to HTTP codes.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("concurrent modification conflict")
)

// AppraisalRepository is the persistence contract. The service depends on this
// interface, so tests can swap a mock for real Postgres.
type AppraisalRepository interface {
	// Create inserts an appraisal. Idempotent on request_id: a second insert for
	// the same request is a no-op (created=false), so a redelivered event never
	// doubles the row.
	Create(ctx context.Context, a *domain.Appraisal) (created bool, err error)

	GetByID(ctx context.Context, id uuid.UUID) (*domain.Appraisal, error)

	// GetByRequestID finds the appraisal of a request (one per request, without
	// comparables — used for cross-service linking).
	GetByRequestID(ctx context.Context, requestID uuid.UUID) (*domain.Appraisal, error)

	// Update changes appraiser_id/notes/market_value/report_s3_key only (never
	// status). prevUpdatedAt is the optimistic-lock guard.
	Update(ctx context.Context, a *domain.Appraisal, prevUpdatedAt time.Time) error

	// Complete flips status in_progress→completed and writes the event to the
	// outbox in the SAME transaction. completedAt is also the new updated_at.
	Complete(ctx context.Context, id uuid.UUID, completedAt time.Time, event domain.EventEnvelope) error

	// AddComparable inserts a comparable, guarded in SQL so it cannot attach to a
	// completed appraisal: 0 rows → ErrNotFound (no appraisal) or ErrConflict
	// (appraisal completed concurrently).
	AddComparable(ctx context.Context, comp *domain.Comparable) error

	// DeleteComparable removes one comparable of the given appraisal.
	DeleteComparable(ctx context.Context, appraisalID, comparableID uuid.UUID) error

	ListByAppraiserID(ctx context.Context, appraiserID uuid.UUID) ([]*domain.Appraisal, error)
	ListAll(ctx context.Context, limit, offset int) ([]*domain.Appraisal, error)
}
