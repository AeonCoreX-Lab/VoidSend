# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o voidsend main.go

# Final stage
FROM alpine:3.18

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/voidsend .
COPY --from=builder /app/config ./config
COPY --from=builder /app/templates ./templates

# Create non-root user
RUN addgroup -g 1000 -S voidsend && \
    adduser -u 1000 -S voidsend -G voidsend && \
    chown -R voidsend:voidsend /app

USER voidsend

# Expose ports
EXPOSE 8080 9090

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ./voidsend health || exit 1

# Run application
CMD ["./voidsend"]