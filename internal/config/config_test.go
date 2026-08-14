package config

// HOW TO TEST config.Load():
//
// Load() reads os.Getenv with fallbacks, so tests must be hermetic:
//   - Set the var you care about with t.Setenv(key, value). t.Setenv
//     automatically restores the previous value when the test finishes,
//     so it is safe even if the machine already had the var set.
//   - Set every OTHER var to "" (t.Setenv("PORT", "")) so a value that a
//     previous test set cannot leak into your assertions.
//
// Call clearAll() at the top of every test before setting what you need.

import (
	"testing"
	"time"
)

func clearAll(t *testing.T) {
	for _, k := range []string{"PORT", "APP_ENV", "LOG_LEVEL", "DATABASE_URL",
		"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "SHORT_URL_BASE",
		"DEFAULT_LINK_TTL", "CLICK_FLUSH_INTERVAL", "LINK_CLEANUP_INTERVAL",
		"JWT_SECRET", "JWT_TTL"} {
		t.Setenv(k, "")
	}
}

// TestLoadDefaults asserts every field has its documented fallback when no
// environment variables are set. Match the values in Load() exactly:
// Port "8080", AppEnv "development", LogLevel "info",
// DatabaseURL "postgres://myuser:mysecretpassword@localhost:5432/smalllight",
// RedisAddr "localhost:6379", RedisPassword "myredispassword", RedisDB 0,
// ShortURLBase "http://localhost:8080", DefaultLinkTTL 168h,
// ClickFlushInterval 30s, LinkCleanupInterval 1h,
// JWTSecret "dev-jwt-secret-change-me", JWTTTL 24h.
func TestLoadDefaults(t *testing.T) {
	clearAll(t)
	got := Load()

	want := Config{
		Port:                "8080",
		AppEnv:              "development",
		LogLevel:            "info",
		DatabaseURL:         "postgres://myuser:mysecretpassword@localhost:5432/smalllight",
		RedisAddr:           "localhost:6379",
		RedisPassword:       "myredispassword",
		RedisDB:             0,
		ShortURLBase:        "http://localhost:8080",
		DefaultLinkTTL:      168 * time.Hour,
		ClickFlushInterval:  30 * time.Second,
		LinkCleanupInterval: time.Hour,
		JWTSecret:           "dev-jwt-secret-change-me",
		JWTTTL:              24 * time.Hour,
	}

	if got != want {
		t.Errorf("Load() = %+v\nwant   %+v", got, want)
	}
}

// TestLoadEnvOverrides sets a few vars and asserts they are parsed. Cover
// every type at least once: a string (PORT), an int (REDIS_DB), a duration
// (CLICK_FLUSH_INTERVAL like "45s").
//
//  1. clearAll, then t.Setenv("PORT", "9090"), t.Setenv("REDIS_DB", "2"),
//     t.Setenv("CLICK_FLUSH_INTERVAL", "45s").
//  2. got := Load().
//  3. Assert got.Port == "9090", got.RedisDB == 2, got.ClickFlushInterval ==
//     45*time.Second. (time is imported by the config package — import it in
//     the test file too.)
func TestLoadEnvOverrides(t *testing.T) {
	clearAll(t)
	t.Setenv("PORT", "9090")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("CLICK_FLUSH_INTERVAL", "45s")

	got := Load()

	if got.Port != "9090" {
		t.Errorf("got %v, want 9090", got.Port)
	}

	if got.RedisDB != 2 {
		t.Errorf("got %v, want 2", got.RedisDB)
	}

	if got.ClickFlushInterval != 45*time.Second {
		t.Errorf("got %v, want 45s", got.ClickFlushInterval)
	}
}

// TestLoadInvalidEnvFallsBack feeds garbage values and asserts the fallback
// wins instead of a crash or zero value.
func TestLoadInvalidEnvFallsBack(t *testing.T) {
	clearAll(t)
	t.Setenv("REDIS_DB", "not-a-number")
	t.Setenv("CLICK_FLUSH_INTERVAL", "bogus")

	got := Load()

	// A garbage int must fall back to 0, not crash strconv.Atoi.
	if got.RedisDB != 0 {
		t.Errorf("RedisDB = %d, want fallback 0", got.RedisDB)
	}
	// A garbage duration must fall back to the 30s default, not zero out the
	// worker (a 0 interval would spin the flush loop hot).
	if got.ClickFlushInterval != 30*time.Second {
		t.Errorf("ClickFlushInterval = %v, want fallback 30s", got.ClickFlushInterval)
	}
}

// TestValidateProductionSecret asserts Validate() enforces a non-default
// JWT_SECRET only in production.
func TestValidateProductionSecret(t *testing.T) {
	// Default secret in production is the foot-gun this guard exists for.
	cfg := Config{AppEnv: "production", JWTSecret: "dev-jwt-secret-change-me"}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() = nil, want error for the default secret in production")
	}

	// A real secret satisfies the guard.
	cfg.JWTSecret = "a-real-secret"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for a real secret", err)
	}

	// Development is allowed to keep the default so local runs stay zero-config.
	dev := Config{AppEnv: "development", JWTSecret: "dev-jwt-secret-change-me"}
	if err := dev.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil in development", err)
	}
}
