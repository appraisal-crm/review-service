package handler

import (
	"encoding/json"

	"github.com/appraisal-crm/review-service/internal/domain"
	"github.com/google/uuid"
)

// updateAppraisalDTO — all fields optional; only the present ones are patched.
// market_value travels as a numeric string (NUMERIC in the DB, no floats).
type updateAppraisalDTO struct {
	AppraiserID *uuid.UUID `json:"appraiser_id"`
	Notes       *string    `json:"notes"        validate:"omitempty,max=10000"`
	MarketValue *string    `json:"market_value" validate:"omitempty,numeric"`
}

// addComparableDTO — free-form analog data; the field set is not formalized yet.
type addComparableDTO struct {
	Data json.RawMessage `json:"data" validate:"required" swaggertype:"object"`
}

// uploadReportDTO — the appraiser sends the original filename; we build the S3 key.
type uploadReportDTO struct {
	Filename string `json:"filename" validate:"required,min=1,max=255"`
}

// reportResponse returns the stored object key plus the URL to upload the bytes to.
type reportResponse struct {
	S3Key     string `json:"s3_key"`
	UploadURL string `json:"upload_url"`
}

// listAllResponse is the paginated envelope for the list endpoint.
type listAllResponse struct {
	Data  []*domain.Appraisal `json:"data"`
	Page  int                 `json:"page"`
	Limit int                 `json:"limit"`
}
