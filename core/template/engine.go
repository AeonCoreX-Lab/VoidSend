package template

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AeonCoreX-Lab/VoidSend/core/database"
	"github.com/AeonCoreX-Lab/VoidSend/monitoring"
)

// TemplateEngine ফাইল-ভিত্তিক টেমপ্লেট হ্যান্ডেল করে
type TemplateEngine struct {
	cache     map[string]*CachedTemplate
	mu        sync.RWMutex
	functions template.FuncMap
}

type CachedTemplate struct {
	Template  *template.Template
	Hash      string
	LastUsed  time.Time
	CreatedAt time.Time
}

type TemplateData struct {
	User      map[string]interface{}
	App       map[string]interface{}
	Custom    map[string]interface{}
	Timestamp time.Time
	Year      int
}

var (
	engine      *TemplateEngine
	engineOnce  sync.Once
	templateDir = "templates"
)

// GetEngine returns the singleton TemplateEngine
func GetEngine() *TemplateEngine {
	engineOnce.Do(func() {
		engine = &TemplateEngine{
			cache: make(map[string]*CachedTemplate),
			functions: template.FuncMap{
				"upper":      strings.ToUpper,
				"lower":      strings.ToLower,
				"title":      strings.Title,
				"formatDate": func(t time.Time, layout string) string { return t.Format(layout) },
				"now":        time.Now,
				"add":        func(a, b int) int { return a + b },
				"sub":        func(a, b int) int { return a - b },
				"mul":        func(a, b int) int { return a * b },
				"div":        func(a, b int) int { return a / b },
				"mod":        func(a, b int) int { return a % b },
				"join":       strings.Join,
				"split":      strings.Split,
				"replace":    strings.ReplaceAll,
				"json": func(v interface{}) string {
					b, _ := json.Marshal(v)
					return string(b)
				},
				"default": func(def, val interface{}) interface{} {
					if val == nil {
						return def
					}
					return val
				},
				"dict": func(values ...interface{}) (map[string]interface{}, error) {
					if len(values)%2 != 0 {
						return nil, fmt.Errorf("invalid dict call")
					}
					dict := make(map[string]interface{}, len(values)/2)
					for i := 0; i < len(values); i += 2 {
						key, ok := values[i].(string)
						if !ok {
							return nil, fmt.Errorf("dict keys must be strings")
						}
						dict[key] = values[i+1]
					}
					return dict, nil
				},
			},
		}
		go engine.cleanupCache()
	})
	return engine
}

// ProcessTemplateFile শুধুমাত্র ফাইল সিস্টেম থেকে টেমপ্লেট প্রসেস করে
func ProcessTemplateFile(action string, data map[string]interface{}) (subject, htmlBody string, err error) {
	e := GetEngine()
	subject = getDefaultSubject(action)

	htmlBody, err = e.renderFileTemplate(action+".html", data)
	if err != nil {
		return "", "", err
	}

	return subject, htmlBody, nil
}

// renderFileTemplate ফাইল থেকে টেমপ্লেট রেন্ডার করে
func (e *TemplateEngine) renderFileTemplate(filename string, data map[string]interface{}) (string, error) {
	e.mu.RLock()
	cached, exists := e.cache[filename]
	e.mu.RUnlock()

	filePath := filepath.Join(templateDir, filename)

	if exists {
		cached.LastUsed = time.Now()
		var buf bytes.Buffer
		err := cached.Template.Execute(&buf, e.prepareData(data))
		if err == nil {
			return buf.String(), nil
		}
		monitoring.Warn("Template %s execution failed, reloading: %v", filename, err)
	}

	// ফাইল থেকে টেমপ্লেট লোড
	tmpl, err := template.New(filename).Funcs(e.functions).ParseFiles(filePath)
	if err != nil {
		return "", err
	}

	// হ্যাশ ক্যালকুলেট
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	hash := md5.Sum(content)
	hashStr := hex.EncodeToString(hash[:])

	// ক্যাশে রাখুন
	e.mu.Lock()
	e.cache[filename] = &CachedTemplate{
		Template:  tmpl,
		Hash:      hashStr,
		LastUsed:  time.Now(),
		CreatedAt: time.Now(),
	}
	e.mu.Unlock()

	// Redis-এ ক্যাশ
	go func() {
		cacheData := map[string]interface{}{
			"hash":    hashStr,
			"content": string(content),
		}
		database.RedisClient.Set(context.Background(), "template:"+filename, cacheData, time.Hour*24)
	}()

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, e.prepareData(data))
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// prepareData ডাটা প্রস্তুত করে
func (e *TemplateEngine) prepareData(data map[string]interface{}) TemplateData {
	now := time.Now()

	tplData := TemplateData{
		User:      make(map[string]interface{}),
		App:       make(map[string]interface{}),
		Custom:    data,
		Timestamp: now,
		Year:      now.Year(),
	}

	if user, ok := data["user"].(map[string]interface{}); ok {
		tplData.User = user
		delete(data, "user")
	}

	if app, ok := data["app"].(map[string]interface{}); ok {
		tplData.App = app
		delete(data, "app")
	}

	return tplData
}

// cleanupCache periodically cleans old cache entries
func (e *TemplateEngine) cleanupCache() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		e.mu.Lock()
		for name, cached := range e.cache {
			if time.Since(cached.LastUsed) > 24*time.Hour {
				delete(e.cache, name)
				monitoring.Debug("Removed unused template: %s", name)
			}
		}
		e.mu.Unlock()
	}
}

// getDefaultSubject ডিফল্ট সাবজেক্ট রিটার্ন করে
func getDefaultSubject(action string) string {
	subjects := map[string]string{
		"welcome":        "Welcome!",
		"otp":            "Your OTP Code",
		"password_reset": "Password Reset Request",
		"invoice":        "Your Invoice",
		"notification":   "New Notification",
	}

	if subject, ok := subjects[action]; ok {
		return subject
	}
	return "Notification from VoidSend"
}

// GetFileTemplate ফাইল টেমপ্লেট রিটার্ন করে (manager-এর জন্য)
func (e *TemplateEngine) GetFileTemplate(action string) (*template.Template, error) {
	cacheKey := action + ".html"

	e.mu.RLock()
	cached, exists := e.cache[cacheKey]
	e.mu.RUnlock()

	if exists {
		return cached.Template, nil
	}

	// ফাইল থেকে লোড করুন
	filePath := filepath.Join(templateDir, action+".html")
	tmpl, err := template.New(action+".html").Funcs(e.functions).ParseFiles(filePath)
	if err != nil {
		return nil, err
	}

	return tmpl, nil
}

// GetFunctions রিটার্ন করে template functions
func (e *TemplateEngine) GetFunctions() template.FuncMap {
	return e.functions
}