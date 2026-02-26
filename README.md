<p align="center">
  <img src="assets/logo.png" width="180" alt="VoidSend Logo" />
</p>

<h1 align="center">VoidSend</h1>
<p align="center">
  Ultra-Scale Self-Hosted Email Dispatch Engine
</p>

<p align="center">

<img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go" />
<img src="https://img.shields.io/github/license/AeonCoreX-Lab/VoidSend?style=flat-square" />
<img src="https://img.shields.io/github/stars/AeonCoreX-Lab/VoidSend?style=flat-square" />
<img src="https://img.shields.io/github/issues/AeonCoreX-Lab/VoidSend?style=flat-square" />

<br/>

<a href="https://vercel.com">
  <img src="https://img.shields.io/badge/Deploy-Vercel-000000?style=flat-square&logo=vercel" />
</a>
<a href="https://render.com">
  <img src="https://img.shields.io/badge/Deploy-Render-46E3B7?style=flat-square&logo=render" />
</a>
<a href="https://railway.app">
  <img src="https://img.shields.io/badge/Deploy-Railway-0B0D0E?style=flat-square&logo=railway" />
</a>
<a href="https://www.docker.com/">
  <img src="https://img.shields.io/badge/Docker-Ready-2496ED?style=flat-square&logo=docker" />
</a>

</p>

---

## 📌 Overview

VoidSend is a high-performance, self-hosted email dispatch engine built in Go for developers who require:

- Full infrastructure control  
- Zero vendor lock-in  
- High concurrency  
- Enterprise-grade reliability  

Designed for SaaS platforms, fintech systems, automation tools, and authentication services that require predictable, scalable email delivery.

---

## ⚡ Core Features

### High Throughput Engine
- Configurable worker pool (1000+ workers)
- In-memory queue (50,000+ capacity)
- Retry system with exponential backoff
- Backpressure handling

### Multi-Provider Failover
Supports:
- SMTP (connection pooling)
- SendGrid
- AWS SES

Features:
- Automatic failover
- Provider health checks
- Circuit breaker logic

### Advanced Template Engine
- File-based templates
- Database-stored templates
- Dynamic variable injection
- Built-in helper functions
- Template caching
- Per-developer isolation

### Queue & Scheduling
- Priority queue
- Scheduled dispatch
- Batch sending
- Retry management

### Security Layer
- API key authentication
- Rate limiting (IP + developer level)
- AES-256 encryption for sensitive data
- IP allow-list support
- Structured audit logs

### Observability
- Prometheus metrics
- Structured JSON logging
- Queue monitoring
- Failure rate tracking
- Slack / PagerDuty alerts

---

## 🏗 Architecture

Client Apps ↓ API Layer (Gin) ↓ Dispatcher ↓ Worker Pool ↓ Mailer Abstraction ↓ SMTP / SES / SendGrid

Infrastructure:
- PostgreSQL → persistent storage
- Redis → caching & rate limiting

---

## 🚀 Deployment

### 1️⃣ Docker (Recommended)


docker run -d \
  --name voidsend \
  -p 8080:8080 \
  -e DATABASE_URL=postgresql://user:pass@host:5432/voidsend \
  -e REDIS_URL=redis://:password@host:6379 \
  ghcr.io/aeoncorex/voidsend:latest


---

## 2️⃣ Deploy on Render

Fork repository

Create Web Service

Build: go build -o voidsend main.go

Start: ./voidsend

Add DATABASE_URL and REDIS_URL



---

## 3️⃣ Deploy on Railway

Deploy from GitHub

Add environment variables

Auto-detect Go build



---

## 4️⃣ Deploy on Vercel (Light Usage)

Recommended for:

Low-volume email APIs

MVP usage


Note: Worker pools and background queues are limited in serverless environments.


---

⚙ Configuration (config.yaml)

server:
  port: "8080"
  mode: "release"

engine:
  max_workers: 1000
  queue_size: 50000
  retry_attempts: 3
  retry_delay: 5s

providers:
  primary: "failover"
  fallbacks: ["ses", "sendgrid", "smtp"]

database:
  postgres:
    url: "postgresql://user:pass@localhost:5432/voidsend?sslmode=disable"
  redis:
    url: "localhost:6379"
    password: ""
    db: 0

security:
  rate_limit: 1000
  rate_window: 1m
  encryption_key: "32-byte-secure-key"

monitoring:
  prometheus_enabled: true
  metrics_port: "9090"

Environment variables override YAML values.


---

## 📡 API Overview

Public

Method	Endpoint	Description

POST	/v1/signup	Create developer
GET	/health	Health check
GET	/metrics	Prometheus metrics



---

Protected (API Key Required)

Email Dispatch

Method	Endpoint

POST	/v2/ultra-dispatch
POST	/v2/batch-dispatch
POST	/v2/scheduled-dispatch



---

Template Management

Method	Endpoint

POST	/v2/template/upload
GET	/v2/template/:action
GET	/v2/template/list
DELETE	/v2/template/:action



---

## 🛠 Development

Requirements

Go 1.21+

PostgreSQL 13+

Redis 6+


Run Locally

git clone https://github.com/yourname/voidsend.git
cd voidsend
go mod download
cp config/config.example.yaml config/config.yaml
go run main.go

Run Tests

go test -v ./...


---

## 📁 Project Structure

voidsend/
├── api/
├── config/
├── core/
│   ├── database/
│   ├── engine/
│   ├── mailer/
│   ├── security/
│   └── template/
├── monitoring/
├── sdk/
├── scripts/
├── templates/
├── utils/
├── Dockerfile
├── docker-compose.yml
├── go.mod
└── README.md


---

## 🤝 Contributing

Open issues for bugs

Submit pull requests

Follow clean commit practices



---

## 📄 License

MIT License


---

## ⭐ Support the Project

If you find this project useful, consider starring the repository.

VoidSend is built for developers who prefer control, scalability, and transparency.

---

