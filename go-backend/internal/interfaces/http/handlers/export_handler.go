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

	// 3. Stream CSV to browser
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=export_%s_%d.csv", doc.Name, time.Now().Unix()))
	c.Header("Content-Type", "text/csv")

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write CSV Header
	header := []string{"Page", "Row"}
	for col := 0; col <= maxCol; col++ {
		colName := fmt.Sprintf("Col_%d", col)
		if col == 0 {
			colName = "Subject Code"
		} else if col == 1 {
			colName = "Batch"
		} else if col == 2 {
			colName = "Examiner Name"
		} else if col == 3 {
			colName = "Mobile Number"
		}
		header = append(header, colName)
	}
	includeConf := h.includeConfidence(c.Request.Context())
	if includeConf {
		for col := 0; col <= maxCol; col++ {
			colName := fmt.Sprintf("Col_%d", col)
			if col == 0 {
				colName = "Subject Code"
			} else if col == 1 {
				colName = "Batch"
			} else if col == 2 {
				colName = "Examiner Name"
			} else if col == 3 {
				colName = "Mobile Number"
			}
			header = append(header, colName+"_Confidence")
		}
	}
	_ = writer.Write(header)

	// Write rows
	for _, key := range rowKeys {
		cols := rowMap[key]
		csvRow := []string{strconv.Itoa(key.Page), strconv.Itoa(key.Row)}
		
		// Values
		for col := 0; col <= maxCol; col++ {
			val := ""
			if cell, exists := cols[col]; exists {
				val = cell.CurrentValue
			}
			csvRow = append(csvRow, val)
		}
		// Confidences
		if includeConf {
			for col := 0; col <= maxCol; col++ {
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

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=export_%s_%d.xls", doc.Name, time.Now().Unix()))
	c.Header("Content-Type", "application/vnd.ms-excel")

	writer := csv.NewWriter(c.Writer)
	writer.Comma = '\t' // Excel Tab-delimited
	defer writer.Flush()

	// Metadata headers
	_ = writer.Write([]string{"# METADATA SHEET"})
	_ = writer.Write([]string{"Document Name", doc.Name})
	_ = writer.Write([]string{"Upload Date", doc.CreatedAt.Format(time.RFC3339)})
	_ = writer.Write([]string{"Review Status", doc.Status})
	_ = writer.Write([]string{"Export Timestamp", time.Now().Format(time.RFC3339)})
	_ = writer.Write([]string{""}) // Empty row separator

	// Data Headers
	header := []string{"Page", "Row"}
	for col := 0; col <= maxCol; col++ {
		colName := fmt.Sprintf("Col_%d", col)
		if col == 0 {
			colName = "Subject Code"
		} else if col == 1 {
			colName = "Batch"
		} else if col == 2 {
			colName = "Examiner Name"
		} else if col == 3 {
			colName = "Mobile Number"
		}
		header = append(header, colName)
	}
	includeConf := h.includeConfidence(c.Request.Context())
	if includeConf {
		for col := 0; col <= maxCol; col++ {
			colName := fmt.Sprintf("Col_%d", col)
			if col == 0 {
				colName = "Subject Code"
			} else if col == 1 {
				colName = "Batch"
			} else if col == 2 {
				colName = "Examiner Name"
			} else if col == 3 {
				colName = "Mobile Number"
			}
			header = append(header, colName+"_Confidence")
		}
	}
	_ = writer.Write(header)

	for _, key := range rowKeys {
		cols := rowMap[key]
		csvRow := []string{strconv.Itoa(key.Page), strconv.Itoa(key.Row)}
		
		// Values
		for col := 0; col <= maxCol; col++ {
			val := ""
			if cell, exists := cols[col]; exists {
				val = cell.CurrentValue
			}
			csvRow = append(csvRow, val)
		}
		// Confidences
		if includeConf {
			for col := 0; col <= maxCol; col++ {
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
