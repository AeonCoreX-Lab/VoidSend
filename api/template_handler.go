package api

import (
	"net/http"
	"time"

	"github.com/VoidSend/core/template"
	"github.com/VoidSend/monitoring"
	"github.com/gin-gonic/gin"
)

// TemplateUploadRequest – টেমপ্লেট আপলোড রিকোয়েস্ট
type TemplateUploadRequest struct {
	Action      string `json:"action" binding:"required"`
	HTMLContent string `json:"html_content" binding:"required"`
	Subject     string `json:"subject"`
}

// HandleTemplateUpload – টেমপ্লেট আপলোড
func HandleTemplateUpload(c *gin.Context) {
	var req TemplateUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// ডেভেলপার আইডি (API key থেকে)
	devID, exists := c.Get("developer_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Developer not identified"})
		return
	}
	devIDStr := devID.(string)

	// টেমপ্লেট ম্যানেজার দিয়ে সংরক্ষণ
	tm := template.GetManager()
	err := tm.SaveDeveloperTemplate(devIDStr, req.Action, req.HTMLContent, req.Subject)
	if err != nil {
		monitoring.Error("Template upload failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Template uploaded successfully",
		"action":  req.Action,
	})
}

// HandleGetTemplate – নির্দিষ্ট টেমপ্লেট দেখায়
func HandleGetTemplate(c *gin.Context) {
	action := c.Param("action")
	devID, _ := c.Get("developer_id")
	devIDStr, _ := devID.(string)

	tm := template.GetManager()
	// টেমপ্লেট রেন্ডার করে দেখা (ডাটা ছাড়া)
	_, htmlContent, err := tm.RenderTemplate(devIDStr, action, nil)
	if err != nil {
		// ফাইল টেমপ্লেট试试 করুন
		_, htmlContent, err = template.ProcessTemplateFile(action, nil)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
			return
		}
	}

	// সাবজেক্ট বের করা (যদি ডাটাবেজে থাকে)
	subject := template.GetDefaultSubject(action) // ডিফল্ট

	c.JSON(http.StatusOK, gin.H{
		"action":       action,
		"html_content": htmlContent,
		"subject":      subject,
	})
}

// HandleListTemplates – ডেভেলপারের সব টেমপ্লেটের তালিকা
func HandleListTemplates(c *gin.Context) {
	devID, _ := c.Get("developer_id")
	devIDStr, _ := devID.(string)

	tm := template.GetManager()
	templates, err := tm.ListDeveloperTemplates(devIDStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"templates": templates,
		"count":     len(templates),
	})
}

// HandleDeleteTemplate – টেমপ্লেট ডিলিট
func HandleDeleteTemplate(c *gin.Context) {
	action := c.Param("action")
	devID, _ := c.Get("developer_id")
	devIDStr, _ := devID.(string)

	tm := template.GetManager()
	err := tm.DeleteDeveloperTemplate(devIDStr, action)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Template deleted",
	})
}