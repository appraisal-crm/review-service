package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/appraisal-crm/review-service/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRepository struct {
	db *pgxpool.Pool
}

// NewPostgresRepository returns the interface, not the concrete type — callers
// depend on the contract only.
func NewPostgresRepository(db *pgxpool.Pool) AppraisalRepository {
	return &postgresRepository{db: db}
}

// market_value is NUMERIC in Postgres but travels as *string in Go (no float
// rounding), hence the ::text on read; on write Postgres coerces the text
// parameter to NUMERIC itself.
const appraisalColumns = `id, request_id, appraiser_id, status, notes, market_value::text, report_s3_key, completed_at, created_at, updated_at`

func (r *postgresRepository) Create(ctx context.Context, a *domain.Appraisal) (bool, error) {
	// ON CONFLICT (request_id): one appraisal per request. A redelivered creation
	// event matches nothing new, so RowsAffected() is 0 → created=false.
	query := `
		INSERT INTO appraisals (id, request_id, appraiser_id, status, notes, market_value, report_s3_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (request_id) DO NOTHING
	`
	tag, err := r.db.Exec(ctx, query,
		a.ID, a.RequestID, a.AppraiserID, a.Status, a.Notes,
		a.MarketValue, a.ReportS3Key, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *postgresRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Appraisal, error) {
	query := `SELECT ` + appraisalColumns + ` FROM appraisals WHERE id = $1`
	var a domain.Appraisal
	err := r.db.QueryRow(ctx, query, id).Scan(
		&a.ID, &a.RequestID, &a.AppraiserID, &a.Status, &a.Notes,
		&a.MarketValue, &a.ReportS3Key, &a.CompletedAt, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	comparables, err := r.listComparables(ctx, id)
	if err != nil {
		return nil, err
	}
	a.Comparables = comparables
	return &a, nil
}

func (r *postgresRepository) GetByRequestID(ctx context.Context, requestID uuid.UUID) (*domain.Appraisal, error) {
	query := `SELECT ` + appraisalColumns + ` FROM appraisals WHERE request_id = $1`
	var a domain.Appraisal
	err := r.db.QueryRow(ctx, query, requestID).Scan(
		&a.ID, &a.RequestID, &a.AppraiserID, &a.Status, &a.Notes,
		&a.MarketValue, &a.ReportS3Key, &a.CompletedAt, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *postgresRepository) listComparables(ctx context.Context, appraisalID uuid.UUID) ([]domain.Comparable, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, appraisal_id, data, created_at
		FROM comparables
		WHERE appraisal_id = $1
		ORDER BY created_at
	`, appraisalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comparables := make([]domain.Comparable, 0)
	for rows.Next() {
		var c domain.Comparable
		if err := rows.Scan(&c.ID, &c.AppraisalID, &c.Data, &c.CreatedAt); err != nil {
			return nil, err
		}
		comparables = append(comparables, c)
	}
	return comparables, rows.Err()
}

// Update sets appraiser_id, notes, market_value and report_s3_key. It never
// touches status — status changes go through Complete only. Optimistic lock:
// updated_at must match.
func (r *postgresRepository) Update(ctx context.Context, a *domain.Appraisal, prevUpdatedAt time.Time) error {
	query := `
		UPDATE appraisals
		SET appraiser_id = $1, notes = $2, market_value = $3, report_s3_key = $4, updated_at = $5
		WHERE id = $6 AND updated_at = $7
	`
	tag, err := r.db.Exec(ctx, query,
		a.AppraiserID, a.Notes, a.MarketValue, a.ReportS3Key, a.UpdatedAt, a.ID, prevUpdatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return r.notFoundOrConflict(ctx, a.ID)
	}
	return nil
}

func (r *postgresRepository) Complete(ctx context.Context, id uuid.UUID, completedAt time.Time, event domain.EventEnvelope) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op after a successful Commit

	// CAS guard: only an in-progress appraisal can be completed. 0 rows → gone or
	// already completed; the tx rolls back, so no outbox row is written.
	tag, err := tx.Exec(ctx, `
		UPDATE appraisals
		SET status = $1, completed_at = $2, updated_at = $2
		WHERE id = $3 AND status = $4
	`, domain.StatusCompleted, completedAt, id, domain.StatusInProgress)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM appraisals WHERE id = $1)", id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return ErrConflict
	}

	if err := insertOutbox(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// insertOutbox writes the event into the outbox within the caller's tx, so the
// event and the state change commit atomically. aggregate_id = request_id keeps
// review.events ordered per request.
func insertOutbox(ctx context.Context, tx pgx.Tx, event domain.EventEnvelope) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox (event_id, topic, event_type, aggregate_id, payload)
		VALUES ($1, $2, $3, $4, $5)
	`, event.EventID, domain.TopicReviewEvents, event.EventType, event.RequestID, payload)
	return err
}

func (r *postgresRepository) AddComparable(ctx context.Context, comp *domain.Comparable) error {
	// The WHERE EXISTS guard makes insert-into-completed-appraisal impossible even
	// when Complete lands between the service's status check and this insert.
	tag, err := r.db.Exec(ctx, `
		INSERT INTO comparables (id, appraisal_id, data, created_at)
		SELECT $1, $2, $3, $4
		WHERE EXISTS (SELECT 1 FROM appraisals WHERE id = $2 AND status = $5)
	`, comp.ID, comp.AppraisalID, comp.Data, comp.CreatedAt, domain.StatusInProgress)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return r.notFoundOrConflict(ctx, comp.AppraisalID)
	}
	return nil
}

func (r *postgresRepository) DeleteComparable(ctx context.Context, appraisalID, comparableID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM comparables WHERE id = $1 AND appraisal_id = $2
	`, comparableID, appraisalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *postgresRepository) ListByAppraiserID(ctx context.Context, appraiserID uuid.UUID) ([]*domain.Appraisal, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+appraisalColumns+`
		FROM appraisals
		WHERE appraiser_id = $1
		ORDER BY created_at DESC
	`, appraiserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAppraisals(rows)
}

func (r *postgresRepository) ListAll(ctx context.Context, limit, offset int) ([]*domain.Appraisal, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+appraisalColumns+`
		FROM appraisals
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAppraisals(rows)
}

// scanAppraisals reads list rows (without comparables — lists stay light).
func scanAppraisals(rows pgx.Rows) ([]*domain.Appraisal, error) {
	appraisals := make([]*domain.Appraisal, 0)
	for rows.Next() {
		var a domain.Appraisal
		if err := rows.Scan(
			&a.ID, &a.RequestID, &a.AppraiserID, &a.Status, &a.Notes,
			&a.MarketValue, &a.ReportS3Key, &a.CompletedAt, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		appraisals = append(appraisals, &a)
	}
	return appraisals, rows.Err()
}

// notFoundOrConflict disambiguates a 0-row UPDATE: missing row → ErrNotFound,
// otherwise a concurrent modification → ErrConflict.
func (r *postgresRepository) notFoundOrConflict(ctx context.Context, id uuid.UUID) error {
	var exists bool
	if err := r.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM appraisals WHERE id = $1)", id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return ErrConflict
}
