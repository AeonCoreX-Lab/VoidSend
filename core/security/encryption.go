package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/AeonCoreX-Lab/VoidSend/monitoring"
	"golang.org/x/crypto/bcrypt"
)

var (
	encryptionKey []byte
	jwtSecret     []byte
)

type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func InitSecurity(encKey, jtwKey string) error {
	// Generate key from passphrase
	hash := sha256.Sum256([]byte(encKey))
	encryptionKey = hash[:]

	hash = sha256.Sum256([]byte(jtwKey))
	jwtSecret = hash[:]

	monitoring.Info("Security initialized")
	return nil
}

// Encryption
func Encrypt(plaintext string) (string, error) {
	if encryptionKey == nil {
		return "", errors.New("encryption not initialized")
	}

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}

	// Generate nonce
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	ciphertext := aesgcm.Seal(nil, nonce, []byte(plaintext), nil)

	// Combine nonce and ciphertext
	result := make([]byte, len(nonce)+len(ciphertext))
	copy(result, nonce)
	copy(result[len(nonce):], ciphertext)

	return base64.StdEncoding.EncodeToString(result), nil
}

func Decrypt(encrypted string) (string, error) {
	if encryptionKey == nil {
		return "", errors.New("encryption not initialized")
	}

	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}

	if len(data) < 12 {
		return "", errors.New("invalid ciphertext")
	}

	nonce := data[:12]
	ciphertext := data[12:]

	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return "", err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// JWT Tokens
func GenerateToken(userID, email, role string) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "VoidSend",
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// Password Hashing
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// API Key Generation
func GenerateAPIKey() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// Email Validation
func ValidateEmail(email string) bool {
	// Basic email validation
	if len(email) < 3 || len(email) > 254 {
		return false
	}
	
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	
	if len(parts[0]) < 1 || len(parts[1]) < 3 {
		return false
	}
	
	if !strings.Contains(parts[1], ".") {
		return false
	}
	
	return true
}

// SQL Injection Prevention
func SanitizeInput(input string) string {
	// Basic sanitization
	replacer := strings.NewReplacer(
		"'", "''",
		"--", "",
		";", "",
		"/*", "",
		"*/", "",
	)
	return replacer.Replace(input)
}

// XSS Prevention
func EscapeHTML(input string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(input)
}

// CSRF Token
func GenerateCSRFToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return base64.StdEncoding.EncodeToString(bytes)
}

// Mask Email (for logs)
func MaskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "invalid@email"
	}
	
	local := parts[0]
	if len(local) > 3 {
		local = local[:3] + "***"
	} else {
		local = local + "***"
	}
	
	return local + "@" + parts[1]
}