package engine

import (
	"context"
	"sync"
	"time"

	"github.com/AeonCoreX-Lab/VoidSend/core/mailer"
	"github.com/AeonCoreX-Lab/VoidSend/monitoring"
)

type Worker struct {
	ID         int
	queue      <-chan *DispatchJob
	results    chan<- *DispatchResult
	config     *Config
	processed  int64
	failed     int64
	lastActive time.Time
}

func NewWorker(id int, queue <-chan *DispatchJob, results chan<- *DispatchResult, config *Config) *Worker {
	return &Worker{
		ID:         id,
		queue:      queue,
		results:    results,
		config:     config,
		lastActive: time.Now(),
	}
}

func (w *Worker) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	monitoring.Debug("Worker %d started", w.ID)
	for {
		select {
		case <-ctx.Done():
			monitoring.Debug("Worker %d stopping", w.ID)
			return
		case job, ok := <-w.queue:
			if !ok {
				return
			}
			w.process(job)
		}
	}
}

func (w *Worker) process(job *DispatchJob) {
	start := time.Now()
	w.lastActive = start
	w.processed++

	monitoring.Debug("Worker %d processing job %s", w.ID, job.ID)

	// job-এ সব তথ্য ready থাকে (toEmail, subject, htmlBody)
	to := job.ToEmail
	subject := job.Subject
	htmlBody := job.HTMLBody
	textBody := job.TextBody
	if textBody == "" {
		textBody = "Please enable HTML to view this email." // অথবা HTML থেকে generate করা যেতে পারে
	}

	// provider নির্বাচন
	provider := job.Provider
	if provider == "" {
		provider = mailer.SelectOptimalProvider(to)
	}

	var messageID string
	var sendErr error

	switch provider {
	case "smtp":
		messageID, sendErr = mailer.SendViaSMTP(to, subject, htmlBody, textBody)
	case "sendgrid":
		messageID, sendErr = mailer.SendViaSendGrid(to, subject, htmlBody, textBody)
	case "ses":
		messageID, sendErr = mailer.SendViaSES(to, subject, htmlBody, textBody)
	default:
		messageID, sendErr = mailer.SendViaFailover(to, subject, htmlBody, textBody)
	}

	if sendErr != nil {
		w.results <- &DispatchResult{
			JobID:    job.ID,
			Success:  false,
			Error:    sendErr,
			Provider: provider,
			Duration: time.Since(start),
			Attempts: job.RetryCount + 1,
		}
		w.failed++
		return
	}

	w.results <- &DispatchResult{
		JobID:     job.ID,
		Success:   true,
		Provider:  provider,
		MessageID: messageID,
		Duration:  time.Since(start),
		Attempts:  job.RetryCount + 1,
		Timestamp: time.Now(),
	}

	monitoring.Debug("Worker %d completed job %s in %v", w.ID, job.ID, time.Since(start))
}

func (w *Worker) Stats() map[string]interface{} {
	return map[string]interface{}{
		"id":           w.ID,
		"processed":    w.processed,
		"failed":       w.failed,
		"success_rate": float64(w.processed-w.failed) / float64(w.processed) * 100,
		"last_active":  w.lastActive,
	}
}