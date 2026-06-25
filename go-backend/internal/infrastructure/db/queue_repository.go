package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
)

type QueueRepository struct {
	db *DB
}

func NewQueueRepository(db *DB) *QueueRepository {
	return &QueueRepository{db: db}
}

func (r *QueueRepository) Enqueue(ctx context.Context, job *domain.ProcessingJob) error {
	query := `
		INSERT INTO processing_jobs (id, tenant_id, document_id, status, error_message, attempts, max_attempts, run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.RunAt.IsZero() {
		job.RunAt = now
	}

	_, err := r.db.ExecContext(ctx, query, job.ID, job.TenantID, job.DocumentID, job.Status, job.ErrorMessage, job.Attempts, job.MaxAttempts, job.RunAt, job.CreatedAt, job.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}
	return nil
}

func (r *QueueRepository) Dequeue(ctx context.Context, workerID string) (*domain.ProcessingJob, error) {
	// Atomic lock and fetch using FOR UPDATE SKIP LOCKED
	query := `
		UPDATE processing_jobs
		SET status = 'processing',
		    locked_at = $1,
		    locked_by = $2,
		    attempts = attempts + 1,
		    updated_at = $1
		WHERE id = (
			SELECT id
			FROM processing_jobs
			WHERE (status = 'pending' OR status = 'retrying') AND run_at <= $1
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, tenant_id, document_id, status, error_message, attempts, max_attempts, run_at, locked_at, locked_by, created_at, updated_at
	`
	now := time.Now()
	var job domain.ProcessingJob
	
	err := r.db.QueryRowContext(ctx, query, now, workerID).Scan(
		&job.ID,
		&job.TenantID,
		&job.DocumentID,
		&job.Status,
		&job.ErrorMessage,
		&job.Attempts,
		&job.MaxAttempts,
		&job.RunAt,
		&job.LockedAt,
		&job.LockedBy,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // No jobs available
		}
		return nil, fmt.Errorf("failed to dequeue job: %w", err)
	}

	return &job, nil
}

func (r *QueueRepository) Complete(ctx context.Context, jobID uuid.UUID) error {
	query := `
		UPDATE processing_jobs
		SET status = 'completed',
		    locked_at = NULL,
		    locked_by = NULL,
		    updated_at = $1
		WHERE id = $2
	`
	_, err := r.db.ExecContext(ctx, query, time.Now(), jobID)
	if err != nil {
		return fmt.Errorf("failed to complete job: %w", err)
	}
	return nil
}

func (r *QueueRepository) Fail(ctx context.Context, jobID uuid.UUID, reason string) error {
	query := `
		UPDATE processing_jobs
		SET status = 'failed',
		    error_message = $1,
		    locked_at = NULL,
		    locked_by = NULL,
		    updated_at = $2
		WHERE id = $3
	`
	_, err := r.db.ExecContext(ctx, query, reason, time.Now(), jobID)
	if err != nil {
		return fmt.Errorf("failed to mark job as failed: %w", err)
	}
	return nil
}

func (r *QueueRepository) Retry(ctx context.Context, jobID uuid.UUID, reason string, nextRun time.Time) error {
	query := `
		UPDATE processing_jobs
		SET status = 'retrying',
		    error_message = $1,
		    run_at = $2,
		    locked_at = NULL,
		    locked_by = NULL,
		    updated_at = $3
		WHERE id = $4
	`
	_, err := r.db.ExecContext(ctx, query, reason, nextRun, time.Now(), jobID)
	if err != nil {
		return fmt.Errorf("failed to set job for retry: %w", err)
	}
	return nil
}

func (r *QueueRepository) RecordAttempt(ctx context.Context, attempt *domain.JobAttempt) error {
	query := `
		INSERT INTO job_attempts (id, job_id, attempt_number, started_at, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	if attempt.ID == uuid.Nil {
		attempt.ID = uuid.New()
	}
	attempt.CreatedAt = time.Now()
	attempt.StartedAt = time.Now()

	_, err := r.db.ExecContext(ctx, query, attempt.ID, attempt.JobID, attempt.AttemptNumber, attempt.StartedAt, attempt.Status, attempt.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to record job attempt: %w", err)
	}
	return nil
}

func (r *QueueRepository) UpdateAttempt(ctx context.Context, attemptID uuid.UUID, status string, errStr *string, endedAt time.Time) error {
	query := `
		UPDATE job_attempts
		SET status = $1, error_message = $2, ended_at = $3
		WHERE id = $4
	`
	_, err := r.db.ExecContext(ctx, query, status, errStr, endedAt, attemptID)
	if err != nil {
		return fmt.Errorf("failed to update job attempt: %w", err)
	}
	return nil
}
