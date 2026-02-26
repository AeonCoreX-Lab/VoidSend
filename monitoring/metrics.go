package monitoring

import (
	"log"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	logger *logrus.Logger

	jobsProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "voidsend_jobs_processed_total",
			Help: "Total number of jobs processed",
		},
		[]string{"status", "provider"},
	)

	jobDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "voidsend_job_duration_seconds",
			Help:    "Duration of job processing",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"action"},
	)

	activeJobs = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "voidsend_active_jobs",
			Help: "Number of active jobs",
		},
	)

	queueSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "voidsend_queue_size",
			Help: "Current queue size",
		},
	)

	rateLimitHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "voidsend_rate_limit_hits_total",
			Help: "Total number of rate limit hits",
		},
	)
)

type Metrics struct {
	enabled bool
}

type EngineMetrics struct {
	TotalJobs      int64
	SuccessJobs    int64
	FailedJobs     int64
	AvgProcessTime time.Duration
	QueueLength    int
	ActiveWorkers  int
}

func init() {
	logger = logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})

	prometheus.MustRegister(jobsProcessed)
	prometheus.MustRegister(jobDuration)
	prometheus.MustRegister(activeJobs)
	prometheus.MustRegister(queueSize)
	prometheus.MustRegister(rateLimitHits)
}

func InitLogger(level string) {
	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		lvl = logrus.InfoLevel
	}
	logger.SetLevel(lvl)

	if dsn := os.Getenv("SENTRY_DSN"); dsn != "" {
		// Add Sentry hook
	}
}

func NewMetrics(enabled bool) *Metrics {
	return &Metrics{enabled: enabled}
}

func (m *Metrics) Start(port string) {
	if !m.enabled {
		return
	}
	r := gin.Default()
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	go func() {
		if err := r.Run(":" + port); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()
}

func (m *Metrics) Handler() gin.HandlerFunc {
	if !m.enabled {
		return func(c *gin.Context) {
			c.Status(404)
		}
	}
	return gin.WrapH(promhttp.Handler())
}

// Logging functions
func Info(format string, args ...interface{}) {
	logger.Infof(format, args...)
}

func Debug(format string, args ...interface{}) {
	logger.Debugf(format, args...)
}

func Warn(format string, args ...interface{}) {
	logger.Warnf(format, args...)
}

func Error(format string, args ...interface{}) {
	logger.Errorf(format, args...)
}

func Fatal(format string, args ...interface{}) {
	logger.Fatalf(format, args...)
}

// LogAlert – renamed from Alert to avoid conflict with alerts.go struct
func LogAlert(message string, fields map[string]interface{}) {
	logger.WithFields(fields).Error(message)
	// Send to alerting system (PagerDuty, OpsGenie, etc.)
}

// Metrics recording
func RecordJobProcessed(status, provider string) {
	jobsProcessed.WithLabelValues(status, provider).Inc()
}

func RecordJobDuration(action string, duration time.Duration) {
	jobDuration.WithLabelValues(action).Observe(duration.Seconds())
}

func SetActiveJobs(count int) {
	activeJobs.Set(float64(count))
}

func SetQueueSize(size int) {
	queueSize.Set(float64(size))
}

func IncRateLimitHit() {
	rateLimitHits.Inc()
}

// ADDED: RecordMetrics function to record engine metrics
func RecordMetrics(metrics *EngineMetrics) {
	SetActiveJobs(metrics.ActiveWorkers)
	SetQueueSize(metrics.QueueLength)
	// You can also record other metrics as gauges if needed
}

// Middleware
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		logger.WithFields(logrus.Fields{
			"status":     status,
			"method":     method,
			"path":       path,
			"ip":         c.ClientIP(),
			"latency":    latency,
			"user_agent": c.Request.UserAgent(),
		}).Info("Request processed")
	}
}