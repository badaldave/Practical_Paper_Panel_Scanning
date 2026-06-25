package domain

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"
)

type Document struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	Name       string     `json:"name"`
	FilePath   string     `json:"file_path"`
	FileSize   int64      `json:"file_size"`
	MimeType   string     `json:"mime_type"`
	Status             string     `json:"status"` // 'uploaded', 'queued', 'processing', 'extracted', 'failed', 'verified'
	ProgressPercentage int        `json:"progress_percentage"`
	TemplateID         *uuid.UUID `json:"template_id,omitempty"`
	UploadedBy         uuid.UUID  `json:"uploaded_by"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type DocumentPage struct {
	ID              uuid.UUID `json:"id"`
	DocumentID      uuid.UUID `json:"document_id"`
	PageNumber      int       `json:"page_number"`
	ImagePath       string    `json:"image_path"`
	Width           int       `json:"width"`
	Height          int       `json:"height"`
	Status          string    `json:"status"` // 'pending', 'processed', 'failed'
	CollegeCode     *string   `json:"college_code,omitempty"`
	CollegeName     *string   `json:"college_name,omitempty"`
	SubjectCode     *string   `json:"subject_code,omitempty"`
	SubjectName     *string   `json:"subject_name,omitempty"`
	Faculty         *string   `json:"faculty,omitempty"`
	TotalCandidates *int      `json:"total_candidates,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type DocumentRepository interface {
	Create(ctx context.Context, doc *Document) error
	GetByID(ctx context.Context, id uuid.UUID) (*Document, error)
	GetByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*Document, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	UpdateTemplate(ctx context.Context, id uuid.UUID, templateID uuid.UUID) error
	CreatePage(ctx context.Context, page *DocumentPage) error
	GetPages(ctx context.Context, docID uuid.UUID) ([]*DocumentPage, error)
	GetPageByNumber(ctx context.Context, docID uuid.UUID, pageNum int) (*DocumentPage, error)
	UpdatePageMetadata(ctx context.Context, docID uuid.UUID, pageNum int, page *DocumentPage) error
	Delete(ctx context.Context, id uuid.UUID) error
	DeleteAll(ctx context.Context, tenantID uuid.UUID) error
}

type StorageProvider interface {
	SaveFile(ctx context.Context, filename string, reader io.Reader) (string, error)
	GetFile(ctx context.Context, filePath string) (io.ReadCloser, error)
	DeleteFile(ctx context.Context, filePath string) error
}
