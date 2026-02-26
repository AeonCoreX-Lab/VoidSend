package monitoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Alert struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Severity  string                 `json:"severity"` // critical, warning, info
	Tags      []string               `json:"tags"`
	Data      map[string]interface{} `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
}

type AlertConfig struct {
	SlackWebhookURL string
	PagerDutyKey    string
	EmailAlertsTo   []string
}

var alertConfig AlertConfig

func InitAlerts(config AlertConfig) {
	alertConfig = config
}

func SendAlert(alert Alert) {
	// Log alert
	LogAlert(alert.Message, map[string]interface{}{
		"alert_id": alert.ID,
		"severity": alert.Severity,
		"title":    alert.Title,
	})

	// Send to different channels based on severity
	switch alert.Severity {
	case "critical":
		go sendPagerDutyAlert(alert)
		go sendSlackAlert(alert)
		go sendEmailAlert(alert)
	case "warning":
		go sendSlackAlert(alert)
	case "info":
		// Just log
	}
}

func sendSlackAlert(alert Alert) {
	if alertConfig.SlackWebhookURL == "" {
		return
	}

	color := map[string]string{
		"critical": "danger",
		"warning":  "warning",
		"info":     "good",
	}[alert.Severity]

	payload := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color":      color,
				"title":      alert.Title,
				"text":       alert.Message,
				"fields":     alert.Data,
				"footer":     "VoidSend Alert System",
				"ts":         alert.Timestamp.Unix(),
				"mrkdwn_in":  []string{"text"},
			},
		},
	}

	data, _ := json.Marshal(payload)
	http.Post(alertConfig.SlackWebhookURL, "application/json", bytes.NewBuffer(data))
}

func sendPagerDutyAlert(alert Alert) {
	if alertConfig.PagerDutyKey == "" {
		return
	}

	payload := map[string]interface{}{
		"routing_key":  alertConfig.PagerDutyKey,
		"event_action": "trigger",
		"payload": map[string]interface{}{
			"summary":       alert.Title,
			"source":        "VoidSend",
			"severity":      alert.Severity,
			"timestamp":     alert.Timestamp.Format(time.RFC3339),
			"component":     "email-engine",
			"group":         "production",
			"class":         "email-delivery",
			"custom_details": alert.Data,
		},
	}

	data, _ := json.Marshal(payload)
	http.Post("https://events.pagerduty.com/v2/enqueue",
		"application/json", bytes.NewBuffer(data))
}

func sendEmailAlert(alert Alert) {
	if len(alertConfig.EmailAlertsTo) == 0 {
		return
	}
	// Use your email engine to send alert
	// This would call your email dispatch system
}

// Predefined alerts
func AlertHighFailureRate(rate float64, duration time.Duration) {
	SendAlert(Alert{
		ID:       fmt.Sprintf("high-failure-%d", time.Now().Unix()),
		Title:    "High Email Failure Rate Detected",
		Message:  fmt.Sprintf("Failure rate is %.2f%% over last %v", rate*100, duration),
		Severity: "critical",
		Tags:     []string{"failure-rate", "critical"},
		Data: map[string]interface{}{
			"failure_rate": rate,
			"duration":     duration.String(),
			"threshold":    "5%",
		},
		Timestamp: time.Now(),
	})
}

func AlertQueueBacklog(queueSize int, threshold int) {
	if queueSize > threshold {
		SendAlert(Alert{
			ID:       fmt.Sprintf("queue-backlog-%d", time.Now().Unix()),
			Title:    "Email Queue Backlog Detected",
			Message:  fmt.Sprintf("Queue size is %d (threshold: %d)", queueSize, threshold),
			Severity: "warning",
			Tags:     []string{"queue", "performance"},
			Data: map[string]interface{}{
				"queue_size": queueSize,
				"threshold":  threshold,
			},
			Timestamp: time.Now(),
		})
	}
}

func AlertProviderFailure(provider string, err error) {
	SendAlert(Alert{
		ID:       fmt.Sprintf("provider-failure-%s", provider),
		Title:    fmt.Sprintf("%s Provider Failure", provider),
		Message:  fmt.Sprintf("Provider %s is failing: %v", provider, err),
		Severity: "critical",
		Tags:     []string{"provider", "failure"},
		Data: map[string]interface{}{
			"provider": provider,
			"error":    err.Error(),
		},
		Timestamp: time.Now(),
	})
}

func AlertRateLimitHit(userID string, limit int) {
	SendAlert(Alert{
		ID:       fmt.Sprintf("rate-limit-%s", userID),
		Title:    "Rate Limit Exceeded",
		Message:  fmt.Sprintf("User %s exceeded rate limit of %d", userID, limit),
		Severity: "info",
		Tags:     []string{"rate-limit", "user"},
		Data: map[string]interface{}{
			"user_id": userID,
			"limit":   limit,
		},
		Timestamp: time.Now(),
	})
}