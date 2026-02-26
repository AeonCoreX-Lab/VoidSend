package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/VoidSend/core/database"
	"github.com/VoidSend/core/mailer"
	"github.com/VoidSend/monitoring"
	"github.com/google/uuid"
)

// DispatchJob – ইঞ্জিনে পাঠানো job
type DispatchJob struct {
	ID          string                 `json:"id"`
	UserID      string                 `json:"user_id"`
	ToEmail     string                 `json:"to_email"`               // ✓ নতুন
	Action      string                 `json:"action"`
	Subject     string                 `json:"subject"`                // ✓ নতুন
	HTMLBody    string                 `json:"html_body"`              // ✓ নতুন
	TextBody    string                 `json:"text_body"`              // ✓ নতুন (optional)
	Data        map[string]interface{} `json:"data"`
	Priority    int                    `json:"priority"`
	ScheduledAt *time.Time             `json:"scheduled_at"`
	RetryCount  int                    `json:"retry_count"`
	MaxRetries  int                    `json:"max_retries"`
	Provider    string                  `json:"provider"`
	Tags        []string                `json:"tags"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

// DispatchResult – job-এর ফলাফল
type DispatchResult struct {
	JobID       string
	Success     bool
	Provider    string
	MessageID   string
	Error       error
	Duration    time.Duration
	Attempts    int
	Timestamp   time.Time
}

// UltraEngine – মূল ইঞ্জিন স্ট্রাকচার
type UltraEngine struct {
	config      *Config
	workers     []*Worker
	queue       chan *DispatchJob
	results     chan *DispatchResult
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	activeJobs  map[string]*DispatchJob
	metrics     *EngineMetrics
}

type Config struct {
	MaxWorkers    int
	QueueSize     int
	RetryAttempts int
	RetryDelay    time.Duration
	BatchSize     int
	MaxConcurrent int
}

type EngineMetrics struct {
	TotalJobs      int64
	SuccessJobs    int64
	FailedJobs     int64
	AvgProcessTime time.Duration
	QueueLength    int
	ActiveWorkers  int
	mu             sync.RWMutex
}

// NewUltraEngine – নতুন ইঞ্জিন তৈরি
func NewUltraEngine(config *Config) *UltraEngine {
	if config == nil {
		config = &Config{
			MaxWorkers:    100,
			QueueSize:     10000,
			RetryAttempts: 3,
			RetryDelay:    time.Second * 5,
			BatchSize:     50,
			MaxConcurrent: 1000,
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &UltraEngine{
		config:     config,
		queue:      make(chan *DispatchJob, config.QueueSize),
		results:    make(chan *DispatchResult, config.QueueSize),
		ctx:        ctx,
		cancel:     cancel,
		activeJobs: make(map[string]*DispatchJob),
		metrics:    &EngineMetrics{},
	}
}

// Start – ইঞ্জিন চালু
func (e *UltraEngine) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := 0; i < e.config.MaxWorkers; i++ {
		worker := NewWorker(i, e.queue, e.results, e.config)
		e.workers = append(e.workers, worker)
		e.wg.Add(1)
		go worker.Start(e.ctx, &e.wg)
	}
	go e.processResults()
	go e.collectMetrics()
	monitoring.Info("UltraEngine started with %d workers", e.config.MaxWorkers)
}

// Stop – ইঞ্জিন বন্ধ
func (e *UltraEngine) Stop() {
	e.cancel()
	e.wg.Wait()
	close(e.queue)
	close(e.results)
	monitoring.Info("UltraEngine stopped")
}

// Dispatch – একটি job পাঠায়
func (e *UltraEngine) Dispatch(job *DispatchJob) error {
	if job.ID == "" {
		job.ID = uuid.New().String()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	if job.MaxRetries == 0 {
		job.MaxRetries = e.config.RetryAttempts
	}
	// Rate limit check
	if !e.checkRateLimit(job.UserID) {
		return errors.New("rate limit exceeded")
	}
	// Active jobs-এ রাখি
	e.mu.Lock()
	e.activeJobs[job.ID] = job
	e.mu.Unlock()

	// Schedule যদি ভবিষ্যতে হয়
	if job.ScheduledAt != nil && job.ScheduledAt.After(time.Now()) {
		return e.scheduleJob(job)
	}

	// Queue-তে পাঠাই
	select {
	case e.queue <- job:
		e.metrics.mu.Lock()
		e.metrics.TotalJobs++
		e.metrics.mu.Unlock()
		monitoring.Debug("Job %s dispatched to queue", job.ID)
		return nil
	default:
		e.mu.Lock()
		delete(e.activeJobs, job.ID)
		e.mu.Unlock()
		return errors.New("queue is full")
	}
}

// BatchDispatch – একাধিক job পাঠায়
func (e *UltraEngine) BatchDispatch(jobs []*DispatchJob) []error {
	errs := make([]error, len(jobs))
	for i, job := range jobs {
		if err := e.Dispatch(job); err != nil {
			errs[i] = err
		}
	}
	return errs
}

// processResults – ফলাফল প্রসেস করে
func (e *UltraEngine) processResults() {
	for result := range e.results {
		e.mu.Lock()
		job, exists := e.activeJobs[result.JobID]
		e.mu.Unlock()
		if !exists {
			continue
		}
		if result.Success {
			e.metrics.mu.Lock()
			e.metrics.SuccessJobs++
			e.metrics.mu.Unlock()
			e.updateJobStatus(job, "completed", result)
			e.sendStatusWebhook(job, result)
			monitoring.Info("Job %s completed via %s in %v", job.ID, result.Provider, result.Duration)
		} else {
			if job.RetryCount < job.MaxRetries {
				job.RetryCount++
				backoff := time.Duration(job.RetryCount) * e.config.RetryDelay
				time.AfterFunc(backoff, func() {
					monitoring.Warn("Retrying job %s (attempt %d/%d)", job.ID, job.RetryCount, job.MaxRetries)
					e.queue <- job
				})
			} else {
				e.metrics.mu.Lock()
				e.metrics.FailedJobs++
				e.metrics.mu.Unlock()
				e.updateJobStatus(job, "failed", result)
				e.sendFailureAlert(job, result)
				monitoring.Error("Job %s failed after %d attempts: %v", job.ID, job.RetryCount, result.Error)
			}
		}
		e.mu.Lock()
		delete(e.activeJobs, result.JobID)
		e.mu.Unlock()
	}
}

// scheduleJob – schedule সংরক্ষণ
func (e *UltraEngine) scheduleJob(job *DispatchJob) error {
	data, _ := json.Marshal(job)
	err := database.RedisClient.Set(e.ctx, "scheduled:"+job.ID, data, time.Until(*job.ScheduledAt)).Err()
	if err != nil {
		return err
	}
	_, err = database.PostgresPool.Exec(e.ctx,
		`INSERT INTO scheduled_jobs (id, user_id, action, to_email, subject, html_body, data, scheduled_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		job.ID, job.UserID, job.Action, job.ToEmail, job.Subject, job.HTMLBody, job.Data, job.ScheduledAt, time.Now())
	return err
}

// checkRateLimit – rate limit চেক
func (e *UltraEngine) checkRateLimit(userID string) bool {
	key := "rate:" + userID + ":" + time.Now().Format("2006-01-02-15")
	count, err := database.RedisClient.Incr(e.ctx, key).Result()
	if err != nil {
		return true
	}
	if count == 1 {
		database.RedisClient.Expire(e.ctx, key, time.Hour)
	}
	return count <= int64(e.config.MaxConcurrent)
}

// updateJobStatus – ইতিহাসে সংরক্ষণ
func (e *UltraEngine) updateJobStatus(job *DispatchJob, status string, result *DispatchResult) {
	_, err := database.PostgresPool.Exec(e.ctx,
		`INSERT INTO job_history (job_id, user_id, action, to_email, status, provider, message_id, error, duration, attempts, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		job.ID, job.UserID, job.Action, job.ToEmail, status, result.Provider,
		result.MessageID, result.Error, result.Duration, result.Attempts, time.Now())
	if err != nil {
		monitoring.Error("Failed to update job status: %v", err)
	}
}

// sendStatusWebhook – webhook পাঠায়
func (e *UltraEngine) sendStatusWebhook(job *DispatchJob, result *DispatchResult) {
	go func() {
		webhookURL, err := e.getWebhookURL(job.UserID)
		if err != nil || webhookURL == "" {
			return
		}
		payload := map[string]interface{}{
			"job_id":     job.ID,
			"user_id":    job.UserID,
			"action":     job.Action,
			"status":     "completed",
			"provider":   result.Provider,
			"message_id": result.MessageID,
			"timestamp":  time.Now(),
		}
		mailer.SendWebhook(webhookURL, payload)
	}()
}

// sendFailureAlert – অ্যালার্ট
func (e *UltraEngine) sendFailureAlert(job *DispatchJob, result *DispatchResult) {
	monitoring.Alert("Job failed permanently", map[string]interface{}{
		"job_id":  job.ID,
		"user_id": job.UserID,
		"error":   result.Error.Error(),
	})
}

// collectMetrics – মেট্রিক্স সংগ্রহ
func (e *UltraEngine) collectMetrics() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.metrics.mu.Lock()
			e.metrics.QueueLength = len(e.queue)
			e.metrics.ActiveWorkers = len(e.workers)
			e.metrics.mu.Unlock()
			monitoring.RecordMetrics(e.metrics)
		}
	}
}

// getWebhookURL – ডাটাবেজ থেকে webhook URL আনে (ডামি)
func (e *UltraEngine) getWebhookURL(userID string) (string, error) {
	// এখানে 실제 DB কোয়েরি হবে
	return "", nil
}