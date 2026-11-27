package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr            string
	DatabaseURL     string
	Production      bool
	AdminUsername   string
	AdminPassword   string
	EncryptionKey   string
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
	RotateInterval  time.Duration
	GracePeriod     time.Duration

	WorkerID               string
	WorkerLease            time.Duration
	WorkerPollInterval     time.Duration
	WorkerMaxAttempts      int
	WorkerConcurrency      int
	WorkerDrainTimeout     time.Duration
	WorkerRecoveryInterval time.Duration
	LogLevel               string
	ServiceName            string
}

func Load() Config {
	return Config{
		Addr:            env("APP_ADDR", ":8080"),
		DatabaseURL:     env("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5434/dbmp?sslmode=disable"),
		Production:      env("APP_ENV", "development") == "production",
		AdminUsername:   env("ADMIN_USERNAME", "admin"),
		AdminPassword:   os.Getenv("ADMIN_PASSWORD"),
		EncryptionKey:   os.Getenv("APP_ENCRYPTION_KEY"),
		IdleTimeout:     duration("SESSION_IDLE_TIMEOUT", 30*time.Minute),
		AbsoluteTimeout: duration("SESSION_ABSOLUTE_TIMEOUT", 8*time.Hour),
		RotateInterval:  duration("SESSION_ROTATE_INTERVAL", 15*time.Minute),
		GracePeriod:     duration("SESSION_GRACE_PERIOD", 30*time.Second),

		WorkerID:               env("WORKER_ID", ""),
		WorkerLease:            duration("WORKER_LEASE", 30*time.Second),
		WorkerPollInterval:     duration("WORKER_POLL_INTERVAL", 250*time.Millisecond),
		WorkerMaxAttempts:      intEnv("WORKER_MAX_ATTEMPTS", 5),
		WorkerConcurrency:      intEnv("WORKER_CONCURRENCY", 4),
		WorkerDrainTimeout:     duration("WORKER_DRAIN_TIMEOUT", 30*time.Second),
		WorkerRecoveryInterval: duration("WORKER_RECOVERY_INTERVAL", 15*time.Second),
		LogLevel:               env("LOG_LEVEL", "info"),
		ServiceName:            env("OTEL_SERVICE_NAME", "dbmp"),
	}
}

func intEnv(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func duration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
