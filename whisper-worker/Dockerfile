# Whisper Worker Dockerfile
# Multi-stage build for small production image

# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o whisper-worker ./cmd

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/whisper-worker .
COPY config.yaml .

RUN adduser -D -g '' appuser && chown -R appuser:appuser /app
USER appuser

ENTRYPOINT ["./whisper-worker"]