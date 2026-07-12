package service

import (
	"context"
	"encoding/json"

	"github.com/appraisal-crm/review-service/internal/domain"
	"github.com/google/uuid"
)

// UpdateInput carries the mutable fields. Nil pointers mean "leave as is", so a
// caller can patch one field without clobbering the others.
type UpdateInput struct {
	AppraiserID *uuid.UUID
	Notes       *string
	MarketValue *string
}

// ReportUploadResult is returned when a report slot is registered: the stored
// object key plus the URL the appraiser uploads the file bytes to.
type ReportUploadResult struct {
	S3Key     string
	UploadURL string
}

// AppraisalService is the business-logic contract used by the HTTP handlers and
// the Kafka consumer.
type AppraisalService interface {
	// CreateFromRequest is driven by the request.status_changed consumer when a
	// request enters the appraisal status. Idempotent per request.
	CreateFromRequest(ctx context.Context, requestID uuid.UUID) (created bool, err error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Appraisal, error)
	// GetByRequestID finds the appraisal of a request — for cross-service links.
	GetByRequestID(ctx context.Context, requestID uuid.UUID) (*domain.Appraisal, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*domain.Appraisal, error)
	Complete(ctx context.Context, id uuid.UUID) (*domain.Appraisal, error)
	// UploadReport registers the report file and returns a presigned upload URL.
	UploadReport(ctx context.Context, id uuid.UUID, filename string) (*ReportUploadResult, error)
	AddComparable(ctx context.Context, id uuid.UUID, data json.RawMessage) (*domain.Comparable, error)
	DeleteComparable(ctx context.Context, appraisalID, comparableID uuid.UUID) error
	ListByAppraiserID(ctx context.Context, appraiserID uuid.UUID) ([]*domain.Appraisal, error)
	ListAll(ctx context.Context, limit, offset int) ([]*domain.Appraisal, error)
}
