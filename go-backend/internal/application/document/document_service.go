package document

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
	"university-result-processing/backend/internal/pkg/contextutil"
)

type UploadDocumentRequest struct {
	Name     string
	MimeType string
	Size     int64
	File     io.Reader
}

type DocumentService struct {
	docRepo     domain.DocumentRepository
	queueRepo   domain.QueueRepository
	storage     domain.StorageProvider
}

func NewDocumentService(dr domain.DocumentRepository, qr domain.QueueRepository, sp domain.StorageProvider) *DocumentService {
	return &DocumentService{
		docRepo:   dr,
		queueRepo: qr,
		storage:   sp,
	}
}

func (s *DocumentService) Upload(ctx context.Context, req UploadDocumentRequest) (*domain.Document, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, err
	}

	userID, err := contextutil.GetUserID(ctx)
	if err != nil {
		return nil, err
	}

	// Validate File Type
	ext := filepath.Ext(req.Name)
	if ext != ".pdf" && ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		return nil, errors.New("unsupported file format; only PDF, PNG, and JPG/JPEG are supported")
	}

	// Max File Size: 500 MB (500 * 1024 * 1024 bytes)
	if req.Size > 500*1024*1024 {
		return nil, errors.New("file size exceeds the maximum limit of 500 MB")
	}

	// Generate storage path name: tenant_id/doc_id_filename
	docID := uuid.New()
	filename := fmt.Sprintf("%s/%s%s", tenantID.String(), docID.String(), ext)
	
	filePath, err := s.storage.SaveFile(ctx, filename, req.File)
	if err != nil {
		return nil, fmt.Errorf("failed to save file to storage: %w", err)
	}

	// Save to DB
	doc := &domain.Document{
		ID:         docID,
		TenantID:   tenantID,
		Name:       req.Name,
		FilePath:   filePath,
		FileSize:   req.Size,
		MimeType:   req.MimeType,
		Status:     "uploaded",
		UploadedBy: userID,
	}

	if err := s.docRepo.Create(ctx, doc); err != nil {
		// Clean up file if DB save fails
		_ = s.storage.DeleteFile(ctx, filePath)
		return nil, fmt.Errorf("failed to save document record: %w", err)
	}

	// Enqueue OCR Job
	job := &domain.ProcessingJob{
		ID:          uuid.New(),
		TenantID:    tenantID,
		DocumentID:  docID,
		Status:      "pending",
		Attempts:    0,
		MaxAttempts: 3,
		RunAt:       time.Now(),
	}

	if err := s.queueRepo.Enqueue(ctx, job); err != nil {
		// Log error, but don't fail upload since document is in DB (status 'uploaded')
		// We'll update the status to 'failed' if queue enqueue fails, or keep it 'uploaded' for retry
		_ = s.docRepo.UpdateStatus(ctx, docID, "failed")
		return nil, fmt.Errorf("failed to queue processing job: %w", err)
	}

	// Update status to queued
	_ = s.docRepo.UpdateStatus(ctx, docID, "queued")
	doc.Status = "queued"

	return doc, nil
}

func (s *DocumentService) GetList(ctx context.Context, limit, offset int) ([]*domain.Document, error) {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return nil, err
	}
	return s.docRepo.GetByTenant(ctx, tenantID, limit, offset)
}

func (s *DocumentService) GetDetails(ctx context.Context, id uuid.UUID) (*domain.Document, []*domain.DocumentPage, error) {
	doc, err := s.docRepo.GetByID(ctx, id)
	if err != nil || doc == nil {
		return nil, nil, err
	}

	pages, err := s.docRepo.GetPages(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	return doc, pages, nil
}

func (s *DocumentService) UpdatePageMetadata(ctx context.Context, docID uuid.UUID, pageNum int, page *domain.DocumentPage) error {
	existingPage, err := s.docRepo.GetPageByNumber(ctx, docID, pageNum)
	if err != nil {
		return err
	}
	if existingPage == nil {
		return errors.New("page not found")
	}

	existingPage.CollegeCode = page.CollegeCode
	existingPage.CollegeName = page.CollegeName
	existingPage.SubjectCode = page.SubjectCode
	existingPage.SubjectName = page.SubjectName
	existingPage.Faculty = page.Faculty
	existingPage.TotalCandidates = page.TotalCandidates

	return s.docRepo.UpdatePageMetadata(ctx, docID, pageNum, existingPage)
}

func (s *DocumentService) Delete(ctx context.Context, id uuid.UUID) error {
	doc, err := s.docRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if doc == nil {
		return errors.New("document not found")
	}

	pages, err := s.docRepo.GetPages(ctx, id)
	if err != nil {
		return err
	}

	// Delete from repository first (so DB constraints are satisfied)
	err = s.docRepo.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Clean up files on disk
	_ = s.storage.DeleteFile(ctx, doc.FilePath)
	for _, page := range pages {
		_ = s.storage.DeleteFile(ctx, page.ImagePath)
	}

	return nil
}

func (s *DocumentService) DeleteAll(ctx context.Context) error {
	tenantID, err := contextutil.GetTenantID(ctx)
	if err != nil {
		return err
	}

	// List all documents to clean up files (up to 1000)
	docs, err := s.docRepo.GetByTenant(ctx, tenantID, 1000, 0)
	if err != nil {
		return err
	}

	// For each document, retrieve page paths to delete
	var allFiles []string
	for _, doc := range docs {
		allFiles = append(allFiles, doc.FilePath)
		pages, err := s.docRepo.GetPages(ctx, doc.ID)
		if err == nil {
			for _, page := range pages {
				allFiles = append(allFiles, page.ImagePath)
			}
		}
	}

	// Delete records from database
	err = s.docRepo.DeleteAll(ctx, tenantID)
	if err != nil {
		return err
	}

	// Clean up files on disk
	for _, fp := range allFiles {
		_ = s.storage.DeleteFile(ctx, fp)
	}

	return nil
}
