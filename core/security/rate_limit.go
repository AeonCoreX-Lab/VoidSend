package security

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/AeonCoreX-Lab/VoidSend/core/database"
	"github.com/AeonCoreX-Lab/VoidSend/monitoring"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

var (
	ipLimiters = make(map[string]*rate.Limiter)
	mu         sync.RWMutex
)

type RateLimitConfig struct {
	RequestsPerSecond float64
	Burst             int
	Window            time.Duration
}

func RateLimitMiddleware(limit int, window time.Duration) gin.HandlerFunc {
	config := &RateLimitConfig{
		RequestsPerSecond: float64(limit) / window.Seconds(),
		Burst:             limit,
		Window:            window,
	}

	return func(c *gin.Context) {
		// Get client IP
		clientIP := c.ClientIP()
		
		// Check if IP is allowed
		if !isIPAllowed(clientIP) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "IP not allowed",
			})
			return
		}

		// Get limiter for IP
		limiter := getLimiterForIP(clientIP, config)

		// Check rate limit
		if !limiter.Allow() {
			monitoring.IncRateLimitHit()
			
			// Check Redis for distributed rate limiting
			allowed, err := database.RateLimitCheck(c.Request.Context(), 
				"ip:"+clientIP, config.Burst, config.Window)
			
			if err != nil || !allowed {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error":       "Rate limit exceeded",
					"retry_after": config.Window.Seconds(),
				})
				return
			}
		}

		c.Next()
	}
}

func getLimiterForIP(ip string, config *RateLimitConfig) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()

	limiter, exists := ipLimiters[ip]
	if !exists {
		limiter = rate.NewLimiter(rate.Limit(config.RequestsPerSecond), config.Burst)
		ipLimiters[ip] = limiter

		// Cleanup old limiters
		go func() {
			time.Sleep(config.Window * 2)
			mu.Lock()
			delete(ipLimiters, ip)
			mu.Unlock()
		}()
	}

	return limiter
}

func isIPAllowed(ip string) bool {
	// Parse IP
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Check against whitelist
	allowedCIDRs := []string{
		"0.0.0.0/0", // Allow all - configure in production
	}

	for _, cidr := range allowedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipNet.Contains(parsedIP) {
			return true
		}
	}

	return false
}

// User-based rate limiting
func UserRateLimitMiddleware(requestsPerDay int) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		if userID == "" {
			c.Next()
			return
		}

		ctx := c.Request.Context()
		key := "user_rate:" + userID + ":" + time.Now().Format("2006-01-02")

		count, err := database.RedisClient.Incr(ctx, key).Result()
		if err != nil {
			c.Next()
			return
		}

		if count == 1 {
			database.RedisClient.Expire(ctx, key, 24*time.Hour)
		}

		if count > int64(requestsPerDay) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Daily request limit exceeded",
			})
			return
		}

		c.Next()
	}
}

// API Key rate limiting
func APIKeyRateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			c.Next()
			return
		}

		ctx := c.Request.Context()
		key := "api_rate:" + apiKey + ":" + time.Now().Format("2006-01-02-15")

		count, err := database.RedisClient.Incr(ctx, key).Result()
		if err != nil {
			c.Next()
			return
		}

		if count == 1 {
			database.RedisClient.Expire(ctx, key, time.Hour)
		}

		// Get limits from database
		limit, _ := database.RedisClient.Get(ctx, "api_limit:"+apiKey).Int()

		if limit == 0 {
			limit = 1000 // Default
		}

		if count > int64(limit) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "API rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}