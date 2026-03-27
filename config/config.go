package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration loaded from the environment.
type Config struct {
	// Epic SMART Backend Services
	EpicClientID       string
	EpicFHIRBase       string
	EpicTokenURL       string
	EpicPrivateKeyPath string
	EpicPublicKeyPath  string
	EpicJWKSURL        string // publicly reachable URL Epic will fetch for JWKS
	EpicGroupID        string // FHIR Group ID defining the patient population

	// HTTP server (JWKS endpoint)
	Port string

	// SMTP
	SMTPHost string
	SMTPPort string
	SMTPUser string
	SMTPPass string
	SMTPTo   string
	SMTPFrom string

	// Scheduler
	SchedulerInterval time.Duration
}

// Load reads .env (if present) then validates required fields.
func Load() (*Config, error) {
	// godotenv.Load is a no-op if .env doesn't exist (e.g. in production where
	// env vars are injected by Cloud Run / docker-compose).
	_ = godotenv.Load()

	cfg := &Config{
		EpicClientID:       os.Getenv("EPIC_CLIENT_ID"),
		EpicFHIRBase:       os.Getenv("EPIC_FHIR_BASE"),
		EpicTokenURL:       os.Getenv("EPIC_TOKEN_URL"),
		EpicPrivateKeyPath: os.Getenv("EPIC_PRIVATE_KEY_PATH"),
		EpicPublicKeyPath:  os.Getenv("EPIC_PUBLIC_KEY_PATH"),
		EpicJWKSURL:        os.Getenv("EPIC_JWKS_URL"),
		EpicGroupID:        os.Getenv("EPIC_GROUP_ID"),
		Port:              getEnvOrDefault("PORT", "8080"),
		SMTPHost:          os.Getenv("SMTP_HOST"),
		SMTPPort:          getEnvOrDefault("SMTP_PORT", "587"),
		SMTPUser:          os.Getenv("SMTP_USER"),
		SMTPPass:          os.Getenv("SMTP_PASS"),
		SMTPTo:            os.Getenv("SMTP_TO"),
		SMTPFrom:          os.Getenv("SMTP_FROM"),
	}

	// Parse optional scheduler interval (default: 24h)
	if raw := os.Getenv("SCHEDULER_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid SCHEDULER_INTERVAL %q: %w", raw, err)
		}
		cfg.SchedulerInterval = d
	} else {
		cfg.SchedulerInterval = 24 * time.Hour
	}

	// Validate required fields — fail loudly rather than silently misbehave
	required := map[string]string{
		"EPIC_CLIENT_ID":        cfg.EpicClientID,
		"EPIC_FHIR_BASE":        cfg.EpicFHIRBase,
		"EPIC_TOKEN_URL":        cfg.EpicTokenURL,
		"EPIC_PRIVATE_KEY_PATH": cfg.EpicPrivateKeyPath,
		"EPIC_PUBLIC_KEY_PATH":  cfg.EpicPublicKeyPath,
		"EPIC_JWKS_URL":  cfg.EpicJWKSURL,
		"EPIC_GROUP_ID": cfg.EpicGroupID,
		"SMTP_HOST":            cfg.SMTPHost,
		"SMTP_USER":            cfg.SMTPUser,
		"SMTP_PASS":            cfg.SMTPPass,
		"SMTP_TO":              cfg.SMTPTo,
		"SMTP_FROM":            cfg.SMTPFrom,
	}
	for name, val := range required {
		if val == "" {
			return nil, fmt.Errorf("required env var %s is not set", name)
		}
	}

	return cfg, nil
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
