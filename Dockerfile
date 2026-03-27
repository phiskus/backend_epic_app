# ── Stage 1: build ──────────────────────────────────────────────────────────
FROM golang:alpine AS builder
WORKDIR /app

# Cache dependency downloads separately from source code
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o lab-reporter .

# ── Stage 2: minimal runtime ────────────────────────────────────────────────
FROM alpine:latest
# ca-certificates: needed for HTTPS calls to Epic / Gmail
# tzdata: needed for correct scheduler timezone handling
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/lab-reporter .

EXPOSE 8080
ENTRYPOINT ["./lab-reporter"]
