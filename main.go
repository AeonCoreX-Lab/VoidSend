package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/VoidSend/api"
	"github.com/VoidSend/core/database"
	"github.com/VoidSend/core/engine"
	"github.com/VoidSend/core/mailer"
	"github.com/VoidSend/core/security"
	"github.com/VoidSend/core/template"
	"github.com/VoidSend/monitoring"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// ==================== কনফিগারেশন স্ট্রাকচার ====================

type UltraConfig struct {
	Server struct {
		Port            string        `yaml:"port"`
		Mode            string        `yaml:"mode"`
		ReadTimeout     time.Duration `yaml:"read_timeout"`
		WriteTimeout    time.Duration `yaml:"write_timeout"`
		MaxHeaderBytes  int           `yaml:"max_header_bytes"`
		ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	} `yaml:"server"`

	Engine struct {
		MaxWorkers    int           `yaml:"max_workers"`
		QueueSize     int           `yaml:"queue_size"`
		RetryAttempts int           `yaml:"retry_attempts"`
		RetryDelay    time.Duration `yaml:"retry_delay"`
		BatchSize     int           `yaml:"batch_size"`
		MaxConcurrent int           `yaml:"max_concurrent"`
	} `yaml:"engine"`

	Providers struct {
		Primary   string        `yaml:"primary"`
		Fallbacks []string      `yaml:"fallbacks"`
		SMTP      SMTPConfig    `yaml:"smtp"`
		SendGrid  SendGridConfig `yaml:"sendgrid"`
		AWS       AWSConfig     `yaml:"aws"`
	} `yaml:"providers"`

	Database struct {
		Postgres PostgresConfig `yaml:"postgres"`
		Redis    RedisConfig    `yaml:"redis"`
		MongoDB  MongoDBConfig  `yaml:"mongodb"`
	} `yaml:"database"`

	Security struct {
		RateLimit     int           `yaml:"rate_limit"`
		RateWindow    time.Duration `yaml:"rate_window"`
		EncryptionKey string        `yaml:"encryption_key"`
		AllowedIPs    []string      `yaml:"allowed_ips"`
		JWTSecret     string        `yaml:"jwt_secret"`
	} `yaml:"security"`

	Monitoring struct {
		PrometheusEnabled bool   `yaml:"prometheus_enabled"`
		MetricsPort       string `yaml:"metrics_port"`
		LogLevel          string `yaml:"log_level"`
		SentryDSN         string `yaml:"sentry_dsn"`
	} `yaml:"monitoring"`
}

type SMTPConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	User       string `yaml:"user"`
	Pass       string `yaml:"pass"`
	FromName   string `yaml:"from_name"`
	FromEmail  string `yaml:"from_email"`
	Encryption string `yaml:"encryption"` // TLS, SSL, STARTTLS
	PoolSize   int    `yaml:"pool_size"`
}

type SendGridConfig struct {
	APIKey      string `yaml:"api_key"`
	FromEmail   string `yaml:"from_email"`
	FromName    string `yaml:"from_name"`
	SandboxMode bool   `yaml:"sandbox_mode"`
}

type AWSConfig struct {
	Region    string `yaml:"region"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	FromEmail string `yaml:"from_email"`
	ConfigSet string `yaml:"configuration_set"`
}

type PostgresConfig struct {
	URL             string        `yaml:"url"`
	MaxConns        int32         `yaml:"max_conns"`
	MinConns        int32         `yaml:"min_conns"`
	MaxConnLifetime time.Duration `yaml:"max_conn_lifetime"`
	MaxConnIdleTime time.Duration `yaml:"max_conn_idle_time"`
}

type RedisConfig struct {
	URL      string `yaml:"url"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type MongoDBConfig struct {
	URL        string `yaml:"url"`
	Database   string `yaml:"database"`
	Collection string `yaml:"collection"`
}

// ==================== গ্লোবাল ভেরিয়েবল ====================

var GlobalConfig UltraConfig
var EngineInstance *engine.UltraEngine
var MetricsServer *monitoring.Metrics

// ==================== ইনিশিয়ালাইজেশন ====================

func init() {
	loadConfig()
	monitoring.InitLogger(GlobalConfig.Monitoring.LogLevel)
	MetricsServer = monitoring.NewMetrics(GlobalConfig.Monitoring.PrometheusEnabled)
}

func loadConfig() {
	file, err := os.ReadFile("config/config.yaml")
	if err != nil {
		log.Fatalf("🔥 Critical: Failed to read config - %v", err)
	}
	err = yaml.Unmarshal(file, &GlobalConfig)
	if err != nil {
		log.Fatalf("🔥 Critical: Failed to parse config - %v", err)
	}

	// Environment variables override
	if port := os.Getenv("PORT"); port != "" {
		GlobalConfig.Server.Port = port
	}
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		GlobalConfig.Database.Postgres.URL = dbURL
	}
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		GlobalConfig.Database.Redis.URL = redisURL
	}

	validateConfig()
}

func validateConfig() {
	if GlobalConfig.Server.Port == "" {
		GlobalConfig.Server.Port = "8080"
	}
	if GlobalConfig.Engine.MaxWorkers == 0 {
		GlobalConfig.Engine.MaxWorkers = 100
	}
	if GlobalConfig.Security.RateLimit == 0 {
		GlobalConfig.Security.RateLimit = 1000
	}
}

// ==================== ডাটাবেজ ও প্রোভাইডার ইনিশিয়ালাইজেশন ====================

func initDatabases() {
	if GlobalConfig.Database.Postgres.URL != "" {
		err := database.InitPostgres(&database.PostgresConfig{
			URL:             GlobalConfig.Database.Postgres.URL,
			MaxConns:        GlobalConfig.Database.Postgres.MaxConns,
			MinConns:        GlobalConfig.Database.Postgres.MinConns,
			MaxConnLifetime: GlobalConfig.Database.Postgres.MaxConnLifetime,
			MaxConnIdleTime: GlobalConfig.Database.Postgres.MaxConnIdleTime,
		})
		if err != nil {
			log.Printf("⚠️ PostgreSQL connection failed: %v", err)
		}
	}

	if GlobalConfig.Database.Redis.URL != "" {
		err := database.InitRedis(&database.RedisConfig{
			URL:      GlobalConfig.Database.Redis.URL,
			Password: GlobalConfig.Database.Redis.Password,
			DB:       GlobalConfig.Database.Redis.DB,
		})
		if err != nil {
			log.Printf("⚠️ Redis connection failed: %v", err)
		}
	}
}

func initProviders() {
	if GlobalConfig.Providers.SMTP.Host != "" {
		mailer.InitSMTPPool(&mailer.SMTPConfig{
			Host:       GlobalConfig.Providers.SMTP.Host,
			Port:       GlobalConfig.Providers.SMTP.Port,
			User:       GlobalConfig.Providers.SMTP.User,
			Pass:       GlobalConfig.Providers.SMTP.Pass,
			FromName:   GlobalConfig.Providers.SMTP.FromName,
			FromEmail:  GlobalConfig.Providers.SMTP.FromEmail,
			Encryption: GlobalConfig.Providers.SMTP.Encryption,
			PoolSize:   GlobalConfig.Providers.SMTP.PoolSize,
		})
	}

	if GlobalConfig.Providers.SendGrid.APIKey != "" {
		mailer.InitSendGrid(&mailer.SendGridConfig{
			APIKey:      GlobalConfig.Providers.SendGrid.APIKey,
			FromEmail:   GlobalConfig.Providers.SendGrid.FromEmail,
			FromName:    GlobalConfig.Providers.SendGrid.FromName,
			SandboxMode: GlobalConfig.Providers.SendGrid.SandboxMode,
		})
	}

	if GlobalConfig.Providers.AWS.AccessKey != "" {
		mailer.InitAWSSES(&mailer.AWSConfig{
			Region:    GlobalConfig.Providers.AWS.Region,
			AccessKey: GlobalConfig.Providers.AWS.AccessKey,
			SecretKey: GlobalConfig.Providers.AWS.SecretKey,
			FromEmail: GlobalConfig.Providers.AWS.FromEmail,
			ConfigSet: GlobalConfig.Providers.AWS.ConfigSet,
		})
	}
}

// ==================== মেইন ফাংশন ====================

func main() {
	if GlobalConfig.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// ইঞ্জিন ইনিশিয়ালাইজ
	EngineInstance = engine.NewUltraEngine(&engine.Config{
		MaxWorkers:    GlobalConfig.Engine.MaxWorkers,
		QueueSize:     GlobalConfig.Engine.QueueSize,
		RetryAttempts: GlobalConfig.Engine.RetryAttempts,
		RetryDelay:    GlobalConfig.Engine.RetryDelay,
		BatchSize:     GlobalConfig.Engine.BatchSize,
		MaxConcurrent: GlobalConfig.Engine.MaxConcurrent,
	})

	// api প্যাকেজের EngineInstance সেট করুন
	api.EngineInstance = EngineInstance

	initDatabases()
	initProviders()

	// টেমপ্লেট ম্যানেজার প্রস্তুত (সিঙ্গেলটন ইনিশিয়ালাইজ)
	template.GetManager()

	EngineInstance.Start()

	router := setupRouter()

	srv := &http.Server{
		Addr:           ":" + GlobalConfig.Server.Port,
		Handler:        router,
		ReadTimeout:    GlobalConfig.Server.ReadTimeout,
		WriteTimeout:   GlobalConfig.Server.WriteTimeout,
		MaxHeaderBytes: GlobalConfig.Server.MaxHeaderBytes,
	}

	if GlobalConfig.Monitoring.PrometheusEnabled {
		go MetricsServer.Start(GlobalConfig.Monitoring.MetricsPort)
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("🔥 Failed to start server: %v", err)
		}
	}()

	log.Printf("🌌 VoidSend Ultra Engine ignited on port %s", GlobalConfig.Server.Port)
	log.Printf("🚀 Workers: %d | Queue: %d | Rate Limit: %d/sec",
		GlobalConfig.Engine.MaxWorkers,
		GlobalConfig.Engine.QueueSize,
		GlobalConfig.Security.RateLimit)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down VoidSend Engine...")

	EngineInstance.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), GlobalConfig.Server.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("🔥 Server forced to shutdown:", err)
	}

	log.Println("✅ VoidSend Engine gracefully stopped")
}

// ==================== রাউটার সেটআপ ====================

func setupRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(monitoring.LoggerMiddleware())
	r.Use(security.RateLimitMiddleware(GlobalConfig.Security.RateLimit, GlobalConfig.Security.RateWindow))
	r.Use(security.IPWhitelistMiddleware(GlobalConfig.Security.AllowedIPs))

	// হেল্থ এন্ডপয়েন্ট
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "operational",
			"engine":    "VoidSend Ultra Max",
			"version":   "2.1.0",
			"timestamp": time.Now().Unix(),
		})
	})

	// মেট্রিক্স এন্ডপয়েন্ট
	r.GET("/metrics", MetricsServer.Handler())

	// API v2 গ্রুপ (API Key প্রয়োজন)
	v2 := r.Group("/v2")
	v2.Use(api.APIKeyMiddleware())
	{
		// টেমপ্লেট ম্যানেজমেন্ট API
		templateGroup := v2.Group("/template")
		{
			templateGroup.POST("/upload", api.HandleTemplateUpload)
			templateGroup.GET("/:action", api.HandleGetTemplate)
			templateGroup.GET("/list", api.HandleListTemplates)
			templateGroup.DELETE("/:action", api.HandleDeleteTemplate)
		}

		// ডিসপ্যাচ API
		v2.POST("/ultra-dispatch", handleUltraDispatch)
		v2.POST("/batch-dispatch", handleBatchDispatch)
		v2.POST("/scheduled-dispatch", handleScheduledDispatch)
	}

	// ওয়েবহুক এন্ডপয়েন্ট (পাবলিক)
	r.POST("/webhook/status", handleStatusWebhook)
	r.POST("/webhook/bounce", handleBounceWebhook)

	return r
}

// ==================== ডিসপ্যাচ হ্যান্ডলার ====================

type dispatchRequest struct {
	UserID      string                 `json:"user_id" binding:"required"`
	Action      string                 `json:"action" binding:"required"`
	Data        map[string]interface{} `json:"data"`
	Priority    int                    `json:"priority"`
	ScheduledAt *time.Time              `json:"scheduled_at"`
	Provider    string                  `json:"provider"`
	Tags        []string                `json:"tags"`
}

func handleUltraDispatch(c *gin.Context) {
	var req dispatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// ডেভেলপার আইডি পান (মিডলওয়্যার থেকে)
	devID, _ := c.Get("developer_id")
	devIDStr, _ := devID.(string)

	// টেমপ্লেট প্রসেস করুন (ইউনিফাইড API)
	subject, htmlBody, err := template.ProcessTemplate(devIDStr, req.Action, req.Data)
	if err != nil {
		// যদি কাস্টম টেমপ্লেট না থাকে, ফাইল টেমপ্লেট试试 করুন
		subject, htmlBody, err = template.ProcessTemplateFile(req.Action, req.Data)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Template error: " + err.Error(),
			})
			return
		}
	}

	// ইউজারের ইমেইল পান
	userEmail, err := database.GetUserEmail(c.Request.Context(), req.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// ইঞ্জিন জব তৈরি করুন
	job := &engine.DispatchJob{
		ID:          generateJobID(),
		UserID:      req.UserID,
		ToEmail:     userEmail,
		Action:      req.Action,
		Subject:     subject,
		HTMLBody:    htmlBody,
		Data:        req.Data,
		Priority:    req.Priority,
		ScheduledAt: req.ScheduledAt,
		Provider:    req.Provider,
		Tags:        req.Tags,
	}

	// ইঞ্জিনে ডিসপ্যাচ করুন
	if err := EngineInstance.Dispatch(job); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "Dispatch failed",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":    "queued",
		"job_id":    job.ID,
		"message":   "Email will be dispatched shortly",
		"timestamp": time.Now().Unix(),
	})
}

func handleBatchDispatch(c *gin.Context) {
	var req struct {
		Jobs []dispatchRequest `json:"jobs" binding:"required,min=1,max=100"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	devID, _ := c.Get("developer_id")
	devIDStr, _ := devID.(string)

	jobs := make([]*engine.DispatchJob, len(req.Jobs))
	results := make([]map[string]interface{}, len(req.Jobs))

	for i, j := range req.Jobs {
		// টেমপ্লেট প্রসেস
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

		userEmail, err := database.GetUserEmail(c.Request.Context(), j.UserID)
		if err != nil {
			results[i] = map[string]interface{}{
				"index":  i,
				"status": "failed",
				"error":  "User not found",
			}
			continue
		}

		job := &engine.DispatchJob{
			ID:          generateJobID(),
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
		jobs[i] = job
		results[i] = map[string]interface{}{
			"index":  i,
			"job_id": job.ID,
			"status": "queued",
		}
	}

	errors := EngineInstance.BatchDispatch(jobs)
	for i, err := range errors {
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

func handleScheduledDispatch(c *gin.Context) {
	var req struct {
		UserID     string                 `json:"user_id" binding:"required"`
		Action     string                 `json:"action" binding:"required"`
		Data       map[string]interface{} `json:"data"`
		ScheduleAt time.Time              `json:"schedule_at" binding:"required"`
		Repeat     string                  `json:"repeat"` // daily, weekly, monthly
		EndAt      *time.Time              `json:"end_at"`
	}
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

	subject, htmlBody, err := template.ProcessTemplate(devIDStr, req.Action, req.Data)
	if err != nil {
		subject, htmlBody, err = template.ProcessTemplateFile(req.Action, req.Data)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Template error: " + err.Error()})
			return
		}
	}

	userEmail, err := database.GetUserEmail(c.Request.Context(), req.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	job := &engine.DispatchJob{
		ID:          generateJobID(),
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

	// যদি রিপিট সেট করা থাকে, ডাটাবেজে সংরক্ষণ করুন (এখানে শুধু উদাহরণ)
	if req.Repeat != "" {
		// রিকারিং জব সংরক্ষণের কোড
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":       "scheduled",
		"job_id":       job.ID,
		"scheduled_at": req.ScheduleAt,
		"message":      "Email scheduled successfully",
	})
}

// ==================== ওয়েবহুক হ্যান্ডলার ====================

func handleStatusWebhook(c *gin.Context) {
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
	// ডাটাবেজে আপডেট করুন

	c.JSON(http.StatusOK, gin.H{"status": "received"})
}

func handleBounceWebhook(c *gin.Context) {
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
	// সাপ্রেশন লিস্টে যোগ করুন

	c.JSON(http.StatusOK, gin.H{"status": "processed"})
}

// ==================== হেল্পার ফাংশন ====================

func generateJobID() string {
	return "job_" + time.Now().Format("20060102150405") + "_" + randomString(6)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
		time.Sleep(1) // ন্যানোসেকেন্ড ভিন্নতা আনতে
	}
	return string(b)
}