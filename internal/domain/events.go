package domain

import (
	"time"

	"github.com/google/uuid"
)

// TopicReviewEvents carries every domain event produced by review-service.
// One topic per producing service; events are told apart by EventType (ADR-007).
const TopicReviewEvents = "review.events"

// EventTypeReportReady is emitted when an appraisal is completed and the report
// is ready for the client.
const EventTypeReportReady = "report.ready"

// EventVersion is the envelope schema version. Additive changes keep it; a
// breaking change bumps it.
const EventVersion = 1

// EventEnvelope is the JSON wire format for every event. RequestID is the
// correlation/message key so consumers stay ordered per request; Data holds the
// event-type-specific payload.
type EventEnvelope struct {
	EventID    uuid.UUID `json:"event_id"`
	EventType  string    `json:"event_type"`
	Version    int       `json:"version"`
	OccurredAt time.Time `json:"occurred_at"`
	RequestID  uuid.UUID `json:"request_id"`
	Data       any       `json:"data"`
}

// ReportReadyData is the payload for EventTypeReportReady. It carries enough for
// request-service to advance the request and notification-service to tell the
// client the report can be downloaded.
type ReportReadyData struct {
	AppraisalID uuid.UUID  `json:"appraisal_id"`
	AppraiserID *uuid.UUID `json:"appraiser_id,omitempty"`
	ReportS3Key *string    `json:"report_s3_key,omitempty"`
	CompletedAt time.Time  `json:"completed_at"`
}

// NewReportReadyEvent builds the envelope for a completed appraisal.
func NewReportReadyEvent(a Appraisal) EventEnvelope {
	completedAt := a.UpdatedAt
	if a.CompletedAt != nil {
		completedAt = *a.CompletedAt
	}
	return EventEnvelope{
		EventID:    uuid.New(),
		EventType:  EventTypeReportReady,
		Version:    EventVersion,
		OccurredAt: time.Now().UTC(),
		RequestID:  a.RequestID,
		Data: ReportReadyData{
			AppraisalID: a.ID,
			AppraiserID: a.AppraiserID,
			ReportS3Key: a.ReportS3Key,
			CompletedAt: completedAt,
		},
	}
}
