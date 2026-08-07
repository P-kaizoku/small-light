package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port           string
	AppEnv         string
	LogLevel       string
	DatabaseURL    string
	RedisAddr      string
	RedisPassword  string
	RedisDB        int
	ShortURLBase   string
	DefaultLinkTTL time.Duration
	JWTSecret      string
	JWTTTL         time.Duration
}

func Load() Config {
	return Config{
		Port:           envOr("PORT", "8080"),
		AppEnv:         envOr("APP_ENV", "development"),
		LogLevel:       envOr("LOG_LEVEL", "info"),
		DatabaseURL:    envOr("DATABASE_URL", "postgres://myuser:mysecretpassword@localhost:5432/smalllight"),
		RedisAddr:      envOr("REDIS_ADDR", "localhost:6379"),
		RedisPassword:  envOr("REDIS_PASSWORD", "myredispassword"),
		RedisDB:        envInt("REDIS_DB", 0),
		ShortURLBase:   envOr("SHORT_URL_BASE", "http://localhost:8080"),
		DefaultLinkTTL: envDuration("DEFAULT_LINK_TTL", 168*time.Hour),
		JWTSecret:      envOr("JWT_SECRET", "dev-jwt-secret-change-me"),
		JWTTTL:         envDuration("JWT_TTL", 24*time.Hour),
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func (c Config) Validate() error {
	if c.AppEnv == "production" && c.JWTSecret == "dev-jwt-secret-change-me" {
		return errors.New("JWT_SECRET must be set in production")
	}

	return nil
}
