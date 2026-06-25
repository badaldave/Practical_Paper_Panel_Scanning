package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"university-result-processing/backend/internal/application/document"
	"university-result-processing/backend/internal/domain"
)

type DocumentHandler struct {
	docService *document.DocumentService
}

func NewDocumentHandler(ds *document.DocumentService) *DocumentHandler {
	return &DocumentHandler{docService: ds}
}

func (h *DocumentHandler) Upload(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	fileReader, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open uploaded file"})
		return
	}
	defer fileReader.Close()

	req := document.UploadDocumentRequest{
		Name:     fileHeader.Filename,
		MimeType: fileHeader.Header.Get("Content-Type"),
		Size:     fileHeader.Size,
		File:     fileReader,
	}

	doc, err := h.docService.Upload(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, doc)
}

func (h *DocumentHandler) List(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 10
	}
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	docs, err := h.docService.GetList(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch documents: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, docs)
}

func (h *DocumentHandler) GetByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID format"})
		return
	}

	doc, pages, err := h.docService.GetDetails(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if doc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Document not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"document": doc,
		"pages":    pages,
	})
}

func (h *DocumentHandler) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID format"})
		return
	}

	err = h.docService.Delete(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Document deleted successfully"})
}

func (h *DocumentHandler) DeleteAll(c *gin.Context) {
	err := h.docService.DeleteAll(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "All documents deleted successfully"})
}

func (h *DocumentHandler) UpdatePageMetadata(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID format"})
		return
	}

	pageNumStr := c.Param("page_number")
	pageNum, err := strconv.Atoi(pageNumStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page number format"})
		return
	}

	var req struct {
		CollegeCode     *string `json:"college_code"`
		CollegeName     *string `json:"college_name"`
		SubjectCode     *string `json:"subject_code"`
		SubjectName     *string `json:"subject_name"`
		Faculty         *string `json:"faculty"`
		TotalCandidates *int    `json:"total_candidates"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	page := &domain.DocumentPage{
		CollegeCode:     req.CollegeCode,
		CollegeName:     req.CollegeName,
		SubjectCode:     req.SubjectCode,
		SubjectName:     req.SubjectName,
		Faculty:         req.Faculty,
		TotalCandidates: req.TotalCandidates,
	}

	err = h.docService.UpdatePageMetadata(c.Request.Context(), id, pageNum, page)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Page metadata updated successfully"})
}
