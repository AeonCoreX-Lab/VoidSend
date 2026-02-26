package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"sync"
	"time"

	"github.com/VoidSend/core/database"
	"github.com/VoidSend/monitoring"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ses"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

var (
	smtpPool     *SMTPPool
	sendGridCli  *sendgrid.Client
	sesClient    *ses.SES
	providersMu  sync.RWMutex
)

type SMTPPool struct {
	connections chan *smtp.Client
	config      *SMTPConfig
	mu          sync.Mutex
}

type SMTPConfig struct {
	Host       string
	Port       int
	User       string
	Pass       string
	FromName   string
	FromEmail  string
	Encryption string
	PoolSize   int
}

func InitSMTPPool(config *SMTPConfig) error {
	providersMu.Lock()
	defer providersMu.Unlock()
	
	pool := &SMTPPool{
		connections: make(chan *smtp.Client, config.PoolSize),
		config:      config,
	}
	
	// Create initial connections
	for i := 0; i < config.PoolSize; i++ {
		client, err := pool.createConnection()
		if err != nil {
			return err
		}
		pool.connections <- client
	}
	
	smtpPool = pool
	monitoring.Info("SMTP pool initialized with %d connections", config.PoolSize)
	return nil
}

func (p *SMTPPool) createConnection() (*smtp.Client, error) {
	addr := fmt.Sprintf("%s:%d", p.config.Host, p.config.Port)
	
	var conn net.Conn
	var err error
	
	switch p.config.Encryption {
	case "TLS":
		conn, err = tls.Dial("tcp", addr, &tls.Config{
			ServerName: p.config.Host,
		})
	case "SSL":
		conn, err = tls.Dial("tcp", addr, &tls.Config{
			ServerName: p.config.Host,
		})
	default: // STARTTLS
		conn, err = net.Dial("tcp", addr)
	}
	
	if err != nil {
		return nil, err
	}
	
	client, err := smtp.NewClient(conn, p.config.Host)
	if err != nil {
		conn.Close()
		return nil, err
	}
	
	// Auth
	auth := smtp.PlainAuth("", p.config.User, p.config.Pass, p.config.Host)
	if err = client.Auth(auth); err != nil {
		client.Close()
		return nil, err
	}
	
	return client, nil
}

func SendViaSMTP(to, subject, htmlBody, textBody string) (string, error) {
	providersMu.RLock()
	pool := smtpPool
	providersMu.RUnlock()
	
	if pool == nil {
		return "", fmt.Errorf("SMTP pool not initialized")
	}
	
	// Get connection from pool
	var client *smtp.Client
	select {
	case client = <-pool.connections:
	default:
		// Create new connection if pool empty
		var err error
		client, err = pool.createConnection()
		if err != nil {
			return "", err
		}
	}
	
	defer func() {
		// Return to pool or close
		select {
		case pool.connections <- client:
		default:
			client.Close()
		}
	}()
	
	// Set sender and recipient
	if err := client.Mail(pool.config.FromEmail); err != nil {
		return "", err
	}
	
	if err := client.Rcpt(to); err != nil {
		return "", err
	}
	
	// Send data
	wc, err := client.Data()
	if err != nil {
		return "", err
	}
	defer wc.Close()
	
	// Build message
	messageID := generateMessageID()
	
	buf := bytes.NewBuffer(nil)
	buf.WriteString(fmt.Sprintf("From: %s <%s>\r\n", pool.config.FromName, pool.config.FromEmail))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	buf.WriteString(fmt.Sprintf("Message-ID: %s\r\n", messageID))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: multipart/alternative; boundary=boundary123\r\n")
	buf.WriteString("\r\n")
	buf.WriteString("--boundary123\r\n")
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(textBody)
	buf.WriteString("\r\n")
	buf.WriteString("--boundary123\r\n")
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(htmlBody)
	buf.WriteString("\r\n")
	buf.WriteString("--boundary123--")
	
	_, err = wc.Write(buf.Bytes())
	if err != nil {
		return "", err
	}
	
	return messageID, nil
}

func InitSendGrid(config *SendGridConfig) {
	providersMu.Lock()
	defer providersMu.Unlock()
	
	sendGridCli = sendgrid.NewSendClient(config.APIKey)
	monitoring.Info("SendGrid initialized")
}

func SendViaSendGrid(to, subject, htmlBody, textBody string) (string, error) {
	providersMu.RLock()
	cli := sendGridCli
	from := mail.NewEmail("VoidSend", "noreply@voidsend.com")
	providersMu.RUnlock()
	
	if cli == nil {
		return "", fmt.Errorf("SendGrid not initialized")
	}
	
	toEmail := mail.NewEmail("", to)
	message := mail.NewSingleEmail(from, subject, toEmail, textBody, htmlBody)
	
	response, err := cli.Send(message)
	if err != nil {
		return "", err
	}
	
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("SendGrid error: %s", response.Body)
	}
	
	return response.Headers["X-Message-Id"], nil
}

func InitAWSSES(config *AWSConfig) error {
	providersMu.Lock()
	defer providersMu.Unlock()
	
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(config.Region),
		Credentials: credentials.NewStaticCredentials(config.AccessKey, config.SecretKey, ""),
	})
	
	if err != nil {
		return err
	}
	
	sesClient = ses.New(sess)
	monitoring.Info("AWS SES initialized")
	return nil
}

func SendViaSES(to, subject, htmlBody, textBody string) (string, error) {
	providersMu.RLock()
	cli := sesClient
	providersMu.RUnlock()
	
	if cli == nil {
		return "", fmt.Errorf("AWS SES not initialized")
	}
	
	input := &ses.SendEmailInput{
		Destination: &ses.Destination{
			ToAddresses: []*string{aws.String(to)},
		},
		Message: &ses.Message{
			Body: &ses.Body{
				Html: &ses.Content{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(htmlBody),
				},
				Text: &ses.Content{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(textBody),
				},
			},
			Subject: &ses.Content{
				Charset: aws.String("UTF-8"),
				Data:    aws.String(subject),
			},
		},
		Source: aws.String("VoidSend <noreply@voidsend.com>"),
	}
	
	result, err := cli.SendEmail(input)
	if err != nil {
		return "", err
	}
	
	return *result.MessageId, nil
}

func SelectOptimalProvider(to string) string {
	// Intelligent provider selection based on:
	// - Domain reputation
	// - Current load
	// - Success rates
	// - Geographic location
	
	// For now, simple round-robin or fallback logic
	providers := []string{"smtp", "sendgrid", "ses"}
	
	// Check Redis for provider status
	ctx := context.Background()
	for _, provider := range providers {
		status, err := database.RedisClient.Get(ctx, "provider:"+provider+":status").Result()
		if err == nil && status == "healthy" {
			return provider
		}
	}
	
	return "smtp" // Default
}

func SendViaFailover(to, subject, htmlBody, textBody string) (string, error) {
	// Try providers in order until one succeeds
	providers := []struct {
		name string
		fn   func(string, string, string, string) (string, error)
	}{
		{"ses", SendViaSES},
		{"sendgrid", SendViaSendGrid},
		{"smtp", SendViaSMTP},
	}
	
	var lastErr error
	for _, p := range providers {
		msgID, err := p.fn(to, subject, htmlBody, textBody)
		if err == nil {
			monitoring.Info("Failover succeeded with %s", p.name)
			return msgID, nil
		}
		lastErr = err
	}
	
	return "", fmt.Errorf("all providers failed: %v", lastErr)
}

func FetchUserEmail(userID string) (string, error) {
	// Try cache first
	ctx := context.Background()
	cached, err := database.RedisClient.Get(ctx, "user:"+userID+":email").Result()
	if err == nil {
		return cached, nil
	}
	
	// Fetch from PostgreSQL
	var email string
	err = database.PostgresPool.QueryRow(ctx,
		"SELECT email FROM users WHERE id = $1", userID).Scan(&email)
	
	if err != nil {
		return "", err
	}
	
	// Cache for 1 hour
	database.RedisClient.Set(ctx, "user:"+userID+":email", email, time.Hour)
	
	return email, nil
}

func generateMessageID() string {
	return fmt.Sprintf("<%d.%d@voidsend.com>", 
		time.Now().UnixNano(), 
		time.Now().Unix())
}