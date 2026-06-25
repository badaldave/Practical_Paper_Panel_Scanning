package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type ProcessingJob struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	DocumentID   uuid.UUID  `json:"document_id"`
	Status       string     `json:"status"` // 'pending', 'processing', 'completed', 'failed', 'retrying'
	ErrorMessage *string    `json:"error_message,omitempty"`
	Attempts     int        `json:"attempts"`
	MaxAttempts  int        `json:"max_attempts"`
	RunAt        time.Time  `json:"run_at"`
	LockedAt     *time.Time `json:"locked_at,omitempty"`
	LockedBy     *string    `json:"locked_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type JobAttempt struct {
	ID            uuid.UUID  `json:"id"`
	JobID         uuid.UUID  `json:"job_id"`
	AttemptNumber int        `json:"attempt_number"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	Status        string     `json:"status"`
	ErrorMessage  *string    `json:"error_message,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type QueueRepository interface {
	Enqueue(ctx context.Context, job *ProcessingJob) error
	Dequeue(ctx context.Context, workerID string) (*ProcessingJob, error)
	Complete(ctx context.Context, jobID uuid.UUID) error
	Fail(ctx context.Context, jobID uuid.UUID, reason string) error
	Retry(ctx context.Context, jobID uuid.UUID, reason string, nextRun time.Time) error
	RecordAttempt(ctx context.Context, attempt *JobAttempt) error
	UpdateAttempt(ctx context.Context, attemptID uuid.UUID, status string, errStr *string, endedAt time.Time) error
}
