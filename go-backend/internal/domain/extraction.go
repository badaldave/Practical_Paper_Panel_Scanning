package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Extraction struct {
	ID                uuid.UUID  `json:"id"`
	TenantID          uuid.UUID  `json:"tenant_id"`
	DocumentID        uuid.UUID  `json:"document_id"`
	TemplateVersionID *uuid.UUID `json:"template_version_id,omitempty"`
	Status            string     `json:"status"` // 'pending', 'completed', 'failed'
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ExtractedTable struct {
	ID           uuid.UUID   `json:"id"`
	ExtractionID uuid.UUID   `json:"extraction_id"`
	PageNumber   int         `json:"page_number"`
	TableIndex   int         `json:"table_index"`
	BoundingBox  BoundingBox `json:"bounding_box"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

type ExtractedRow struct {
	ID        uuid.UUID `json:"id"`
	TableID   uuid.UUID `json:"table_id"`
	RowIndex  int       `json:"row_index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ExtractedCell struct {
	ID            uuid.UUID   `json:"id"`
	DocumentID    uuid.UUID   `json:"document_id"`
	PageNumber    int         `json:"page_number"`
	RowIndex      int         `json:"row_index"`
	ColumnIndex   int         `json:"column_index"`
	OriginalValue string      `json:"original_value"`
	CurrentValue  string      `json:"current_value"`
	Confidence    float64     `json:"confidence"`
	BBox          BoundingBox `json:"bbox"`
	IsInferred    bool        `json:"is_inferred"`
	Version       int         `json:"version"`
	CreatedBy     *uuid.UUID  `json:"created_by,omitempty"`
	UpdatedBy     *uuid.UUID  `json:"updated_by,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

type CorrectionFeedback struct {
	ID                  uuid.UUID  `json:"id"`
	TenantID            uuid.UUID  `json:"tenant_id"`
	DocumentType        string     `json:"document_type"`
	OriginalValue       string     `json:"original_value"`
	CorrectedValue      string     `json:"corrected_value"`
	ContextLeft         string     `json:"context_left,omitempty"`
	ContextRight        string     `json:"context_right,omitempty"`
	ImageRegionPath     string     `json:"image_region_path,omitempty"`
	IsAppliedInTraining bool       `json:"is_applied_in_training"`
	CreatedBy           *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

// ExaminerMatch is the best-known examiner identity for a mobile number, resolved
// from prior human-verified corrections and the seeded registry.
type ExaminerMatch struct {
	Mobile    string `json:"mobile"`
	Name      string `json:"name"`
	Ambiguous bool   `json:"ambiguous"`
	Source    string `json:"source"` // "verified", "registry", or "" when unknown
}

type ExtractionRepository interface {
	CreateExtraction(ctx context.Context, ext *Extraction) error
	GetExtractionByDocID(ctx context.Context, docID uuid.UUID) (*Extraction, error)
	
	CreateTable(ctx context.Context, table *ExtractedTable) error
	GetTablesByExtraction(ctx context.Context, extID uuid.UUID) ([]*ExtractedTable, error)
	
	CreateRow(ctx context.Context, row *ExtractedRow) error
	GetRowsByTable(ctx context.Context, tableID uuid.UUID) ([]*ExtractedRow, error)
	
	SaveCell(ctx context.Context, cell *ExtractedCell) error
	GetActiveCellsByDocID(ctx context.Context, docID uuid.UUID) ([]*ExtractedCell, error)
	GetCellHistory(ctx context.Context, docID uuid.UUID, pageNum, rowIdx, colIdx int) ([]*ExtractedCell, error)
	
	CreateFeedback(ctx context.Context, feedback *CorrectionFeedback) error
	GetPendingFeedback(ctx context.Context, limit int) ([]*CorrectionFeedback, error)
	MarkFeedbackApplied(ctx context.Context, ids []uuid.UUID) error

	// LookupExaminerByMobile resolves a 10-digit mobile to the best-known examiner
	// name for the current tenant: the most recent human-verified correction wins
	// over the seeded registry. Returns a match with empty Name when unknown.
	LookupExaminerByMobile(ctx context.Context, mobile string) (*ExaminerMatch, error)
}
