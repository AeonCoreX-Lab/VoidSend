package api

import (
	"context"
	"encoding/json"   // <-- added this import
	"net/http"
	"time"

	"github.com/AeonCoreX-Lab/VoidSend/core/database"
	"github.com/AeonCoreX-Lab/VoidSend/monitoring"
	"github.com/gin-gonic/gin"
)

// Developer struct
type Developer struct {
	ID    string
	Email string
	Name  string
}

// APIKeyMiddleware – API key validate করে
func APIKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// headers থেকে API key খুঁজি
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			apiKey = c.Query("api_key")
		}

		if apiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "API key required. Provide X-API-Key header",
			})
			return
		}

		// প্রথমে Redis cache-এ চেক করি
		dev, err := getDeveloperFromCache(apiKey)
		if err != nil {
			// cache-এ না থাকলে database-এ চেক করি
			dev, err = validateAPIKeyInDB(apiKey)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "Invalid or inactive API key",
				})
				return
			}
			// cache-এ save করি (৫ মিনিটের জন্য)
			cacheDeveloper(apiKey, dev)
		}

		// Developer info context-এ রাখি
		c.Set("developer_id", dev.ID)
		c.Set("developer_email", dev.Email)
		c.Set("developer_name", dev.Name)
		c.Set("api_key", apiKey)

		c.Next()
	}
}

// validateAPIKeyInDB database-এ API key চেক করে
func validateAPIKeyInDB(apiKey string) (*Developer, error) {
	ctx := context.Background()
	var dev Developer

	err := database.PostgresPool.QueryRow(ctx,
		"SELECT id, email, name FROM developers WHERE api_key = $1 AND is_active = true",
		apiKey).Scan(&dev.ID, &dev.Email, &dev.Name)

	if err != nil {
		return nil, err
	}
	return &dev, nil
}

// getDeveloperFromCache Redis থেকে developer data আনে
func getDeveloperFromCache(apiKey string) (*Developer, error) {
	ctx := context.Background()
	data, err := database.RedisClient.Get(ctx, "apikey:"+apiKey).Bytes()
	if err != nil {
		return nil, err
	}
	var dev Developer
	if err := json.Unmarshal(data, &dev); err != nil {
		return nil, err
	}
	return &dev, nil
}

// cacheDeveloper Redis-এ developer data সংরক্ষণ করে
func cacheDeveloper(apiKey string, dev *Developer) {
	ctx := context.Background()
	data, _ := json.Marshal(dev)
	database.RedisClient.Set(ctx, "apikey:"+apiKey, data, 5*time.Minute)
}