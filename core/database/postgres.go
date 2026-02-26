package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/VoidSend/monitoring"
)

var PostgresPool *pgxpool.Pool

type PostgresConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

func InitPostgres(config *PostgresConfig) error {
	poolConfig, err := pgxpool.ParseConfig(config.URL)
	if err != nil {
		return fmt.Errorf("failed to parse postgres URL: %v", err)
	}

	poolConfig.MaxConns = config.MaxConns
	poolConfig.MinConns = config.MinConns
	poolConfig.MaxConnLifetime = config.MaxConnLifetime
	poolConfig.MaxConnIdleTime = config.MaxConnIdleTime

	PostgresPool, err = pgxpool.ConnectConfig(context.Background(), poolConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %v", err)
	}

	// Test connection
	if err := PostgresPool.Ping(context.Background()); err != nil {
		return fmt.Errorf("failed to ping postgres: %v", err)
	}

	// Create tables if not exists
	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %v", err)
	}

	monitoring.Info("PostgreSQL connected successfully")
	return nil
}

func createTables() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id VARCHAR(255) PRIMARY KEY,
			email VARCHAR(255) NOT NULL,
			name VARCHAR(255),
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			preferences JSONB,
			status VARCHAR(50) DEFAULT 'active'
		)`,

		`CREATE TABLE IF NOT EXISTS email_jobs (
			id VARCHAR(255) PRIMARY KEY,
			user_id VARCHAR(255) NOT NULL,
			action VARCHAR(100) NOT NULL,
			status VARCHAR(50) DEFAULT 'pending',
			provider VARCHAR(50),
			message_id VARCHAR(255),
			data JSONB,
			priority INTEGER DEFAULT 5,
			retry_count INTEGER DEFAULT 0,
			max_retries INTEGER DEFAULT 3,
			scheduled_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			completed_at TIMESTAMP,
			error TEXT,
			INDEX idx_user_id (user_id),
			INDEX idx_status (status),
			INDEX idx_scheduled_at (scheduled_at)
		)`,

		`CREATE TABLE IF NOT EXISTS email_history (
			id SERIAL PRIMARY KEY,
			job_id VARCHAR(255) NOT NULL,
			user_id VARCHAR(255) NOT NULL,
			email VARCHAR(255) NOT NULL,
			action VARCHAR(100),
			status VARCHAR(50),
			provider VARCHAR(50),
			message_id VARCHAR(255),
			error TEXT,
			duration INTERVAL,
			opened_at TIMESTAMP,
			clicked_at TIMESTAMP,
			bounced_at TIMESTAMP,
			complained_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW(),
			INDEX idx_user_id (user_id),
			INDEX idx_created_at (created_at)
		)`,

		`CREATE TABLE IF NOT EXISTS templates (
			id VARCHAR(255) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			action VARCHAR(100) UNIQUE NOT NULL,
			subject TEXT NOT NULL,
			html_content TEXT,
			text_content TEXT,
			variables JSONB,
			version INTEGER DEFAULT 1,
			is_active BOOLEAN DEFAULT true,
			created_by VARCHAR(255),
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			INDEX idx_action (action)
		)`,

		`CREATE TABLE IF NOT EXISTS webhooks (
			id VARCHAR(255) PRIMARY KEY,
			user_id VARCHAR(255) NOT NULL,
			url TEXT NOT NULL,
			events TEXT[],
			secret VARCHAR(255),
			is_active BOOLEAN DEFAULT true,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			INDEX idx_user_id (user_id)
		)`,

		`CREATE TABLE IF NOT EXISTS rate_limits (
			user_id VARCHAR(255) PRIMARY KEY,
			current_count INTEGER DEFAULT 0,
			window_start TIMESTAMP DEFAULT NOW(),
			limit_value INTEGER DEFAULT 1000,
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS suppression_list (
			email VARCHAR(255) PRIMARY KEY,
			reason VARCHAR(100),
			source VARCHAR(50),
			created_at TIMESTAMP DEFAULT NOW(),
			INDEX idx_email (email)
		)`,

		`CREATE TABLE IF NOT EXISTS analytics (
			date DATE PRIMARY KEY,
			total_sent INTEGER DEFAULT 0,
			total_delivered INTEGER DEFAULT 0,
			total_opened INTEGER DEFAULT 0,
			total_clicked INTEGER DEFAULT 0,
			total_bounced INTEGER DEFAULT 0,
			total_complained INTEGER DEFAULT 0,
			avg_delivery_time INTERVAL,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,
	}

	ctx := context.Background()
	for _, query := range queries {
		_, err := PostgresPool.Exec(ctx, query)
		if err != nil {
			return err
		}
	}

	return nil
}

// User Operations
func GetUserEmail(ctx context.Context, userID string) (string, error) {
	var email string
	err := PostgresPool.QueryRow(ctx,
		"SELECT email FROM users WHERE id = $1 AND status = 'active'",
		userID).Scan(&email)
	return email, err
}

func CreateUser(ctx context.Context, userID, email, name string, preferences map[string]interface{}) error {
	_, err := PostgresPool.Exec(ctx,
		`INSERT INTO users (id, email, name, preferences) 
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE 
		SET email = $2, name = $3, preferences = $4, updated_at = NOW()`,
		userID, email, name, preferences)
	return err
}

func UpdateUserStatus(ctx context.Context, userID, status string) error {
	_, err := PostgresPool.Exec(ctx,
		"UPDATE users SET status = $1, updated_at = NOW() WHERE id = $2",
		status, userID)
	return err
}

// Job Operations
func SaveJob(ctx context.Context, job *Job) error {
	_, err := PostgresPool.Exec(ctx,
		`INSERT INTO email_jobs (
			id, user_id, action, status, provider, message_id, 
			data, priority, retry_count, max_retries, scheduled_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		job.ID, job.UserID, job.Action, job.Status, job.Provider,
		job.MessageID, job.Data, job.Priority, job.RetryCount,
		job.MaxRetries, job.ScheduledAt)
	return err
}

func UpdateJobStatus(ctx context.Context, jobID, status string, result *JobResult) error {
	_, err := PostgresPool.Exec(ctx,
		`UPDATE email_jobs 
		SET status = $1, provider = $2, message_id = $3, 
			error = $4, completed_at = NOW(), updated_at = NOW()
		WHERE id = $5`,
		status, result.Provider, result.MessageID, result.Error, jobID)
	return err
}

func GetPendingJobs(ctx context.Context, limit int) ([]*Job, error) {
	rows, err := PostgresPool.Query(ctx,
		`SELECT id, user_id, action, data, priority, retry_count, 
			max_retries, scheduled_at, created_at
		FROM email_jobs 
		WHERE status = 'pending' 
			AND (scheduled_at IS NULL OR scheduled_at <= NOW())
		ORDER BY priority DESC, created_at ASC
		LIMIT $1`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var job Job
		err := rows.Scan(
			&job.ID, &job.UserID, &job.Action, &job.Data,
			&job.Priority, &job.RetryCount, &job.MaxRetries,
			&job.ScheduledAt, &job.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		job.Status = "pending"
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

// History Operations
func SaveHistory(ctx context.Context, history *EmailHistory) error {
	_, err := PostgresPool.Exec(ctx,
		`INSERT INTO email_history (
			job_id, user_id, email, action, status, provider, 
			message_id, error, duration
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		history.JobID, history.UserID, history.Email, history.Action,
		history.Status, history.Provider, history.MessageID,
		history.Error, history.Duration)
	return err
}

func GetUserHistory(ctx context.Context, userID string, limit int) ([]*EmailHistory, error) {
	rows, err := PostgresPool.Query(ctx,
		`SELECT job_id, user_id, email, action, status, provider, 
			message_id, error, duration, opened_at, clicked_at, created_at
		FROM email_history 
		WHERE user_id = $1 
		ORDER BY created_at DESC 
		LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []*EmailHistory
	for rows.Next() {
		var h EmailHistory
		err := rows.Scan(
			&h.JobID, &h.UserID, &h.Email, &h.Action, &h.Status,
			&h.Provider, &h.MessageID, &h.Error, &h.Duration,
			&h.OpenedAt, &h.ClickedAt, &h.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		history = append(history, &h)
	}
	return history, nil
}

// Template Operations
func SaveTemplate(ctx context.Context, template *Template) error {
	_, err := PostgresPool.Exec(ctx,
		`INSERT INTO templates (
			id, name, action, subject, html_content, text_content, 
			variables, version, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (action) DO UPDATE 
		SET name = $2, subject = $4, html_content = $5, 
			text_content = $6, variables = $7, version = templates.version + 1,
			updated_at = NOW()`,
		template.ID, template.Name, template.Action, template.Subject,
		template.HTMLContent, template.TextContent, template.Variables,
		template.Version, template.CreatedBy)
	return err
}

func GetTemplate(ctx context.Context, action string) (*Template, error) {
	var t Template
	err := PostgresPool.QueryRow(ctx,
		`SELECT id, name, action, subject, html_content, text_content, 
			variables, version, is_active, created_at, updated_at
		FROM templates 
		WHERE action = $1 AND is_active = true`,
		action).Scan(
		&t.ID, &t.Name, &t.Action, &t.Subject, &t.HTMLContent,
		&t.TextContent, &t.Variables, &t.Version, &t.IsActive,
		&t.CreatedAt, &t.UpdatedAt)
	return &t, err
}

// Webhook Operations
func GetWebhooks(ctx context.Context, userID string) ([]*Webhook, error) {
	rows, err := PostgresPool.Query(ctx,
		"SELECT id, url, events, secret FROM webhooks WHERE user_id = $1 AND is_active = true",
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []*Webhook
	for rows.Next() {
		var w Webhook
		err := rows.Scan(&w.ID, &w.URL, &w.Events, &w.Secret)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, &w)
	}
	return webhooks, nil
}

// Suppression List
func IsSuppressed(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := PostgresPool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM suppression_list WHERE email = $1)",
		email).Scan(&exists)
	return exists, err
}

func AddToSuppressionList(ctx context.Context, email, reason, source string) error {
	_, err := PostgresPool.Exec(ctx,
		"INSERT INTO suppression_list (email, reason, source) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING",
		email, reason, source)
	return err
}

// Analytics
func UpdateAnalytics(ctx context.Context, date time.Time, event string) error {
	var column string
	switch event {
	case "sent":
		column = "total_sent"
	case "delivered":
		column = "total_delivered"
	case "opened":
		column = "total_opened"
	case "clicked":
		column = "total_clicked"
	case "bounced":
		column = "total_bounced"
	case "complained":
		column = "total_complained"
	default:
		return nil
	}

	_, err := PostgresPool.Exec(ctx,
		`INSERT INTO analytics (date, `+column+`) 
		VALUES ($1, 1) 
		ON CONFLICT (date) DO UPDATE 
		SET `+column+` = analytics.`+column+` + 1,
			updated_at = NOW()`,
		date)
	return err
}

// Types
type Job struct {
	ID          string
	UserID      string
	Action      string
	Status      string
	Provider    string
	MessageID   string
	Data        map[string]interface{}
	Priority    int
	RetryCount  int
	MaxRetries  int
	ScheduledAt *time.Time
	CreatedAt   time.Time
}

type JobResult struct {
	Provider  string
	MessageID string
	Error     error
}

type EmailHistory struct {
	JobID      string
	UserID     string
	Email      string
	Action     string
	Status     string
	Provider   string
	MessageID  string
	Error      string
	Duration   time.Duration
	OpenedAt   *time.Time
	ClickedAt  *time.Time
	CreatedAt  time.Time
}

type Template struct {
	ID           string
	Name         string
	Action       string
	Subject      string
	HTMLContent  string
	TextContent  string
	Variables    map[string]interface{}
	Version      int
	IsActive     bool
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Webhook struct {
	ID     string
	URL    string
	Events []string
	Secret string
}