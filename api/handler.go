package api

import (
	"net/http"
	"time"

	"github.com/VoidSend/core/database"
	"github.com/VoidSend/core/engine"
	"github.com/VoidSend/core/template"
	"github.com/VoidSend/monitoring"
	"github.com/gin-gonic/gin"
)

// EngineInstance – global reference to the engine (set by main)
var EngineInstance *engine.UltraEngine

// DispatchRequest – সাধারণ ডিসপ্যাচ রিকোয়েস্ট
type DispatchRequest struct {
	UserID      string                 `json:"user_id" binding:"required"`
	Action      string                 `json:"action" binding:"required"`
	Data        map[string]interface{} `json:"data"`
	Priority    int                    `json:"priority"`
	ScheduledAt *time.Time             `json:"scheduled_at"`
	Provider    string                 `json:"provider"`
	Tags        []string               `json:"tags"`
}

// HandleUltraDispatch – একক ইমেইল ডিসপ্যাচ
func HandleUltraDispatch(c *gin.Context) {
	var req DispatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	devID, _ := c.Get("developer_id")
	devIDStr, _ := devID.(string)

	userEmail, err := database.GetUserEmail(c.Request.Context(), req.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	subject, htmlBody, err := template.ProcessTemplate(devIDStr, req.Action, req.Data)
	if err != nil {
		subject, htmlBody, err = template.ProcessTemplateFile(req.Action, req.Data)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Template error: " + err.Error()})
			return
		}
	}

	job := &engine.DispatchJob{
		UserID:      req.UserID,
		ToEmail:     userEmail,
		Action:      req.Action,
		Subject:     subject,
		HTMLBody:    htmlBody,
		TextBody:    "",
		Data:        req.Data,
		Priority:    req.Priority,
		ScheduledAt: req.ScheduledAt,
		Provider:    req.Provider,
		Tags:        req.Tags,
	}

	if err := EngineInstance.Dispatch(job); err != nil {
		monitoring.Error("Dispatch failed: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":    "queued",
		"job_id":    job.ID,
		"message":   "Email will be dispatched shortly",
		"timestamp": time.Now().Unix(),
	})
}

// BatchDispatchRequest – ব্যাচ রিকোয়েস্ট
type BatchDispatchRequest struct {
	Jobs []DispatchRequest `json:"jobs" binding:"required,min=1,max=100"`
}

func HandleBatchDispatch(c *gin.Context) {
	var req BatchDispatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	devID, _ := c.Get("developer_id")
	devIDStr, _ := devID.(string)

	jobs := make([]*engine.DispatchJob, 0, len(req.Jobs))
	results := make([]map[string]interface{}, len(req.Jobs))

	for i, j := range req.Jobs {
		userEmail, err := database.GetUserEmail(c.Request.Context(), j.UserID)
		if err != nil {
			results[i] = map[string]interface{}{
				"index":  i,
				"status": "failed",
				"error":  "User not found",
			}
			continue
		}

		subject, htmlBody, err := template.ProcessTemplate(devIDStr, j.Action, j.Data)
		if err != nil {
			subject, htmlBody, err = template.ProcessTemplateFile(j.Action, j.Data)
			if err != nil {
				results[i] = map[string]interface{}{
					"index":  i,
					"status": "failed",
					"error":  "Template error: " + err.Error(),
				}
				continue
			}
		}

		job := &engine.DispatchJob{
			UserID:      j.UserID,
			ToEmail:     userEmail,
			Action:      j.Action,
			Subject:     subject,
			HTMLBody:    htmlBody,
			Data:        j.Data,
			Priority:    j.Priority,
			ScheduledAt: j.ScheduledAt,
			Provider:    j.Provider,
			Tags:        j.Tags,
		}
		jobs = append(jobs, job)
		results[i] = map[string]interface{}{
			"index":  i,
			"job_id": job.ID,
			"status": "queued",
		}
	}

	errs := EngineInstance.BatchDispatch(jobs)
	for i, err := range errs {
		if err != nil {
			results[i]["status"] = "failed"
			results[i]["error"] = err.Error()
		}
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":    "batch_queued",
		"total":     len(jobs),
		"results":   results,
		"timestamp": time.Now().Unix(),
	})
}

// ScheduleRequest – scheduled email
type ScheduleRequest struct {
	UserID     string                 `json:"user_id" binding:"required"`
	Action     string                 `json:"action" binding:"required"`
	Data       map[string]interface{} `json:"data"`
	ScheduleAt time.Time              `json:"schedule_at" binding:"required"`
	Repeat     string                 `json:"repeat"` // daily, weekly, monthly
	EndAt      *time.Time             `json:"end_at"`
}

func HandleScheduledDispatch(c *gin.Context) {
	var req ScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ScheduleAt.Before(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Schedule time must be in future"})
		return
	}

	devID, _ := c.Get("developer_id")
	devIDStr, _ := devID.(string)

	userEmail, err := database.GetUserEmail(c.Request.Context(), req.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	subject, htmlBody, err := template.ProcessTemplate(devIDStr, req.Action, req.Data)
	if err != nil {
		subject, htmlBody, err = template.ProcessTemplateFile(req.Action, req.Data)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Template error: " + err.Error()})
			return
		}
	}

	job := &engine.DispatchJob{
		UserID:      req.UserID,
		ToEmail:     userEmail,
		Action:      req.Action,
		Subject:     subject,
		HTMLBody:    htmlBody,
		Data:        req.Data,
		ScheduledAt: &req.ScheduleAt,
	}

	if err := EngineInstance.Dispatch(job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.Repeat != "" {
		// TODO: recurring schedule সংরক্ষণ করুন
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":       "scheduled",
		"job_id":       job.ID,
		"scheduled_at": req.ScheduleAt,
		"message":      "Email scheduled successfully",
	})
}

// Webhook handlers
func HandleStatusWebhook(c *gin.Context) {
	var payload struct {
		Event     string `json:"event"`
		Email     string `json:"email"`
		MessageID string `json:"message_id"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	monitoring.Info("Webhook received: %s for %s", payload.Event, payload.MessageID)
	c.JSON(http.StatusOK, gin.H{"status": "received"})
}

func HandleBounceWebhook(c *gin.Context) {
	var payload struct {
		BounceType string `json:"bounce_type"`
		Email      string `json:"email"`
		MessageID  string `json:"message_id"`
		Reason     string `json:"reason"`
		Timestamp  int64  `json:"timestamp"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	monitoring.Warn("Bounce received: %s - %s", payload.Email, payload.Reason)
	c.JSON(http.StatusOK, gin.H{"status": "processed"})
}