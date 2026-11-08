# Worker Dockerfile
# Multi-stage build for small production image

# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o worker ./cmd

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/worker .
COPY config.yaml .
COPY prompts ./prompts

# ADR 0015: EDGAR_USER_AGENT is required by SEC's fair-access policy
# (must include contact info); FINNHUB_API_KEY is optional — leave
# unset to run finance_ingest with SEC EDGAR data only.
ENV EDGAR_USER_AGENT="Conduit-finance-ingest/0.1 (contact: ops@conduit.local)"
ENV FINNHUB_API_KEY=""

RUN adduser -D -g '' appuser && chown -R appuser:appuser /app
USER appuser

ENTRYPOINT ["./worker"]