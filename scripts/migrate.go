package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/VoidSend/core/database"
	"gopkg.in/yaml.v3"
)

type MigrationConfig struct {
	Database struct {
		Postgres struct {
			URL string `yaml:"url"`
		} `yaml:"postgres"`
	} `yaml:"database"`
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config/config.yaml", "Path to config file")
	flag.Parse()

	// Load config
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatal("Failed to read config:", err)
	}

	var config MigrationConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		log.Fatal("Failed to parse config:", err)
	}

	// Connect to database
	ctx := context.Background()
	
	if err := database.InitPostgres(&database.PostgresConfig{
		URL: config.Database.Postgres.URL,
	}); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Run migrations
	if err := runMigrations(ctx); err != nil {
		log.Fatal("Migration failed:", err)
	}

	log.Println("✅ Migrations completed successfully")
}

func runMigrations(ctx context.Context) error {
	migrations := []string{
		// Initial schema
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT NOW()
		)`,

		// Version 1: Create users table
		`CREATE TABLE IF NOT EXISTS users (
			id VARCHAR(255) PRIMARY KEY,
			email VARCHAR(255) NOT NULL,
			name VARCHAR(255),
			preferences JSONB,
			status VARCHAR(50) DEFAULT 'active',
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// Version 2: Create email_jobs table
		`CREATE TABLE IF NOT EXISTS email_jobs (
			id VARCHAR(255) PRIMARY KEY,
			user_id VARCHAR(255) NOT NULL REFERENCES users(id),
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
			error TEXT
		)`,

		// Version 3: Create indexes
		`CREATE INDEX IF NOT EXISTS idx_email_jobs_user_id ON email_jobs(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_email_jobs_status ON email_jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_email_jobs_scheduled ON email_jobs(scheduled_at) WHERE status = 'pending'`,

		// Version 4: Create email_history table
		`CREATE TABLE IF NOT EXISTS email_history (
			id SERIAL PRIMARY KEY,
			job_id VARCHAR(255) REFERENCES email_jobs(id),
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
			created_at TIMESTAMP DEFAULT NOW()
		)`,

		// Version 5: Create templates table
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
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// Version 6: Create webhooks table
		`CREATE TABLE IF NOT EXISTS webhooks (
			id VARCHAR(255) PRIMARY KEY,
			user_id VARCHAR(255) NOT NULL REFERENCES users(id),
			url TEXT NOT NULL,
			events TEXT[],
			secret VARCHAR(255),
			is_active BOOLEAN DEFAULT true,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// Version 7: Create rate_limits table
		`CREATE TABLE IF NOT EXISTS rate_limits (
			user_id VARCHAR(255) PRIMARY KEY REFERENCES users(id),
			current_count INTEGER DEFAULT 0,
			window_start TIMESTAMP DEFAULT NOW(),
			limit_value INTEGER DEFAULT 1000,
			updated_at TIMESTAMP DEFAULT NOW()
		)`,

		// Version 8: Create suppression_list table
		`CREATE TABLE IF NOT EXISTS suppression_list (
			email VARCHAR(255) PRIMARY KEY,
			reason VARCHAR(100),
			source VARCHAR(50),
			created_at TIMESTAMP DEFAULT NOW()
		)`,

		// Version 9: Create analytics table
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

	for version, migration := range migrations {
		// Check if migration already applied
		var exists bool
		err := database.PostgresPool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)",
			version).Scan(&exists)
		
		if err != nil {
			return err
		}

		if exists {
			log.Printf("Migration %d already applied, skipping", version)
			continue
		}

		// Apply migration
		log.Printf("Applying migration %d...", version)
		
		tx, err := database.PostgresPool.Begin(ctx)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, migration); err != nil {
			tx.Rollback(ctx)
			return err
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)",
			version); err != nil {
			tx.Rollback(ctx)
			return err
		}

		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}

	return nil
}