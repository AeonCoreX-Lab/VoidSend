package template

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
 "sync"
	"time"
)

// TemplateVariable টেমপ্লেটে ব্যবহারযোগ্য ভেরিয়েবলের বিবরণ
type TemplateVariable struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Default     interface{} `json:"default,omitempty"`
	Example     interface{} `json:"example,omitempty"`
}

// TemplateFunction টেমপ্লেট ফাংশনের বিবরণ
type TemplateFunction struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Parameters  []string `json:"parameters"`
	Example     string   `json:"example"`
}

// VariableRegistry সব ভেরিয়েবল এবং ফাংশনের রেজিস্ট্রি
type VariableRegistry struct {
	variables map[string]TemplateVariable
	functions map[string]TemplateFunction
}

var (
	registry     *VariableRegistry
	registryOnce sync.Once
)

// GetRegistry returns the singleton VariableRegistry
func GetRegistry() *VariableRegistry {
	registryOnce.Do(func() {
		registry = &VariableRegistry{
			variables: make(map[string]TemplateVariable),
			functions: make(map[string]TemplateFunction),
		}
		registry.registerDefaults()
	})
	return registry
}

// registerDefaults ডিফল্ট ভেরিয়েবল এবং ফাংশন রেজিস্টার করে
func (r *VariableRegistry) registerDefaults() {
	// System Variables
	r.variables["Year"] = TemplateVariable{
		Name:        "Year",
		Type:        "int",
		Description: "বর্তমান বছর (যেমন: 2024)",
		Example:     2024,
	}

	r.variables["Timestamp"] = TemplateVariable{
		Name:        "Timestamp",
		Type:        "time.Time",
		Description: "বর্তমান সময়",
		Example:     time.Now(),
	}

	r.variables["Now"] = TemplateVariable{
		Name:        "Now",
		Type:        "time.Time",
		Description: "বর্তমান সময় (Timestamp-এর alias)",
		Example:     time.Now(),
	}

	// User Variables
	r.variables["User.id"] = TemplateVariable{
		Name:        "User.id",
		Type:        "string",
		Description: "ইউজারের আইডি",
		Example:     "user_12345",
	}

	r.variables["User.name"] = TemplateVariable{
		Name:        "User.name",
		Type:        "string",
		Description: "ইউজারের নাম",
		Example:     "রহিম",
	}

	r.variables["User.email"] = TemplateVariable{
		Name:        "User.email",
		Type:        "string",
		Description: "ইউজারের ইমেইল",
		Example:     "rahim@example.com",
	}

	// App Variables
	r.variables["App.name"] = TemplateVariable{
		Name:        "App.name",
		Type:        "string",
		Description: "অ্যাপ্লিকেশনের নাম",
		Example:     "MyAwesomeApp",
	}

	r.variables["App.version"] = TemplateVariable{
		Name:        "App.version",
		Type:        "string",
		Description: "অ্যাপ্লিকেশনের ভার্সন",
		Example:     "2.1.0",
	}

	// Custom Variables (examples)
	r.variables["Custom.otp"] = TemplateVariable{
		Name:        "Custom.otp",
		Type:        "string",
		Description: "OTP কোড",
		Required:    true,
		Example:     "123456",
	}

	r.variables["Custom.link"] = TemplateVariable{
		Name:        "Custom.link",
		Type:        "string",
		Description: "রিসেট/ভেরিফিকেশন লিংক",
		Example:     "https://example.com/reset?token=abc123",
	}

	r.variables["Custom.message"] = TemplateVariable{
		Name:        "Custom.message",
		Type:        "string",
		Description: "কাস্টম মেসেজ",
		Example:     "আপনার অর্ডার কনফার্ম হয়েছে",
	}

	// Register functions
	r.functions["upper"] = TemplateFunction{
		Name:        "upper",
		Description: "স্ট্রিংকে uppercase এ রূপান্তর করে",
		Parameters:  []string{"string"},
		Example:     `{{ "hello" | upper }} -> "HELLO"`,
	}

	r.functions["lower"] = TemplateFunction{
		Name:        "lower",
		Description: "স্ট্রিংকে lowercase এ রূপান্তর করে",
		Parameters:  []string{"string"},
		Example:     `{{ "HELLO" | lower }} -> "hello"`,
	}

	r.functions["title"] = TemplateFunction{
		Name:        "title",
		Description: "প্রতিটি শব্দের প্রথম অক্ষর capital করে",
		Parameters:  []string{"string"},
		Example:     `{{ "hello world" | title }} -> "Hello World"`,
	}

	r.functions["default"] = TemplateFunction{
		Name:        "default",
		Description: "ভেরিয়েবল empty/nil হলে ডিফল্ট ভ্যালু ব্যবহার করে",
		Parameters:  []string{"default", "value"},
		Example:     `{{ .Custom.message | default "No message" }}`,
	}

	r.functions["formatDate"] = TemplateFunction{
		Name:        "formatDate",
		Description: "টাইমস্ট্যাম্পকে নির্দিষ্ট ফরম্যাটে রূপান্তর করে",
		Parameters:  []string{"time", "layout"},
		Example:     `{{ .Timestamp | formatDate "2006-01-02" }}`,
	}

	r.functions["now"] = TemplateFunction{
		Name:        "now",
		Description: "বর্তমান সময় রিটার্ন করে",
		Parameters:  []string{},
		Example:     `{{ now }}`,
	}

	r.functions["add"] = TemplateFunction{
		Name:        "add",
		Description: "দুটি সংখ্যা যোগ করে",
		Parameters:  []string{"a", "b"},
		Example:     `{{ add 5 3 }} -> 8`,
	}

	r.functions["sub"] = TemplateFunction{
		Name:        "sub",
		Description: "একটি সংখ্যা থেকে অন্যটি বিয়োগ করে",
		Parameters:  []string{"a", "b"},
		Example:     `{{ sub 5 3 }} -> 2`,
	}

	r.functions["mul"] = TemplateFunction{
		Name:        "mul",
		Description: "দুটি সংখ্যা গুণ করে",
		Parameters:  []string{"a", "b"},
		Example:     `{{ mul 5 3 }} -> 15`,
	}

	r.functions["div"] = TemplateFunction{
		Name:        "div",
		Description: "একটি সংখ্যাকে অন্যটি দিয়ে ভাগ করে",
		Parameters:  []string{"a", "b"},
		Example:     `{{ div 6 3 }} -> 2`,
	}

	r.functions["join"] = TemplateFunction{
		Name:        "join",
		Description: "স্ট্রিং slice-কে join করে",
		Parameters:  []string{"slice", "separator"},
		Example:     `{{ join .Custom.items "," }}`,
	}

	r.functions["split"] = TemplateFunction{
		Name:        "split",
		Description: "স্ট্রিংকে separator দিয়ে split করে",
		Parameters:  []string{"string", "separator"},
		Example:     `{{ split "a,b,c" "," }}`,
	}

	r.functions["replace"] = TemplateFunction{
		Name:        "replace",
		Description: "স্ট্রিং-এর অংশবিশেষ replace করে",
		Parameters:  []string{"string", "old", "new"},
		Example:     `{{ replace "Hello World" "World" "Bangladesh" }}`,
	}

	r.functions["json"] = TemplateFunction{
		Name:        "json",
		Description: "অবজেক্টকে JSON স্ট্রিং-এ রূপান্তর করে",
		Parameters:  []string{"value"},
		Example:     `{{ json .Custom }}`,
	}

	r.functions["dict"] = TemplateFunction{
		Name:        "dict",
		Description: "ডিকশনারি তৈরি করে",
		Parameters:  []string{"key1", "value1", "key2", "value2", "..."},
		Example:     `{{ dict "name" "রহিম" "age" 25 }}`,
	}
}

// GetVariable ভেরিয়েবলের বিবরণ দেয়
func (r *VariableRegistry) GetVariable(name string) (TemplateVariable, bool) {
	v, ok := r.variables[name]
	return v, ok
}

// GetFunction ফাংশনের বিবরণ দেয়
func (r *VariableRegistry) GetFunction(name string) (TemplateFunction, bool) {
	f, ok := r.functions[name]
	return f, ok
}

// ListVariables সব ভেরিয়েবলের তালিকা দেয়
func (r *VariableRegistry) ListVariables() []TemplateVariable {
	var vars []TemplateVariable
	for _, v := range r.variables {
		vars = append(vars, v)
	}
	return vars
}

// ListFunctions সব ফাংশনের তালিকা দেয়
func (r *VariableRegistry) ListFunctions() []TemplateFunction {
	var funcs []TemplateFunction
	for _, f := range r.functions {
		funcs = append(funcs, f)
	}
	return funcs
}

// GetVariablesByPrefix prefix দিয়ে ভেরিয়েবল ফিল্টার করে
func (r *VariableRegistry) GetVariablesByPrefix(prefix string) []TemplateVariable {
	var vars []TemplateVariable
	for name, v := range r.variables {
		if strings.HasPrefix(name, prefix) {
			v.Name = name
			vars = append(vars, v)
		}
	}
	return vars
}

// ValidateVariable ভেরিয়েবলের মান বৈধ কিনা চেক করে
func (r *VariableRegistry) ValidateVariable(name string, value interface{}) error {
	v, ok := r.variables[name]
	if !ok {
		return fmt.Errorf("unknown variable: %s", name)
	}

	// Check required
	if v.Required && value == nil {
		return fmt.Errorf("required variable missing: %s", name)
	}

	// Type validation
	if value != nil {
		expectedType := r.getGoType(v.Type)
		actualType := reflect.TypeOf(value)
		
		if expectedType != actualType && actualType != nil {
			return fmt.Errorf("variable %s expected type %s, got %s", 
				name, v.Type, actualType.String())
		}
	}

	return nil
}

// ValidateTemplateData সব ভেরিয়েবল ভ্যালিডেট করে
func (r *VariableRegistry) ValidateTemplateData(data map[string]interface{}) error {
	// Flatten nested data
	flatData := r.flattenMap("", data)

	for name, v := range r.variables {
		if v.Required {
			if _, ok := flatData[name]; !ok {
				// Check in nested structures
				if !r.checkNestedRequired(name, data) {
					return fmt.Errorf("required variable missing: %s", name)
				}
			}
		}
	}

	return nil
}

// Helper methods

func (r *VariableRegistry) getGoType(typeStr string) reflect.Type {
	switch typeStr {
	case "string":
		return reflect.TypeOf("")
	case "int":
		return reflect.TypeOf(0)
	case "bool":
		return reflect.TypeOf(false)
	case "time.Time":
		return reflect.TypeOf(time.Time{})
	case "map":
		return reflect.TypeOf(map[string]interface{}{})
	case "slice":
		return reflect.TypeOf([]interface{}{})
	default:
		return nil
	}
}

func (r *VariableRegistry) flattenMap(prefix string, m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		
		if nested, ok := v.(map[string]interface{}); ok {
			// Recursively flatten nested maps
			nestedResult := r.flattenMap(key, nested)
			for nk, nv := range nestedResult {
				result[nk] = nv
			}
		} else {
			result[key] = v
		}
	}
	
	return result
}

func (r *VariableRegistry) checkNestedRequired(path string, data map[string]interface{}) bool {
	parts := strings.Split(path, ".")
	
	current := data
	for i, part := range parts {
		if i == len(parts)-1 {
			_, ok := current[part]
			return ok
		}
		
		next, ok := current[part].(map[string]interface{})
		if !ok {
			return false
		}
		current = next
	}
	
	return false
}

// Template Variable Helper

// ExtractVariables টেমপ্লেট থেকে ভেরিয়েবল এক্সট্র্যাক্ট করে
func ExtractVariables(templateContent string) []string {
	// Find {{ .Variable }} patterns
	re := regexp.MustCompile(`{{[ ]*\.[a-zA-Z0-9_.]+[ ]*}}`)
	matches := re.FindAllString(templateContent, -1)
	
	// Clean up the matches
	var vars []string
	seen := make(map[string]bool)
	
	for _, match := range matches {
		// Remove {{ and }} and trim spaces
		varName := strings.TrimSpace(match[2 : len(match)-2])
		varName = strings.TrimPrefix(varName, ".")
		
		if !seen[varName] {
			seen[varName] = true
			vars = append(vars, varName)
		}
	}
	
	return vars
}

// GenerateTemplateDoc টেমপ্লেট ডকুমেন্টেশন জেনারেট করে
func GenerateTemplateDoc() string {
	reg := GetRegistry()
	
	var doc strings.Builder
	
	doc.WriteString("# 📝 Template Variables and Functions\n\n")
	
	// Variables section
	doc.WriteString("## 🔢 ভেরিয়েবলসমূহ\n\n")
	doc.WriteString("| নাম | টাইপ | বিবরণ | উদাহরণ |\n")
	doc.WriteString("|-----|------|--------|--------|\n")
	
	for _, v := range reg.ListVariables() {
		example := fmt.Sprintf("%v", v.Example)
		if len(example) > 30 {
			example = example[:27] + "..."
		}
		doc.WriteString(fmt.Sprintf("| `%s` | %s | %s | `%v` |\n", 
			v.Name, v.Type, v.Description, example))
	}
	
	// Functions section
	doc.WriteString("\n## ⚙️ ফাংশনসমূহ\n\n")
	doc.WriteString("| নাম | বিবরণ | উদাহরণ |\n")
	doc.WriteString("|-----|--------|--------|\n")
	
	for _, f := range reg.ListFunctions() {
		doc.WriteString(fmt.Sprintf("| `%s` | %s | `%s` |\n", 
			f.Name, f.Description, f.Example))
	}
	
	return doc.String()
}