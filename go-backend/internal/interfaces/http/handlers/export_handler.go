package handlers

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"university-result-processing/backend/internal/application/settings"
	"university-result-processing/backend/internal/domain"
)

type ExportHandler struct {
	docRepo     domain.DocumentRepository
	extRepo     domain.ExtractionRepository
	auditRepo   domain.AuditRepository
	settingsSvc *settings.SettingsService
}

func NewExportHandler(dr domain.DocumentRepository, er domain.ExtractionRepository, ar domain.AuditRepository, ss *settings.SettingsService) *ExportHandler {
	return &ExportHandler{
		docRepo:     dr,
		extRepo:     er,
		auditRepo:   ar,
		settingsSvc: ss,
	}
}

// exportColLabels maps a stored cell column_index to its display name. The grid
// stores Subject Code at col 0 and Subject ID at col 4 (split from one merged
// cell); Batch/Name/Mobile stay at 1/2/3. Display/export order is given by
// exportColOrder, not the raw index.
var exportColLabels = map[int]string{
	4: "Subject ID",
	0: "Subject Code",
	1: "Batch",
	2: "Examiner Name",
	3: "Mobile Number",
}

// exportColOrder returns column indices in display order — Subject ID, Subject
// Code, Batch, Examiner Name, Mobile Number, then any extra columns — limited to
// those that actually exist (<= maxCol). Documents extracted before the subject
// split have no col 4, so Subject ID is simply omitted for them.
func exportColOrder(maxCol int) []int {
	preferred := []int{4, 0, 1, 2, 3}
	seen := make(map[int]bool)
	out := make([]int, 0, maxCol+1)
	for _, c := range preferred {
		if c <= maxCol {
			out = append(out, c)
			seen[c] = true
		}
	}
	for c := 0; c <= maxCol; c++ {
		if !seen[c] {
			out = append(out, c)
		}
	}
	return out
}

func exportColName(c int) string {
	if n, ok := exportColLabels[c]; ok {
		return n
	}
	return fmt.Sprintf("Col_%d", c)
}

// pageMetaHeaders are the per-page metadata columns shown in the UI and appended
// to every exported row.
var pageMetaHeaders = []string{"College Code", "College Name", "Subject Name", "Faculty", "Total Candidates"}

func pageMetaValues(p *domain.DocumentPage) []string {
	str := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	if p == nil {
		return make([]string, len(pageMetaHeaders))
	}
	cand := ""
	if p.TotalCandidates != nil {
		cand = strconv.Itoa(*p.TotalCandidates)
	}
	return []string{str(p.CollegeCode), str(p.CollegeName), str(p.SubjectName), str(p.Faculty), cand}
}

// includeConfidence honors the tenant's export_include_confidence setting,
// defaulting to true if settings can't be read.
func (h *ExportHandler) includeConfidence(ctx context.Context) bool {
	if h.settingsSvc == nil {
		return true
	}
	s, err := h.settingsSvc.Get(ctx)
	if err != nil {
		return true
	}
	if v, ok := s.Settings["export_include_confidence"].(bool); ok {
		return v
	}
	return true
}

func (h *ExportHandler) ExportCSV(c *gin.Context) {
	docIDStr := c.Param("id")
	docID, err := uuid.Parse(docIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID format"})
		return
	}

	// Fetch document info
	doc, err := h.docRepo.GetByID(c.Request.Context(), docID)
	if err != nil || doc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Document not found"})
		return
	}

	// Fetch active cells
	cells, err := h.extRepo.GetActiveCellsByDocID(c.Request.Context(), docID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch cells: " + err.Error()})
		return
	}

	// 1. Determine maxCol
	maxCol := -1
	for _, cell := range cells {
		if cell.ColumnIndex > maxCol {
			maxCol = cell.ColumnIndex
		}
	}

	if maxCol == -1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No extracted data available to export"})
		return
	}

	// 2. Group cells by (PageNumber, RowIndex)
	type RowKey struct {
		Page int
		Row  int
	}

	var rowKeys []RowKey
	rowMap := make(map[RowKey]map[int]*domain.ExtractedCell)

	for _, cell := range cells {
		key := RowKey{Page: cell.PageNumber, Row: cell.RowIndex}
		if _, exists := rowMap[key]; !exists {
			rowKeys = append(rowKeys, key)
			rowMap[key] = make(map[int]*domain.ExtractedCell)
		}
		rowMap[key][cell.ColumnIndex] = cell
	}

	// Per-page metadata (college, subject name, faculty, candidate count) shown in
	// the UI is appended to every row.
	pages, _ := h.docRepo.GetPages(c.Request.Context(), docID)
	pageMeta := make(map[int]*domain.DocumentPage)
	for _, p := range pages {
		pageMeta[p.PageNumber] = p
	}
	colOrder := exportColOrder(maxCol)

	// 3. Stream CSV to browser
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=export_%s_%d.csv", doc.Name, time.Now().Unix()))
	c.Header("Content-Type", "text/csv")

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write CSV Header: Page, Row, [value cols in display order], [page metadata], [confidences]
	header := []string{"Page", "Row"}
	for _, col := range colOrder {
		header = append(header, exportColName(col))
	}
	header = append(header, pageMetaHeaders...)
	includeConf := h.includeConfidence(c.Request.Context())
	if includeConf {
		for _, col := range colOrder {
			header = append(header, exportColName(col)+"_Confidence")
		}
	}
	_ = writer.Write(header)

	// Write rows
	for _, key := range rowKeys {
		cols := rowMap[key]
		csvRow := []string{strconv.Itoa(key.Page), strconv.Itoa(key.Row)}

		// Values
		for _, col := range colOrder {
			val := ""
			if cell, exists := cols[col]; exists {
				val = cell.CurrentValue
			}
			csvRow = append(csvRow, val)
		}
		// Page metadata
		csvRow = append(csvRow, pageMetaValues(pageMeta[key.Page])...)
		// Confidences
		if includeConf {
			for _, col := range colOrder {
				conf := ""
				if cell, exists := cols[col]; exists {
					conf = fmt.Sprintf("%.2f%%", cell.Confidence*100)
				}
				csvRow = append(csvRow, conf)
			}
		}
		_ = writer.Write(csvRow)
	}

	// 4. Log audit event
	userIDVal, exists := c.Get("user_id")
	var userID *uuid.UUID
	if exists {
		uid := userIDVal.(uuid.UUID)
		userID = &uid
	}

	audit := &domain.AuditLog{
		TenantID:   doc.TenantID,
		UserID:     userID,
		EntityType: "document",
		EntityID:   doc.ID,
		Action:     "exported",
		OldValue:   nil,
		NewValue:   map[string]interface{}{"format": "CSV", "cells_count": len(cells)},
	}
	_ = h.auditRepo.Log(c.Request.Context(), audit)
}

func (h *ExportHandler) ExportExcel(c *gin.Context) {
	docIDStr := c.Param("id")
	docID, err := uuid.Parse(docIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID format"})
		return
	}

	doc, err := h.docRepo.GetByID(c.Request.Context(), docID)
	if err != nil || doc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Document not found"})
		return
	}

	cells, err := h.extRepo.GetActiveCellsByDocID(c.Request.Context(), docID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch cells: " + err.Error()})
		return
	}

	// 1. Determine maxCol
	maxCol := -1
	for _, cell := range cells {
		if cell.ColumnIndex > maxCol {
			maxCol = cell.ColumnIndex
		}
	}

	if maxCol == -1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No extracted data available to export"})
		return
	}

	// 2. Group cells by (PageNumber, RowIndex)
	type RowKey struct {
		Page int
		Row  int
	}

	var rowKeys []RowKey
	rowMap := make(map[RowKey]map[int]*domain.ExtractedCell)

	for _, cell := range cells {
		key := RowKey{Page: cell.PageNumber, Row: cell.RowIndex}
		if _, exists := rowMap[key]; !exists {
			rowKeys = append(rowKeys, key)
			rowMap[key] = make(map[int]*domain.ExtractedCell)
		}
		rowMap[key][cell.ColumnIndex] = cell
	}

	pages, _ := h.docRepo.GetPages(c.Request.Context(), docID)
	pageMeta := make(map[int]*domain.DocumentPage)
	for _, p := range pages {
		pageMeta[p.PageNumber] = p
	}
	colOrder := exportColOrder(maxCol)

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=export_%s_%d.xls", doc.Name, time.Now().Unix()))
	c.Header("Content-Type", "application/vnd.ms-excel")

	writer := csv.NewWriter(c.Writer)
	writer.Comma = '\t' // Excel Tab-delimited
	defer writer.Flush()

	// Data Headers: Page, Row, [value cols in display order], [page metadata], [confidences]
	header := []string{"Page", "Row"}
	for _, col := range colOrder {
		header = append(header, exportColName(col))
	}
	header = append(header, pageMetaHeaders...)
	includeConf := h.includeConfidence(c.Request.Context())
	if includeConf {
		for _, col := range colOrder {
			header = append(header, exportColName(col)+"_Confidence")
		}
	}
	_ = writer.Write(header)

	for _, key := range rowKeys {
		cols := rowMap[key]
		csvRow := []string{strconv.Itoa(key.Page), strconv.Itoa(key.Row)}

		// Values
		for _, col := range colOrder {
			val := ""
			if cell, exists := cols[col]; exists {
				val = cell.CurrentValue
			}
			csvRow = append(csvRow, val)
		}
		// Page metadata
		csvRow = append(csvRow, pageMetaValues(pageMeta[key.Page])...)
		// Confidences
		if includeConf {
			for _, col := range colOrder {
				conf := ""
				if cell, exists := cols[col]; exists {
					conf = strconv.FormatFloat(cell.Confidence, 'f', 4, 64)
				}
				csvRow = append(csvRow, conf)
			}
		}
		_ = writer.Write(csvRow)
	}

	userIDVal, exists := c.Get("user_id")
	var userID *uuid.UUID
	if exists {
		uid := userIDVal.(uuid.UUID)
		userID = &uid
	}

	audit := &domain.AuditLog{
		TenantID:   doc.TenantID,
		UserID:     userID,
		EntityType: "document",
		EntityID:   doc.ID,
		Action:     "exported",
		OldValue:   nil,
		NewValue:   map[string]interface{}{"format": "XLS", "cells_count": len(cells)},
	}
	_ = h.auditRepo.Log(c.Request.Context(), audit)
}
