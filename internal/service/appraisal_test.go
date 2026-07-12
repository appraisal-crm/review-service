package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/appraisal-crm/review-service/internal/domain"
	"github.com/appraisal-crm/review-service/internal/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRepo implements repository.AppraisalRepository. Each test wires only the
// func fields it needs; unset ones stay nil and simply aren't called.
type mockRepo struct {
	createFn   func(ctx context.Context, a *domain.Appraisal) (bool, error)
	getFn      func(ctx context.Context, id uuid.UUID) (*domain.Appraisal, error)
	updateFn   func(ctx context.Context, a *domain.Appraisal, prev time.Time) error
	completeFn func(ctx context.Context, id uuid.UUID, completedAt time.Time, ev domain.EventEnvelope) error
	addCompFn  func(ctx context.Context, comp *domain.Comparable) error
	delCompFn  func(ctx context.Context, appraisalID, comparableID uuid.UUID) error
}

func (m *mockRepo) Create(ctx context.Context, a *domain.Appraisal) (bool, error) {
	return m.createFn(ctx, a)
}
func (m *mockRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Appraisal, error) {
	return m.getFn(ctx, id)
}
func (m *mockRepo) Update(ctx context.Context, a *domain.Appraisal, prev time.Time) error {
	return m.updateFn(ctx, a, prev)
}
func (m *mockRepo) Complete(ctx context.Context, id uuid.UUID, completedAt time.Time, ev domain.EventEnvelope) error {
	return m.completeFn(ctx, id, completedAt, ev)
}
func (m *mockRepo) AddComparable(ctx context.Context, comp *domain.Comparable) error {
	return m.addCompFn(ctx, comp)
}
func (m *mockRepo) DeleteComparable(ctx context.Context, appraisalID, comparableID uuid.UUID) error {
	return m.delCompFn(ctx, appraisalID, comparableID)
}
func (m *mockRepo) ListByAppraiserID(ctx context.Context, id uuid.UUID) ([]*domain.Appraisal, error) {
	return nil, nil
}
func (m *mockRepo) ListAll(ctx context.Context, limit, offset int) ([]*domain.Appraisal, error) {
	return nil, nil
}

// stubStorage satisfies storage.ReportStorage without any I/O.
type stubStorage struct{}

func (stubStorage) KeyFor(id uuid.UUID, filename string) string { return "key/" + filename }
func (stubStorage) PresignUpload(_ context.Context, key string) (string, error) {
	return "https://upload/" + key, nil
}

func TestCreateFromRequest(t *testing.T) {
	tests := []struct {
		name        string
		repoCreated bool
		repoErr     error
		wantCreated bool
		wantErr     bool
	}{
		{name: "new appraisal", repoCreated: true, wantCreated: true},
		{name: "duplicate request is a no-op", repoCreated: false, wantCreated: false},
		{name: "repo error propagates", repoErr: errors.New("boom"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{createFn: func(_ context.Context, a *domain.Appraisal) (bool, error) {
				assert.Equal(t, domain.StatusInProgress, a.Status) // always born in progress
				return tt.repoCreated, tt.repoErr
			}}
			svc := NewAppraisalService(repo, stubStorage{})
			created, err := svc.CreateFromRequest(context.Background(), uuid.New())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCreated, created)
		})
	}
}

func TestComplete(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name       string
		current    domain.Status
		getErr     error
		completeFn func(t *testing.T) func(context.Context, uuid.UUID, time.Time, domain.EventEnvelope) error
		wantErr    error
	}{
		{
			name:    "in progress completes and emits event",
			current: domain.StatusInProgress,
			completeFn: func(t *testing.T) func(context.Context, uuid.UUID, time.Time, domain.EventEnvelope) error {
				return func(_ context.Context, _ uuid.UUID, _ time.Time, ev domain.EventEnvelope) error {
					assert.Equal(t, domain.EventTypeReportReady, ev.EventType)
					return nil
				}
			},
		},
		{
			name:    "already completed is an invalid transition",
			current: domain.StatusCompleted,
			wantErr: domain.ErrInvalidStatus,
		},
		{
			name:    "not found",
			getErr:  repository.ErrNotFound,
			wantErr: domain.ErrNotFound,
		},
		{
			name:    "repo conflict maps to domain conflict",
			current: domain.StatusInProgress,
			completeFn: func(t *testing.T) func(context.Context, uuid.UUID, time.Time, domain.EventEnvelope) error {
				return func(_ context.Context, _ uuid.UUID, _ time.Time, _ domain.EventEnvelope) error {
					return repository.ErrConflict
				}
			},
			wantErr: domain.ErrConflict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{
				getFn: func(_ context.Context, _ uuid.UUID) (*domain.Appraisal, error) {
					if tt.getErr != nil {
						return nil, tt.getErr
					}
					return &domain.Appraisal{ID: id, RequestID: uuid.New(), Status: tt.current}, nil
				},
			}
			if tt.completeFn != nil {
				repo.completeFn = tt.completeFn(t)
			}
			svc := NewAppraisalService(repo, stubStorage{})
			out, err := svc.Complete(context.Background(), id)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, domain.StatusCompleted, out.Status)
			require.NotNil(t, out.CompletedAt)
		})
	}
}

func TestUpdateAppliesFields(t *testing.T) {
	id := uuid.New()
	appraiser := uuid.New()
	notes := "three comparables found"
	value := "12500000.00"
	repo := &mockRepo{
		getFn: func(_ context.Context, _ uuid.UUID) (*domain.Appraisal, error) {
			return &domain.Appraisal{ID: id, Status: domain.StatusInProgress}, nil
		},
		updateFn: func(_ context.Context, _ *domain.Appraisal, _ time.Time) error { return nil },
	}
	svc := NewAppraisalService(repo, stubStorage{})
	out, err := svc.Update(context.Background(), id, UpdateInput{
		AppraiserID: &appraiser,
		Notes:       &notes,
		MarketValue: &value,
	})
	require.NoError(t, err)
	assert.Equal(t, appraiser, *out.AppraiserID)
	assert.Equal(t, notes, *out.Notes)
	assert.Equal(t, value, *out.MarketValue)
}

func TestUpdateRejectsCompleted(t *testing.T) {
	repo := &mockRepo{
		getFn: func(_ context.Context, _ uuid.UUID) (*domain.Appraisal, error) {
			return &domain.Appraisal{ID: uuid.New(), Status: domain.StatusCompleted}, nil
		},
	}
	svc := NewAppraisalService(repo, stubStorage{})
	_, err := svc.Update(context.Background(), uuid.New(), UpdateInput{})
	require.ErrorIs(t, err, domain.ErrInvalidStatus)
}

func TestUpdateConflict(t *testing.T) {
	repo := &mockRepo{
		getFn: func(_ context.Context, _ uuid.UUID) (*domain.Appraisal, error) {
			return &domain.Appraisal{ID: uuid.New(), Status: domain.StatusInProgress}, nil
		},
		updateFn: func(_ context.Context, _ *domain.Appraisal, _ time.Time) error {
			return repository.ErrConflict
		},
	}
	svc := NewAppraisalService(repo, stubStorage{})
	_, err := svc.Update(context.Background(), uuid.New(), UpdateInput{})
	require.ErrorIs(t, err, domain.ErrConflict)
}

func TestUploadReportRejectsCompleted(t *testing.T) {
	repo := &mockRepo{
		getFn: func(_ context.Context, _ uuid.UUID) (*domain.Appraisal, error) {
			return &domain.Appraisal{ID: uuid.New(), Status: domain.StatusCompleted}, nil
		},
	}
	svc := NewAppraisalService(repo, stubStorage{})
	_, err := svc.UploadReport(context.Background(), uuid.New(), "report.pdf")
	require.ErrorIs(t, err, domain.ErrInvalidStatus)
}

func TestUploadReportSuccess(t *testing.T) {
	var savedKey *string
	repo := &mockRepo{
		getFn: func(_ context.Context, _ uuid.UUID) (*domain.Appraisal, error) {
			return &domain.Appraisal{ID: uuid.New(), Status: domain.StatusInProgress}, nil
		},
		updateFn: func(_ context.Context, a *domain.Appraisal, _ time.Time) error {
			savedKey = a.ReportS3Key
			return nil
		},
	}
	svc := NewAppraisalService(repo, stubStorage{})
	res, err := svc.UploadReport(context.Background(), uuid.New(), "report.pdf")
	require.NoError(t, err)
	require.NotNil(t, savedKey)
	assert.Equal(t, "key/report.pdf", *savedKey)
	assert.Equal(t, "key/report.pdf", res.S3Key)
	assert.Equal(t, "https://upload/key/report.pdf", res.UploadURL)
}

func TestAddComparableRejectsCompleted(t *testing.T) {
	repo := &mockRepo{
		getFn: func(_ context.Context, _ uuid.UUID) (*domain.Appraisal, error) {
			return &domain.Appraisal{ID: uuid.New(), Status: domain.StatusCompleted}, nil
		},
	}
	svc := NewAppraisalService(repo, stubStorage{})
	_, err := svc.AddComparable(context.Background(), uuid.New(), json.RawMessage(`{"price":1}`))
	require.ErrorIs(t, err, domain.ErrInvalidStatus)
}

func TestAddComparableSuccess(t *testing.T) {
	id := uuid.New()
	repo := &mockRepo{
		getFn: func(_ context.Context, _ uuid.UUID) (*domain.Appraisal, error) {
			return &domain.Appraisal{ID: id, Status: domain.StatusInProgress}, nil
		},
		addCompFn: func(_ context.Context, comp *domain.Comparable) error {
			assert.Equal(t, id, comp.AppraisalID)
			return nil
		},
	}
	svc := NewAppraisalService(repo, stubStorage{})
	comp, err := svc.AddComparable(context.Background(), id, json.RawMessage(`{"price":9500000,"address":"Lenina 5"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"price":9500000,"address":"Lenina 5"}`, string(comp.Data))
}

func TestAddComparableRaceMapsToInvalidStatus(t *testing.T) {
	// Complete lands between the status check and the insert → repo returns
	// ErrConflict from the SQL guard → service reports an invalid transition.
	repo := &mockRepo{
		getFn: func(_ context.Context, _ uuid.UUID) (*domain.Appraisal, error) {
			return &domain.Appraisal{ID: uuid.New(), Status: domain.StatusInProgress}, nil
		},
		addCompFn: func(_ context.Context, _ *domain.Comparable) error {
			return repository.ErrConflict
		},
	}
	svc := NewAppraisalService(repo, stubStorage{})
	_, err := svc.AddComparable(context.Background(), uuid.New(), json.RawMessage(`{}`))
	require.ErrorIs(t, err, domain.ErrInvalidStatus)
}

func TestDeleteComparable(t *testing.T) {
	tests := []struct {
		name    string
		status  domain.Status
		delErr  error
		wantErr error
	}{
		{name: "deletes from in-progress appraisal", status: domain.StatusInProgress},
		{name: "rejects completed appraisal", status: domain.StatusCompleted, wantErr: domain.ErrInvalidStatus},
		{name: "missing comparable", status: domain.StatusInProgress, delErr: repository.ErrNotFound, wantErr: domain.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{
				getFn: func(_ context.Context, _ uuid.UUID) (*domain.Appraisal, error) {
					return &domain.Appraisal{ID: uuid.New(), Status: tt.status}, nil
				},
				delCompFn: func(_ context.Context, _, _ uuid.UUID) error {
					return tt.delErr
				},
			}
			svc := NewAppraisalService(repo, stubStorage{})
			err := svc.DeleteComparable(context.Background(), uuid.New(), uuid.New())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
