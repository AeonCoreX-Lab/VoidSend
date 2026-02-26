package utils

import (
	"net"
	"regexp"
	"strings"
	"time"
)

// IsValidEmail checks if email format is valid
func IsValidEmail(email string) bool {
	// Simple regex, but in production use proper validation
	pattern := `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
	matched, _ := regexp.MatchString(pattern, email)
	return matched
}

// IsValidPhone validates phone number (basic)
func IsValidPhone(phone string) bool {
	// Basic: only digits, length 10-15
	pattern := `^\+?[0-9]{10,15}$`
	matched, _ := regexp.MatchString(pattern, phone)
	return matched
}

// IsValidIP checks if string is valid IP address
func IsValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// IsValidMAC validates MAC address
func IsValidMAC(mac string) bool {
	_, err := net.ParseMAC(mac)
	return err == nil
}

// IsValidUUID checks if string is valid UUID
func IsValidUUID(uuid string) bool {
	pattern := `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	matched, _ := regexp.MatchString(pattern, strings.ToLower(uuid))
	return matched
}

// IsValidJSON checks if string is valid JSON
func IsValidJSON(str string) bool {
	var js map[string]interface{}
	return json.Unmarshal([]byte(str), &js) == nil
}

// IsNumeric checks if string contains only digits
func IsNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// IsValidDate checks if string is valid date in given layout
func IsValidDate(dateStr, layout string) bool {
	_, err := time.Parse(layout, dateStr)
	return err == nil
}

// IsFutureDate checks if date is in future
func IsFutureDate(dateStr, layout string) bool {
	t, err := time.Parse(layout, dateStr)
	if err != nil {
		return false
	}
	return t.After(time.Now())
}

// IsAlpha checks if string contains only letters
func IsAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// IsAlphanumeric checks if string contains only letters and digits
func IsAlphanumeric(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// IsStrongPassword checks password strength
func IsStrongPassword(password string) bool {
	var (
		hasUpper   = false
		hasLower   = false
		hasNumber  = false
		hasSpecial = false
	)

	if len(password) < 8 {
		return false
	}

	for _, r := range password {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasNumber = true
		case r == '!' || r == '@' || r == '#' || r == '$' || r == '%' || r == '^' || r == '&' || r == '*' || r == '(' || r == ')' || r == '-' || r == '_' || r == '+' || r == '=':
			hasSpecial = true
		}
	}

	return hasUpper && hasLower && hasNumber && hasSpecial
}