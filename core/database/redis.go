package database

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/AeonCoreX-Lab/VoidSend/monitoring"
)

var RedisClient *redis.Client

type RedisConfig struct {
	URL      string
	Password string
	DB       int
}

func InitRedis(config *RedisConfig) error {
	opt, err := redis.ParseURL(config.URL)
	if err != nil {
		// If not a URL, use as host:port
		RedisClient = redis.NewClient(&redis.Options{
			Addr:     config.URL,
			Password: config.Password,
			DB:       config.DB,
		})
	} else {
		opt.Password = config.Password
		opt.DB = config.DB
		RedisClient = redis.NewClient(opt)
	}

	ctx := context.Background()
	if err := RedisClient.Ping(ctx).Err(); err != nil {
		return err
	}

	monitoring.Info("Redis connected successfully")
	return nil
}

// Cache Operations
func CacheSet(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return RedisClient.Set(ctx, key, data, expiration).Err()
}

func CacheGet(ctx context.Context, key string, dest interface{}) error {
	data, err := RedisClient.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

func CacheDelete(ctx context.Context, key string) error {
	return RedisClient.Del(ctx, key).Err()
}

// Queue Operations
func QueuePush(ctx context.Context, queue string, job interface{}) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return RedisClient.LPush(ctx, "queue:"+queue, data).Err()
}

func QueuePop(ctx context.Context, queue string, timeout time.Duration) ([]byte, error) {
	result, err := RedisClient.BRPop(ctx, timeout, "queue:"+queue).Result()
	if err != nil {
		return nil, err
	}
	if len(result) < 2 {
		return nil, nil
	}
	return []byte(result[1]), nil
}

func QueueLength(ctx context.Context, queue string) (int64, error) {
	return RedisClient.LLen(ctx, "queue:"+queue).Result()
}

// Rate Limiting
func RateLimitCheck(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	now := time.Now().UnixNano()
	windowStart := now - window.Nanoseconds()

	// Remove old entries
	RedisClient.ZRemRangeByScore(ctx, "rate:"+key, "0", string(rune(windowStart)))

	// Add current request
	RedisClient.ZAdd(ctx, "rate:"+key, &redis.Z{
		Score:  float64(now),
		Member: now,
	})

	// Set expiration
	RedisClient.Expire(ctx, "rate:"+key, window)

	// Count requests in window
	count, err := RedisClient.ZCard(ctx, "rate:"+key).Result()
	if err != nil {
		return false, err
	}

	return count <= int64(limit), nil
}

// Distributed Locks
func AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return RedisClient.SetNX(ctx, "lock:"+key, "1", ttl).Result()
}

func ReleaseLock(ctx context.Context, key string) error {
	return RedisClient.Del(ctx, "lock:"+key).Err()
}

// Pub/Sub
func Publish(ctx context.Context, channel string, message interface{}) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return RedisClient.Publish(ctx, channel, data).Err()
}

func Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	return RedisClient.Subscribe(ctx, channels...)
}

// Leaderboard / Analytics
func IncrementCounter(ctx context.Context, key string, incr int64) error {
	return RedisClient.IncrBy(ctx, "counter:"+key, incr).Err()
}

func GetCounter(ctx context.Context, key string) (int64, error) {
	return RedisClient.Get(ctx, "counter:"+key).Int64()
}

// Set Operations
func SetAdd(ctx context.Context, key string, members ...interface{}) error {
	return RedisClient.SAdd(ctx, "set:"+key, members...).Err()
}

// FIXED: Removed the stray quote that was causing syntax error
func SetMembers(ctx context.Context, key string) ([]string, error) {
	return RedisClient.SMembers(ctx, "set:"+key).Result()
}

// Sorted Sets for Scheduling
func ScheduleAdd(ctx context.Context, key string, score float64, member interface{}) error {
	return RedisClient.ZAdd(ctx, "schedule:"+key, &redis.Z{
		Score:  score,
		Member: member,
	}).Err()
}

func ScheduleGetDue(ctx context.Context, key string, maxScore float64) ([]string, error) {
	return RedisClient.ZRangeByScore(ctx, "schedule:"+key, &redis.ZRangeBy{
		Min: "-inf",
		Max: string(rune(maxScore)),
	}).Result()
}

// Hash Operations for User Data
func HashSet(ctx context.Context, key string, values map[string]interface{}) error {
	return RedisClient.HSet(ctx, "hash:"+key, values).Err()
}

func HashGet(ctx context.Context, key, field string) (string, error) {
	return RedisClient.HGet(ctx, "hash:"+key, field).Result()
}

func HashGetAll(ctx context.Context, key string) (map[string]string, error) {
	return RedisClient.HGetAll(ctx, "hash:"+key).Result()
}