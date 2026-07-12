package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/appraisal-crm/review-service/internal/domain"
	"github.com/appraisal-crm/review-service/internal/repository"
	"github.com/appraisal-crm/review-service/internal/storage"
	"github.com/google/uuid"
)

type appraisalService struct {
	repo    repository.AppraisalRepository
	storage storage.ReportStorage
}

func NewAppraisalService(repo repository.AppraisalRepository, store storage.ReportStorage) AppraisalService {
	return &appraisalService{repo: repo, storage: store}
}

func (s *appraisalService) CreateFromRequest(ctx context.Context, requestID uuid.UUID) (bool, error) {
	now := time.Now()
	a := &domain.Appraisal{
		ID:        uuid.New(),
		RequestID: requestID,
		Status:    domain.StatusInProgress,
		CreatedAt: now,
		UpdatedAt: now,
	}
	created, err := s.repo.Create(ctx, a)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create appraisal", "error", err, "request_id", requestID)
		return false, err
	}
	if created {
		slog.InfoContext(ctx, "appraisal created", "appraisal_id", a.ID, "request_id", requestID)
	} else {
		slog.InfoContext(ctx, "appraisal already exists for request, skipping", "request_id", requestID)
	}
	return created, nil
}

func (s *appraisalService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Appraisal, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		slog.ErrorContext(ctx, "failed to get appraisal", "error", err, "appraisal_id", id)
		return nil, err
	}
	return a, nil
}

func (s *appraisalService) GetByRequestID(ctx context.Context, requestID uuid.UUID) (*domain.Appraisal, error) {
	a, err := s.repo.GetByRequestID(ctx, requestID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		slog.ErrorContext(ctx, "failed to get appraisal by request", "error", err, "request_id", requestID)
		return nil, err
	}
	return a, nil
}

func (s *appraisalService) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*domain.Appraisal, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	// A completed appraisal is frozen — the report is already out.
	if a.Status != domain.StatusInProgress {
		slog.WarnContext(ctx, "cannot update a completed appraisal", "appraisal_id", id)
		return nil, domain.ErrInvalidStatus
	}

	// Patch only the fields the caller provided.
	if in.AppraiserID != nil {
		a.AppraiserID = in.AppraiserID
	}
	if in.Notes != nil {
		a.Notes = in.Notes
	}
	if in.MarketValue != nil {
		a.MarketValue = in.MarketValue
	}

	prevUpdatedAt := a.UpdatedAt
	a.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, a, prevUpdatedAt); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		if errors.Is(err, repository.ErrConflict) {
			slog.WarnContext(ctx, "concurrent update detected", "appraisal_id", id)
			return nil, domain.ErrConflict
		}
		slog.ErrorContext(ctx, "failed to update appraisal", "error", err, "appraisal_id", id)
		return nil, err
	}
	slog.InfoContext(ctx, "appraisal updated", "appraisal_id", id)
	return a, nil
}

func (s *appraisalService) Complete(ctx context.Context, id uuid.UUID) (*domain.Appraisal, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	// State machine: only in_progress → completed is allowed.
	if a.Status != domain.StatusInProgress {
		slog.WarnContext(ctx, "cannot complete appraisal in its current status", "appraisal_id", id, "status", a.Status)
		return nil, domain.ErrInvalidStatus
	}

	completedAt := time.Now()
	a.Status = domain.StatusCompleted
	a.CompletedAt = &completedAt
	a.UpdatedAt = completedAt
	event := domain.NewReportReadyEvent(*a)

	if err := s.repo.Complete(ctx, id, completedAt, event); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		if errors.Is(err, repository.ErrConflict) {
			slog.WarnContext(ctx, "concurrent completion detected", "appraisal_id", id)
			return nil, domain.ErrConflict
		}
		slog.ErrorContext(ctx, "failed to complete appraisal", "error", err, "appraisal_id", id)
		return nil, err
	}

	slog.InfoContext(ctx, "appraisal completed", "appraisal_id", id, "request_id", a.RequestID)
	return a, nil
}

func (s *appraisalService) UploadReport(ctx context.Context, id uuid.UUID, filename string) (*ReportUploadResult, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	// The report belongs to the working phase; once completed the appraisal is frozen.
	if a.Status != domain.StatusInProgress {
		slog.WarnContext(ctx, "cannot upload report for a completed appraisal", "appraisal_id", id)
		return nil, domain.ErrInvalidStatus
	}

	key := s.storage.KeyFor(id, filename)
	uploadURL, err := s.storage.PresignUpload(ctx, key)
	if err != nil {
		slog.ErrorContext(ctx, "failed to presign upload", "error", err, "appraisal_id", id)
		return nil, err
	}

	a.ReportS3Key = &key
	prevUpdatedAt := a.UpdatedAt
	a.UpdatedAt = time.Now()
	// Optimistic lock also guards the frozen rule: Complete bumps updated_at, so a
	// concurrent completion turns this into ErrConflict instead of a silent write.
	if err := s.repo.Update(ctx, a, prevUpdatedAt); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		if errors.Is(err, repository.ErrConflict) {
			slog.WarnContext(ctx, "concurrent update detected", "appraisal_id", id)
			return nil, domain.ErrConflict
		}
		slog.ErrorContext(ctx, "failed to store report key", "error", err, "appraisal_id", id)
		return nil, err
	}
	slog.InfoContext(ctx, "report registered", "appraisal_id", id, "s3_key", key)
	return &ReportUploadResult{S3Key: key, UploadURL: uploadURL}, nil
}

func (s *appraisalService) AddComparable(ctx context.Context, id uuid.UUID, data json.RawMessage) (*domain.Comparable, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	// Comparables feed the evaluation; once completed the appraisal is frozen.
	if a.Status != domain.StatusInProgress {
		slog.WarnContext(ctx, "cannot add comparable to a completed appraisal", "appraisal_id", id)
		return nil, domain.ErrInvalidStatus
	}

	comp := domain.Comparable{
		ID:          uuid.New(),
		AppraisalID: id,
		Data:        data,
		CreatedAt:   time.Now(),
	}
	if err := s.repo.AddComparable(ctx, &comp); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		if errors.Is(err, repository.ErrConflict) {
			// The appraisal was completed between our status check and the insert.
			return nil, domain.ErrInvalidStatus
		}
		slog.ErrorContext(ctx, "failed to add comparable", "error", err, "appraisal_id", id)
		return nil, err
	}
	slog.InfoContext(ctx, "comparable added", "appraisal_id", id, "comparable_id", comp.ID)
	return &comp, nil
}

func (s *appraisalService) DeleteComparable(ctx context.Context, appraisalID, comparableID uuid.UUID) error {
	a, err := s.repo.GetByID(ctx, appraisalID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.ErrNotFound
		}
		return err
	}
	if a.Status != domain.StatusInProgress {
		slog.WarnContext(ctx, "cannot delete comparable of a completed appraisal", "appraisal_id", appraisalID)
		return domain.ErrInvalidStatus
	}

	if err := s.repo.DeleteComparable(ctx, appraisalID, comparableID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.ErrNotFound
		}
		slog.ErrorContext(ctx, "failed to delete comparable", "error", err, "appraisal_id", appraisalID, "comparable_id", comparableID)
		return err
	}
	slog.InfoContext(ctx, "comparable deleted", "appraisal_id", appraisalID, "comparable_id", comparableID)
	return nil
}

func (s *appraisalService) ListByAppraiserID(ctx context.Context, appraiserID uuid.UUID) ([]*domain.Appraisal, error) {
	appraisals, err := s.repo.ListByAppraiserID(ctx, appraiserID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list appraisals", "error", err, "appraiser_id", appraiserID)
		return nil, err
	}
	return appraisals, nil
}

func (s *appraisalService) ListAll(ctx context.Context, limit, offset int) ([]*domain.Appraisal, error) {
	appraisals, err := s.repo.ListAll(ctx, limit, offset)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list all appraisals", "error", err)
		return nil, err
	}
	return appraisals, nil
}
