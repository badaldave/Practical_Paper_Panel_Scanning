package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"university-result-processing/backend/internal/application/verification"
)

type VerificationHandler struct {
	svc *verification.VerificationService
}

func NewVerificationHandler(svc *verification.VerificationService) *VerificationHandler {
	return &VerificationHandler{svc: svc}
}

func (h *VerificationHandler) docID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return uuid.Nil, false
	}
	return id, true
}

// Queue lists the verification pool. ?scope=available|mine|all|submitted
func (h *VerificationHandler) Queue(c *gin.Context) {
	scope := c.DefaultQuery("scope", "available")
	items, err := h.svc.Queue(c.Request.Context(), scope)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *VerificationHandler) State(c *gin.Context) {
	id, ok := h.docID(c)
	if !ok {
		return
	}
	state, err := h.svc.State(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if state == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Document not found"})
		return
	}
	c.JSON(http.StatusOK, state)
}

func (h *VerificationHandler) Claim(c *gin.Context) {
	id, ok := h.docID(c)
	if !ok {
		return
	}
	state, err := h.svc.Claim(c.Request.Context(), id)
	if err != nil {
		// Already locked / not available — 409 so the UI can refresh the pool.
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, state)
}

func (h *VerificationHandler) Release(c *gin.Context) {
	id, ok := h.docID(c)
	if !ok {
		return
	}
	if err := h.svc.Release(c.Request.Context(), id, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Released"})
}

// ForceRelease is an admin override that clears another user's lock.
func (h *VerificationHandler) ForceRelease(c *gin.Context) {
	id, ok := h.docID(c)
	if !ok {
		return
	}
	if err := h.svc.Release(c.Request.Context(), id, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Lock force-released"})
}

func (h *VerificationHandler) Assign(c *gin.Context) {
	id, ok := h.docID(c)
	if !ok {
		return
	}
	var req struct {
		AssigneeID *string `json:"assignee_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var assignee *uuid.UUID
	if req.AssigneeID != nil && *req.AssigneeID != "" {
		parsed, err := uuid.Parse(*req.AssigneeID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid assignee ID"})
			return
		}
		assignee = &parsed
	}
	if err := h.svc.Assign(c.Request.Context(), id, assignee); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Assignment updated"})
}

func (h *VerificationHandler) Presence(c *gin.Context) {
	id, ok := h.docID(c)
	if !ok {
		return
	}
	var req struct {
		Page int `json:"page"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.SetPresence(c.Request.Context(), id, req.Page); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *VerificationHandler) MarkPage(c *gin.Context) {
	id, ok := h.docID(c)
	if !ok {
		return
	}
	page, err := strconv.Atoi(c.Param("page"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page number"})
		return
	}
	var req struct {
		Verified bool `json:"verified"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.MarkPage(c.Request.Context(), id, page, req.Verified); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Page updated"})
}

func (h *VerificationHandler) Submit(c *gin.Context) {
	id, ok := h.docID(c)
	if !ok {
		return
	}
	if err := h.svc.Submit(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Document submitted as verified"})
}
