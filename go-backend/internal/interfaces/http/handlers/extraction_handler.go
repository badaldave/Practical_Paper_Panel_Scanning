package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"university-result-processing/backend/internal/application/extraction"
)

type ExtractionHandler struct {
	extService *extraction.ExtractionService
}

func NewExtractionHandler(es *extraction.ExtractionService) *ExtractionHandler {
	return &ExtractionHandler{extService: es}
}

func (h *ExtractionHandler) GetActiveCells(c *gin.Context) {
	docIDStr := c.Param("id")
	docID, err := uuid.Parse(docIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID format"})
		return
	}

	cells, err := h.extService.GetActiveCells(c.Request.Context(), docID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch cells: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, cells)
}

func (h *ExtractionHandler) UpdateCell(c *gin.Context) {
	docIDStr := c.Param("id")
	docID, err := uuid.Parse(docIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID format"})
		return
	}

	var req extraction.UpdateCellRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	req.DocumentID = docID

	// Extract updater user ID from JWT context
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	userID := userIDVal.(uuid.UUID)

	err = h.extService.UpdateCell(c.Request.Context(), req, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Cell updated successfully"})
}

func (h *ExtractionHandler) GetCellHistory(c *gin.Context) {
	docIDStr := c.Param("id")
	docID, err := uuid.Parse(docIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID format"})
		return
	}

	rowStr := c.Param("row")
	colStr := c.Param("col")

	rowIdx, err := strconv.Atoi(rowStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid row index"})
		return
	}

	colIdx, err := strconv.Atoi(colStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid column index"})
		return
	}

	pageNumStr := c.DefaultQuery("page", "1")
	pageNum, err := strconv.Atoi(pageNumStr)
	if err != nil || pageNum <= 0 {
		pageNum = 1
	}

	history, err := h.extService.GetHistory(c.Request.Context(), docID, pageNum, rowIdx, colIdx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch cell history: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, history)
}
