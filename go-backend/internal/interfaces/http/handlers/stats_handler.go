package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"university-result-processing/backend/internal/application/stats"
)

type StatsHandler struct {
	svc *stats.StatsService
}

func NewStatsHandler(svc *stats.StatsService) *StatsHandler {
	return &StatsHandler{svc: svc}
}

func (h *StatsHandler) Overview(c *gin.Context) {
	o, err := h.svc.Overview(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, o)
}

func (h *StatsHandler) Presence(c *gin.Context) {
	rows, err := h.svc.Presence(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

func (h *StatsHandler) Activity(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	rows, err := h.svc.RecentActivity(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// parseRange reads ?from=YYYY-MM-DD&to=YYYY-MM-DD, defaulting to the last 30
// days. `to` is treated inclusively (extended to end-of-day) for range queries.
func parseRange(c *gin.Context) (time.Time, time.Time) {
	loc := time.Now().Location()
	to := time.Now()
	from := to.AddDate(0, 0, -29)
	if v := c.Query("from"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
			from = t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
			to = t
		}
	}
	// Normalize: from at start-of-day, to at end-of-day.
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	to = time.Date(to.Year(), to.Month(), to.Day(), 23, 59, 59, 0, loc)
	return from, to
}

func (h *StatsHandler) Productivity(c *gin.Context) {
	from, to := parseRange(c)
	rows, err := h.svc.Productivity(c.Request.Context(), from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

func (h *StatsHandler) Timeseries(c *gin.Context) {
	from, to := parseRange(c)
	rows, err := h.svc.Timeseries(c.Request.Context(), from, to)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}
