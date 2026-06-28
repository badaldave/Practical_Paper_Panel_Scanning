package extraction

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
)

type UpdateCellRequest struct {
	DocumentID  uuid.UUID          `json:"document_id"`
	PageNumber  int                `json:"page_number"`
	RowIndex    int                `json:"row_index"`
	ColumnIndex int                `json:"column_index"`
	NewValue    string             `json:"value"`
	BBox        domain.BoundingBox `json:"bbox"`
	// Optional. Set by automated/derived fills (auto Batch/Subject, mobile→name
	// lookup) so they can mark a cell inferred and avoid polluting the correction
	// feedback loop with machine-generated values.
	IsInferred *bool    `json:"is_inferred,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
	Auto       bool     `json:"auto,omitempty"`
}

type ExtractionService struct {
	extRepo   domain.ExtractionRepository
	docRepo   domain.DocumentRepository
	auditRepo domain.AuditRepository
}

func NewExtractionService(er domain.ExtractionRepository, dr domain.DocumentRepository, ar domain.AuditRepository) *ExtractionService {
	return &ExtractionService{
		extRepo:   er,
		docRepo:   dr,
		auditRepo: ar,
	}
}

func (s *ExtractionService) GetActiveCells(ctx context.Context, docID uuid.UUID) ([]*domain.ExtractedCell, error) {
	return s.extRepo.GetActiveCellsByDocID(ctx, docID)
}

func (s *ExtractionService) UpdateCell(ctx context.Context, req UpdateCellRequest, updaterID uuid.UUID) error {
	// 1. Fetch document to ensure correct tenant scoping
	doc, err := s.docRepo.GetByID(ctx, req.DocumentID)
	if err != nil || doc == nil {
		return errors.New("document not found or unauthorized")
	}

	// 2. Fetch history to find original and current cell values
	history, err := s.extRepo.GetCellHistory(ctx, req.DocumentID, req.PageNumber, req.RowIndex, req.ColumnIndex)
	if err != nil {
		return fmt.Errorf("failed to fetch cell history: %w", err)
	}

	var originalCell *domain.ExtractedCell
	var activeCell *domain.ExtractedCell

	if len(history) > 0 {
		activeCell = history[0]                  // latest version
		originalCell = history[len(history)-1]    // oldest version (extracted by AI)
	}

	// If no changes, exit early
	if activeCell != nil && activeCell.CurrentValue == req.NewValue {
		return nil
	}

	// Determine original value if cell is brand new
	var origValue string
	if originalCell != nil {
		origValue = originalCell.OriginalValue
	} else {
		origValue = req.NewValue
	}

	// 3. Create updated cell. Human corrections override with 100% confidence;
	// automated/derived fills may pass their own confidence and inferred flag.
	confidence := 1.0
	if req.Confidence != nil {
		confidence = *req.Confidence
	}
	isInferred := false
	if req.IsInferred != nil {
		isInferred = *req.IsInferred
	}
	newCell := &domain.ExtractedCell{
		DocumentID:    req.DocumentID,
		PageNumber:    req.PageNumber,
		RowIndex:      req.RowIndex,
		ColumnIndex:   req.ColumnIndex,
		OriginalValue: origValue,
		CurrentValue:  req.NewValue,
		Confidence:    confidence,
		IsInferred:    isInferred,
		BBox:          req.BBox,
		CreatedBy:     &updaterID,
		UpdatedBy:     &updaterID,
	}

	if err := s.extRepo.SaveCell(ctx, newCell); err != nil {
		return fmt.Errorf("failed to save cell modification: %w", err)
	}

	// 4. Extract left and right spatial context for the learning loop
	var contextLeft, contextRight string
	siblings, err := s.extRepo.GetActiveCellsByDocID(ctx, req.DocumentID)
	if err == nil {
		for _, sib := range siblings {
			if sib.RowIndex == req.RowIndex {
				if sib.ColumnIndex == req.ColumnIndex-1 {
					contextLeft = sib.CurrentValue
				}
				if sib.ColumnIndex == req.ColumnIndex+1 {
					contextRight = sib.CurrentValue
				}
			}
		}
	}

	// 5. Store Correction Feedback
	// If active cell has a value that differs from the correction, save it. Skip
	// for automated/derived fills — those aren't human corrections and would
	// otherwise feed machine output back into the learning loop.
	if !req.Auto && activeCell != nil && activeCell.CurrentValue != req.NewValue {
		feedback := &domain.CorrectionFeedback{
			TenantID:            doc.TenantID,
			DocumentType:        doc.MimeType, // fallback to mime type or template name
			OriginalValue:       activeCell.CurrentValue, // previous extracted/saved value
			CorrectedValue:      req.NewValue,
			ContextLeft:         contextLeft,
			ContextRight:        contextRight,
			IsAppliedInTraining: false,
			CreatedBy:           &updaterID,
		}
		_ = s.extRepo.CreateFeedback(ctx, feedback)
	}

	// 6. Write Immutable Audit Log
	oldVal := map[string]interface{}{}
	if activeCell != nil {
		oldVal["value"] = activeCell.CurrentValue
		oldVal["version"] = activeCell.Version
	}
	newVal := map[string]interface{}{
		"value":   req.NewValue,
		"version": newCell.Version,
	}

	audit := &domain.AuditLog{
		TenantID:   doc.TenantID,
		UserID:     &updaterID,
		EntityType: "cell",
		EntityID:   newCell.ID,
		Action:     "updated",
		OldValue:   oldVal,
		NewValue:   newVal,
	}
	_ = s.auditRepo.Log(ctx, audit)

	return nil
}

func (s *ExtractionService) GetHistory(ctx context.Context, docID uuid.UUID, pageNum, rowIdx, colIdx int) ([]*domain.ExtractedCell, error) {
	return s.extRepo.GetCellHistory(ctx, docID, pageNum, rowIdx, colIdx)
}

// LookupExaminer resolves a mobile number to the best-known examiner name for the
// tenant (latest verified correction first, then the seeded registry).
func (s *ExtractionService) LookupExaminer(ctx context.Context, mobile string) (*domain.ExaminerMatch, error) {
	return s.extRepo.LookupExaminerByMobile(ctx, mobile)
}
