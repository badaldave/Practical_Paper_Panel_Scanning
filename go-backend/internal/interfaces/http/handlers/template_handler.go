package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"university-result-processing/backend/internal/domain"
)

type TemplateHandler struct {
	tmplRepo domain.TemplateRepository
}

func NewTemplateHandler(tr domain.TemplateRepository) *TemplateHandler {
	return &TemplateHandler{tmplRepo: tr}
}

func (h *TemplateHandler) Create(c *gin.Context) {
	var req domain.Template
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Extract tenant ID from Gin context (set by AuthMiddleware)
	tenantIDVal, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	req.TenantID = tenantIDVal.(uuid.UUID)

	if err := h.tmplRepo.Create(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create template: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, req)
}

func (h *TemplateHandler) List(c *gin.Context) {
	tenantIDVal, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	tenantID := tenantIDVal.(uuid.UUID)

	templates, err := h.tmplRepo.GetByTenant(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list templates: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, templates)
}

func (h *TemplateHandler) CreateVersion(c *gin.Context) {
	tmplIDStr := c.Param("id")
	tmplID, err := uuid.Parse(tmplIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID"})
		return
	}

	var req domain.TemplateVersion
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.TemplateID = tmplID

	if err := h.tmplRepo.CreateVersion(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to version template: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, req)
}

func (h *TemplateHandler) CreateField(c *gin.Context) {
	versionIDStr := c.Param("version_id")
	versionID, err := uuid.Parse(versionIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template version ID"})
		return
	}

	var req domain.TemplateField
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.TemplateVersionID = versionID

	if err := h.tmplRepo.CreateField(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add template field: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, req)
}

func (h *TemplateHandler) GetFields(c *gin.Context) {
	versionIDStr := c.Param("version_id")
	versionID, err := uuid.Parse(versionIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template version ID"})
		return
	}

	fields, err := h.tmplRepo.GetFields(c.Request.Context(), versionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch fields: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, fields)
}
