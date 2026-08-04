package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	Port          string
	AppEnv        string
	LogLevel      string
	DatabaseURL   string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	APIKey        string
	ShortURLBase  string
}

func Load() Config {
	return Config{
		Port:          envOr("PORT", "8080"),
		AppEnv:        envOr("APP_ENV", "development"),
		LogLevel:      envOr("LOG_LEVEL", "info"),
		DatabaseURL:   envOr("DATABASE_URL", "postgres://myuser:mysecretpassword@localhost:5432/smalllight"),
		RedisAddr:     envOr("REDIS_ADDR", "localhost:6379"),
		RedisPassword: envOr("REDIS_PASSWORD", "myredispassword"),
		RedisDB:       envInt("REDIS_DB", 0),
		APIKey:        envOr("API_KEY", "dev-api-key"),
		ShortURLBase:  envOr("SHORT_URL_BASE", "http://localhost:8080"),
	}
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
	if c.AppEnv == "production" && c.APIKey == "dev-api-key" {
		return errors.New("API_KEY must be set in production")
	}

	return nil
}
