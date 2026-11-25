package config

import (
	"os"
	"testing"
)

func baseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("S3_BUCKET", "b")
	t.Setenv("SESSION_JWT_SECRET", "s")
	t.Setenv("DEV_AUTH_BYPASS", "false")
	// Keep the developer's shell from leaking into assertions.
	for _, k := range []string{"CONFIG_FILE", "REDIS_URL", "REDIS_ADDR", "REDIS_USERNAME", "REDIS_PASSWORD", "REDIS_TLS", "CORS_ALLOWED_ORIGINS", "BOOTSTRAP_ADMIN_EMAIL", "BOOTSTRAP_ADMIN_PASSWORD", "INTERNAL_LOGIN_BREAK_GLASS"} {
		t.Setenv(k, "")
	}
}

func TestHistoryCleanupScheduleIsParsed(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil || cfg.HistoryCleanupSchedule != "0 3 * * *" {
		t.Fatalf("default schedule: %q err=%v", cfg.HistoryCleanupSchedule, err)
	}
	for _, ok := range []string{"@daily", "@every 6h", "30 2 * * 1", "off"} {
		t.Setenv("HISTORY_CLEANUP_SCHEDULE", ok)
		if cfg, err := Load(); err != nil || cfg.HistoryCleanupSchedule != ok {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"whenever", "* * *", "61 * * * *"} {
		t.Setenv("HISTORY_CLEANUP_SCHEDULE", bad)
		if _, err := Load(); err == nil {
			t.Errorf("%q must be rejected at startup", bad)
		}
	}
}

func TestBreakGlassRequiresBootstrapEmail(t *testing.T) {
	baseEnv(t)
	t.Setenv("INTERNAL_LOGIN_BREAK_GLASS", "true")
	if _, err := Load(); err == nil {
		t.Fatal("break-glass without a bootstrap admin email must fail startup")
	}
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "a@example.com")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "pw")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestTrustedProxiesAndRateLimitEnv(t *testing.T) {
	baseEnv(t)
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 192.168.0.1")
	t.Setenv("RATE_LIMIT_LOGIN_PER_MINUTE", "7")
	cfg, err := Load()
	if err != nil || len(cfg.TrustedProxies) != 2 || cfg.RateLimitLoginPerMinute != 7 || !cfg.RateLimitEnabled {
		t.Fatalf("%+v %v", cfg, err)
	}
	t.Setenv("TRUSTED_PROXIES", "not-an-ip")
	if _, err := Load(); err == nil {
		t.Fatal("bad TRUSTED_PROXIES must fail startup")
	}
}

func TestRedisURLParity(t *testing.T) {
	baseEnv(t)
	t.Setenv("REDIS_ADDR", "ignored:1")
	t.Setenv("REDIS_URL", "rediss://user:p%40ss@redis.example.com/3")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RedisAddr != "redis.example.com:6379" || cfg.RedisUsername != "user" || cfg.RedisPassword != "p@ss" || cfg.RedisDB != 3 || !cfg.RedisTLS {
		t.Fatalf("%+v", cfg)
	}
	t.Setenv("REDIS_PASSWORD", "override")
	if cfg, _ = Load(); cfg.RedisPassword != "override" {
		t.Fatal("REDIS_PASSWORD must override the URL's")
	}
	t.Setenv("REDIS_URL", "http://x")
	if _, err := Load(); err == nil {
		t.Fatal("non-redis scheme must fail")
	}
}

func TestRedisAddrForms(t *testing.T) {
	baseEnv(t)
	for in, want := range map[string]string{
		"redis": "redis:6379", "redis:6380": "redis:6380", "[::1]:6380": "[::1]:6380", "::1": "[::1]:6379", "[::1]": "[::1]:6379",
	} {
		t.Setenv("REDIS_ADDR", in)
		if cfg, err := Load(); err != nil || cfg.RedisAddr != want {
			t.Errorf("%q: %q %v", in, cfg.RedisAddr, err)
		}
	}
	t.Setenv("REDIS_ADDR", "redis:99999")
	if _, err := Load(); err == nil {
		t.Error("bad port must fail")
	}
}

func TestCORSAllowedOrigins(t *testing.T) {
	baseEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://Docs.example.com/, http://localhost:5173")
	cfg, err := Load()
	if err != nil || len(cfg.CORSAllowedOrigins) != 2 || cfg.CORSAllowedOrigins[0] != "https://docs.example.com" {
		t.Fatalf("%+v %v", cfg.CORSAllowedOrigins, err)
	}
	for _, bad := range []string{"*", "docs.example.com", "https://x.com/app"} {
		t.Setenv("CORS_ALLOWED_ORIGINS", bad)
		if _, err := Load(); err == nil {
			t.Errorf("%q must fail startup", bad)
		}
	}
}

func TestCORSEnvReplacesYAMLAndYAMLIsValidated(t *testing.T) {
	baseEnv(t)
	dir := t.TempDir()
	write := func(body string) {
		f := dir + "/c.yaml"
		if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CONFIG_FILE", f)
	}
	write("corsAllowedOrigins: ['*']\n")
	if _, err := Load(); err == nil {
		t.Fatal("wildcard from YAML must fail startup")
	}
	write("corsAllowedOrigins: ['https://a.example.com/']\n")
	if cfg, err := Load(); err != nil || cfg.CORSAllowedOrigins[0] != "https://a.example.com" {
		t.Fatalf("%v %v", cfg.CORSAllowedOrigins, err)
	}
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://b.example.com")
	if cfg, _ := Load(); len(cfg.CORSAllowedOrigins) != 1 || cfg.CORSAllowedOrigins[0] != "https://b.example.com" {
		t.Fatalf("env must replace YAML: %v", cfg.CORSAllowedOrigins)
	}
}
