package template

import (
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

	"github.com/VoidSend/core/database"
	"github.com/VoidSend/monitoring"
	"github.com/google/uuid"
)

// TemplateManager ডাটাবেজ-ভিত্তিক টেমপ্লেট ম্যানেজমেন্ট handle করে
type TemplateManager struct {
	mu            sync.RWMutex
	fileCache     map[string]*FileCacheEntry    // ফাইল সিস্টেম থেকে লোড করা টেমপ্লেট
	dbCache       map[string]*DeveloperTemplate // ডাটাবেজ থেকে লোড করা টেমপ্লেট
	fileEngine    *TemplateEngine                // ফাইল ইঞ্জিনের রেফারেন্স
	functions     template.FuncMap
	templateDir   string
}

type FileCacheEntry struct {
	Template  *template.Template
	Hash      string
	LastUsed  time.Time
	IsFile    bool
}

type DeveloperTemplate struct {
	ID          string    `json:"id"`
	DeveloperID string    `json:"developer_id"`
	Action      string    `json:"action"`
	HTMLContent string    `json:"html_content"`
	Subject     string    `json:"subject"`
	Version     int       `json:"version"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

var (
	manager     *TemplateManager
	managerOnce sync.Once
)

// GetManager returns the singleton TemplateManager
func GetManager() *TemplateManager {
	managerOnce.Do(func() {
		manager = &TemplateManager{
			fileCache:   make(map[string]*FileCacheEntry),
			dbCache:     make(map[string]*DeveloperTemplate),
			fileEngine:  GetEngine(), // ফাইল ইঞ্জিন ব্যবহার করবে
			functions:   GetEngine().GetFunctions(), // একই functions ব্যবহার করবে
			templateDir: "templates",
		}
		
		// ক্যাশ ক্লিনআপ রুটিন
		go manager.cleanupCache()
		
		// ডাটাবেজ থেকে টেমপ্লেট লোড
		go manager.loadTemplatesFromDB()
	})
	
	return manager
}

// SaveDeveloperTemplate ডেভেলপারের টেমপ্লেট ডাটাবেজে সেভ করে
func (m *TemplateManager) SaveDeveloperTemplate(devID, action, htmlContent, subject string) error {
	ctx := context.Background()
	
	// HTML কন্টেন্ট ভ্যালিডেট করুন
	if err := m.validateHTML(htmlContent); err != nil {
		return err
	}
	
	// আগের টেমপ্লেট চেক করুন
	var existingID string
	err := database.PostgresPool.QueryRow(ctx,
		"SELECT id FROM developer_templates WHERE developer_id = $1 AND action = $2",
		devID, action).Scan(&existingID)
	
	var query string
	var args []interface{}
	
	if err == nil {
		// আপডেট
		query = `UPDATE developer_templates 
		         SET html_content = $1, subject = $2, version = version + 1, 
		             updated_at = NOW(), is_active = true
		         WHERE developer_id = $3 AND action = $4
		         RETURNING id`
		args = []interface{}{htmlContent, subject, devID, action}
	} else {
		// নতুন ইনসার্ট
		id := uuid.New().String()
		query = `INSERT INTO developer_templates 
		         (id, developer_id, action, html_content, subject, version, created_at, updated_at)
		         VALUES ($1, $2, $3, $4, $5, 1, NOW(), NOW())
		         RETURNING id`
		args = []interface{}{id, devID, action, htmlContent, subject}
	}
	
	var id string
	err = database.PostgresPool.QueryRow(ctx, query, args...).Scan(&id)
	if err != nil {
		return fmt.Errorf("failed to save template: %v", err)
	}
	
	// ক্যাশ আপডেট করুন
	m.mu.Lock()
	delete(m.dbCache, m.cacheKey(devID, action))
	m.mu.Unlock()
	
	monitoring.Info("Template saved for developer %s: %s", devID, action)
	return nil
}

// GetTemplate টেমপ্লেট খুঁজে বের করে (প্রথমে ডাটাবেজ, তারপর ফাইল সিস্টেম)
func (m *TemplateManager) GetTemplate(devID, action string) (*template.Template, string, error) {
	// প্রথমে ডাটাবেজ চেক করুন
	if devID != "" {
		tmpl, subject, err := m.getFromDB(devID, action)
		if err == nil && tmpl != nil {
			return tmpl, subject, nil
		}
	}
	
	// তারপর ফাইল সিস্টেম চেক করুন
	return m.getFromFile(action)
}

// getFromDB ডাটাবেজ থেকে টেমপ্লেট নেয়
func (m *TemplateManager) getFromDB(devID, action string) (*template.Template, string, error) {
	cacheKey := m.cacheKey(devID, action)
	
	// ক্যাশ চেক করুন
	m.mu.RLock()
	cached, exists := m.dbCache[cacheKey]
	m.mu.RUnlock()
	
	if exists && cached.IsActive {
		// ক্যাশ থেকে টেমপ্লেট তৈরি করুন
		tmpl, err := template.New(action).Funcs(m.functions).Parse(cached.HTMLContent)
		if err != nil {
			return nil, "", err
		}
		return tmpl, cached.Subject, nil
	}
	
	// ডাটাবেজ থেকে পড়ুন
	ctx := context.Background()
	var devTemplate DeveloperTemplate
	
	err := database.PostgresPool.QueryRow(ctx,
		`SELECT id, developer_id, action, html_content, subject, version, is_active 
		 FROM developer_templates 
		 WHERE developer_id = $1 AND action = $2 AND is_active = true`,
		devID, action).Scan(
		&devTemplate.ID, &devTemplate.DeveloperID, &devTemplate.Action,
		&devTemplate.HTMLContent, &devTemplate.Subject, &devTemplate.Version,
		&devTemplate.IsActive)
	
	if err != nil {
		return nil, "", err
	}
	
	// ক্যাশে রাখুন
	m.mu.Lock()
	m.dbCache[cacheKey] = &devTemplate
	m.mu.Unlock()
	
	// টেমপ্লেট তৈরি করুন
	tmpl, err := template.New(action).Funcs(m.functions).Parse(devTemplate.HTMLContent)
	if err != nil {
		return nil, "", err
	}
	
	return tmpl, devTemplate.Subject, nil
}

// getFromFile ফাইল সিস্টেম থেকে টেমপ্লেট নেয় (fileEngine ব্যবহার করে)
func (m *TemplateManager) getFromFile(action string) (*template.Template, string, error) {
	// ফাইল ক্যাশ চেক করুন
	m.mu.RLock()
	cached, exists := m.fileCache[action]
	m.mu.RUnlock()
	
	if exists {
		cached.LastUsed = time.Now()
		return cached.Template, getDefaultSubject(action), nil
	}
	
	// ফাইল ইঞ্জিন থেকে টেমপ্লেট নিন
	fileTmpl, err := m.fileEngine.GetFileTemplate(action)
	if err != nil {
		return nil, "", fmt.Errorf("template not found: %s", action)
	}
	
	// ফাইল হ্যাশ ক্যালকুলেট করুন
	filePath := filepath.Join(m.templateDir, action+".html")
	content, _ := os.ReadFile(filePath)
	hash := md5.Sum(content)
	hashStr := hex.EncodeToString(hash[:])
	
	// ক্যাশে রাখুন
	m.mu.Lock()
	m.fileCache[action] = &FileCacheEntry{
		Template: fileTmpl,
		Hash:     hashStr,
		LastUsed: time.Now(),
		IsFile:   true,
	}
	m.mu.Unlock()
	
	return fileTmpl, getDefaultSubject(action), nil
}

// RenderTemplate টেমপ্লেট রেন্ডার করে ডাটা সহ (পাবলিক API)
func (m *TemplateManager) RenderTemplate(devID, action string, data map[string]interface{}) (subject, htmlBody string, err error) {
	tmpl, subject, err := m.GetTemplate(devID, action)
	if err != nil {
		return "", "", err
	}
	
	// ডাটা প্রস্তুত করুন
	templateData := m.prepareData(data)
	
	// রেন্ডার করুন
	var buf strings.Builder
	err = tmpl.Execute(&buf, templateData)
	if err != nil {
		return "", "", err
	}
	
	return subject, buf.String(), nil
}

// ProcessTemplate - ইউনিফাইড টেমপ্লেট প্রসেসিং ফাংশন (সহজ ব্যবহারের জন্য)
func ProcessTemplate(devID, action string, data map[string]interface{}) (subject, htmlBody string, err error) {
	manager := GetManager()
	return manager.RenderTemplate(devID, action, data)
}

// ListDeveloperTemplates ডেভেলপারের সব টেমপ্লেটের তালিকা দেয়
func (m *TemplateManager) ListDeveloperTemplates(devID string) ([]map[string]interface{}, error) {
	ctx := context.Background()
	
	rows, err := database.PostgresPool.Query(ctx,
		`SELECT action, subject, version, created_at, updated_at 
		 FROM developer_templates 
		 WHERE developer_id = $1 AND is_active = true
		 ORDER BY updated_at DESC`,
		devID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var templates []map[string]interface{}
	for rows.Next() {
		var action, subject string
		var version int
		var createdAt, updatedAt time.Time
		
		err := rows.Scan(&action, &subject, &version, &createdAt, &updatedAt)
		if err != nil {
			continue
		}
		
		templates = append(templates, map[string]interface{}{
			"action":     action,
			"subject":    subject,
			"version":    version,
			"created_at": createdAt,
			"updated_at": updatedAt,
		})
	}
	
	return templates, nil
}

// DeleteDeveloperTemplate টেমপ্লেট ডিলিট করে (সফট ডিলিট)
func (m *TemplateManager) DeleteDeveloperTemplate(devID, action string) error {
	ctx := context.Background()
	
	_, err := database.PostgresPool.Exec(ctx,
		`UPDATE developer_templates 
		 SET is_active = false, updated_at = NOW()
		 WHERE developer_id = $1 AND action = $2`,
		devID, action)
	
	if err == nil {
		// ক্যাশ থেকে মুছুন
		m.mu.Lock()
		delete(m.dbCache, m.cacheKey(devID, action))
		m.mu.Unlock()
	}
	
	return err
}

// prepareData ডাটা প্রস্তুত করে
func (m *TemplateManager) prepareData(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	
	// ইউজার ডাটা
	if user, ok := data["user"].(map[string]interface{}); ok {
		result["User"] = user
		delete(data, "user")
	}
	
	// কাস্টম ডাটা
	result["Custom"] = data
	
	// সিস্টেম ডাটা
	now := time.Now()
	result["Year"] = now.Year()
	result["Timestamp"] = now
	result["Now"] = now
	
	return result
}

// validateHTML HTML কন্টেন্ট ভ্যালিডেট করে
func (m *TemplateManager) validateHTML(html string) error {
	// বেসিক সিকিউরিটি চেক
	if strings.Contains(html, "<script>") {
		return fmt.Errorf("script tags are not allowed")
	}
	
	if strings.Contains(html, "javascript:") {
		return fmt.Errorf("javascript: protocol is not allowed")
	}
	
	// সাইজ চেক (৫০০KB এর বেশি না)
	if len(html) > 500*1024 {
		return fmt.Errorf("template too large (max 500KB)")
	}
	
	return nil
}

func (m *TemplateManager) cacheKey(devID, action string) string {
	return fmt.Sprintf("%s:%s", devID, action)
}

// cleanupCache periodically cleans old cache entries
func (m *TemplateManager) cleanupCache() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	
	for range ticker.C {
		m.mu.Lock()
		
		// পুরনো ফাইল ক্যাশ পরিষ্কার করুন (২৪ ঘন্টা না ব্যবহার হলে)
		for name, cached := range m.fileCache {
			if time.Since(cached.LastUsed) > 24*time.Hour {
				delete(m.fileCache, name)
			}
		}
		
		// ডাটাবেজ ক্যাশ রিফ্রেশ করুন
		m.dbCache = make(map[string]*DeveloperTemplate)
		m.mu.Unlock()
		
		// নতুন করে ডাটাবেজ থেকে লোড করুন
		go m.loadTemplatesFromDB()
	}
}

// loadTemplatesFromDB ডাটাবেজ থেকে সব টেমপ্লেট লোড করে
func (m *TemplateManager) loadTemplatesFromDB() {
	ctx := context.Background()
	
	rows, err := database.PostgresPool.Query(ctx,
		`SELECT developer_id, action, html_content, subject, version 
		 FROM developer_templates 
		 WHERE is_active = true`)
	if err != nil {
		monitoring.Error("Failed to load templates from DB: %v", err)
		return
	}
	defer rows.Close()
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	count := 0
	for rows.Next() {
		var devID, action, htmlContent, subject string
		var version int
		
		err := rows.Scan(&devID, &action, &htmlContent, &subject, &version)
		if err != nil {
			continue
		}
		
		m.dbCache[m.cacheKey(devID, action)] = &DeveloperTemplate{
			DeveloperID: devID,
			Action:      action,
			HTMLContent: htmlContent,
			Subject:     subject,
			Version:     version,
			IsActive:    true,
		}
		count++
	}
	
	monitoring.Info("Loaded %d templates from database", count)
}

// GetDefaultSubject ডিফল্ট সাবজেক্ট রিটার্ন করে (exported)
func GetDefaultSubject(action string) string {
	return getDefaultSubject(action)
}