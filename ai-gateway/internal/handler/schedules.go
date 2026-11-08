package handler

import (
	"net/http"

	"github.com/conduit/ai-gateway/internal/store"
	"github.com/conduit/pkg/log"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ScheduleHandler exposes admin visibility into ADR 0007 schedules.
type ScheduleHandler struct {
	store  *store.ScheduleStore
	logger *log.Logger
}

// NewScheduleHandler creates a new schedule handler.
func NewScheduleHandler(s *store.ScheduleStore, logger *log.Logger) *ScheduleHandler {
	return &ScheduleHandler{store: s, logger: logger.WithHandler("schedule_handler")}
}

// ListSchedules handles GET /schedules
func (h *ScheduleHandler) ListSchedules(c *gin.Context) {
	ctx := c.Request.Context()
	schedules, err := h.store.ListSchedules(ctx)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list schedules")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedules": schedules})
}

// PatchScheduleRequest is the body for PATCH /schedules/:id (pause/resume).
type PatchScheduleRequest struct {
	Enabled *bool `json:"enabled"`
}

// PatchSchedule handles PATCH /schedules/:id
func (h *ScheduleHandler) PatchSchedule(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule id"})
		return
	}

	var req PatchScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body must include {\"enabled\": true|false}"})
		return
	}

	if err := h.store.SetScheduleEnabled(ctx, id, *req.Enabled); err != nil {
		h.logger.Error().Err(err).Str("schedule_id", id.String()).Msg("failed to patch schedule")
		c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "enabled": *req.Enabled})
}
