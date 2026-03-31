package security

import (
 "encoding/Json"
 "regexp"
	"strings"
	"time"
	"unicode"
)

type Validator struct {
	Errors map[string]string
}

func NewValidator() *Validator {
	return &Validator{
		Errors: make(map[string]string),
	}
}

func (v *Validator) IsValid() bool {
	return len(v.Errors) == 0
}

func (v *Validator) AddError(field, message string) {
	if _, exists := v.Errors[field]; !exists {
		v.Errors[field] = message
	}
}

// Required validation
func (v *Validator) Required(value string, field string) {
	if strings.TrimSpace(value) == "" {
		v.AddError(field, "This field is required")
	}
}

// Email validation
func (v *Validator) Email(email string) {
	if !ValidateEmail(email) {
		v.AddError("email", "Invalid email address")
	}
}

// Min length validation
func (v *Validator) MinLength(value string, min int, field string) {
	if len(strings.TrimSpace(value)) < min {
		v.AddError(field, "Minimum length is "+string(min))
	}
}

// Max length validation
func (v *Validator) MaxLength(value string, max int, field string) {
	if len(strings.TrimSpace(value)) > max {
		v.AddError(field, "Maximum length is "+string(max))
	}
}

// Pattern validation
func (v *Validator) Matches(value, pattern, field string) {
	matched, _ := regexp.MatchString(pattern, value)
	if !matched {
		v.AddError(field, "Invalid format")
	}
}

// Numeric validation
func (v *Validator) IsNumeric(value string, field string) {
	for _, r := range value {
		if !unicode.IsDigit(r) {
			v.AddError(field, "Must be numeric")
			return
		}
	}
}

// Date validation
func (v *Validator) IsValidDate(value string, layout string, field string) {
	_, err := time.Parse(layout, value)
	if err != nil {
		v.AddError(field, "Invalid date format")
	}
}

// Future date validation
func (v *Validator) IsFutureDate(value string, layout string, field string) {
	t, err := time.Parse(layout, value)
	if err != nil {
		v.AddError(field, "Invalid date format")
		return
	}
	if t.Before(time.Now()) {
		v.AddError(field, "Date must be in the future")
	}
}

// JSON validation
func (v *Validator) IsValidJSON(value string, field string) {
	var js map[string]interface{}
	if err := json.Unmarshal([]byte(value), &js); err != nil {
		v.AddError(field, "Invalid JSON")
	}
}

// One of validation
func (v *Validator) OneOf(value string, options []string, field string) {
	for _, opt := range options {
		if value == opt {
			return
		}
	}
	v.AddError(field, "Must be one of: "+strings.Join(options, ", "))
}

// URL validation
func (v *Validator) IsValidURL(url string) bool {
	pattern := `^(https?:\/\/)?([\da-z\.-]+)\.([a-z\.]{2,6})([\/\w \.-]*)*\/?$`
	matched, _ := regexp.MatchString(pattern, url)
	return matched
}

// Phone validation
func (v *Validator) IsValidPhone(phone string) bool {
	pattern := `^\+?[1-9]\d{1,14}$`
	matched, _ := regexp.MatchString(pattern, phone)
	return matched
}

// Password strength validation
func (v *Validator) IsStrongPassword(password string) bool {
	var (
		hasUpper   = false
		hasLower   = false
		hasNumber  = false
		hasSpecial = false
	)

	if len(password) < 8 {
		return false
	}

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	return hasUpper && hasLower && hasNumber && hasSpecial
}