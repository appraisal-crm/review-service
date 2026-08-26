package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Status is the appraisal lifecycle. Only two states, one direction.
type Status string

const (
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// Appraisal is the aggregate: one evaluation for one request. It is created when
// request-service moves the request into `appraisal` (Kafka consumer), filled in
// by the appraiser (notes, market value, comparables), and closed with Complete —
// which emits report.ready. The calculation engine is out of scope until the
// formulas are formalized (risk R-001); market_value is entered manually.
type Appraisal struct {
	ID          uuid.UUID    `json:"id"`
	RequestID   uuid.UUID    `json:"request_id"`
	AppraiserID *uuid.UUID   `json:"appraiser_id,omitempty"` // nil until an appraiser takes it
	Status      Status       `json:"status"`
	Notes       *string      `json:"notes,omitempty"`
	MarketValue *string      `json:"market_value,omitempty"` // NUMERIC as string to avoid float rounding
	ReportS3Key     *string         `json:"report_s3_key,omitempty"`
	Comparables     []Comparable    `json:"comparables,omitempty"` // loaded on demand, not a column
	CalculationData json.RawMessage `json:"calculation_data,omitempty" swaggertype:"object"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// Comparable is one analog object the appraiser bases the evaluation on. Its
// fields are not formalized yet, so everything lives in free-form Data.
type Comparable struct {
	ID          uuid.UUID       `json:"id"`
	AppraisalID uuid.UUID       `json:"appraisal_id"`
	Data        json.RawMessage `json:"data" swaggertype:"object"`
	CreatedAt   time.Time       `json:"created_at"`
}
