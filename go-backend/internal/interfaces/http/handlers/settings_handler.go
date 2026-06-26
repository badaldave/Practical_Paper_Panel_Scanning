package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"university-result-processing/backend/internal/application/settings"
)

type SettingsHandler struct {
	svc *settings.SettingsService
}

func NewSettingsHandler(svc *settings.SettingsService) *SettingsHandler {
	return &SettingsHandler{svc: svc}
}

// Get returns the tenant's organization profile and effective settings.
// Readable by any authenticated member so clients (e.g. the verification grid)
// can honor tenant-wide config like the low-confidence threshold.
func (h *SettingsHandler) Get(c *gin.Context) {
	res, err := h.svc.Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *SettingsHandler) Update(c *gin.Context) {
	var req settings.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
