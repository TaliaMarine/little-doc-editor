// Package config loads and validates api service configuration from environment
// variables, with an optional YAML file for values better expressed as structured
// config (see SPEC.md §3: "env vars + .yaml, validated at startup").
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/TaliaMarine/little-doc-editor/api/internal/clientip"
	"github.com/TaliaMarine/little-doc-editor/api/internal/cors"
	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// Config holds all runtime configuration for the api service.
type Config struct {
	// HTTPAddr is the address the HTTP server listens on, e.g. ":8080".
	HTTPAddr string `yaml:"httpAddr"`
	// LogLevel is one of "debug", "info", "warn", "error".
	LogLevel string `yaml:"logLevel"`
	// DatabaseURL is a pgx-compatible connection string for the external,
	// bring-your-own PostgreSQL instance (Decision 18).
	DatabaseURL string `yaml:"databaseURL"`
	// RedisAddr is the address of the bring-your-own Redis instance used for
	// collab fan-out and shared session/presence state (Decisions 3, 20, 29).
	RedisAddr string `yaml:"redisAddr"`
	// RedisPassword is the Redis AUTH password (REDIS_PASSWORD), optional.
	// The api uses Redis for the shared rate limiter (SPEC.md §8).
	RedisPassword string `yaml:"redisPassword"`
	// RedisUsername (REDIS_USERNAME) for ACL users; RedisTLS (REDIS_TLS)
	// enables TLS to Redis.
	RedisUsername string `yaml:"redisUsername"`
	RedisTLS      bool   `yaml:"redisTLS"`
	// RedisDB is the logical database number (from REDIS_URL's /db path).
	RedisDB int `yaml:"redisDB"`

	// DevAuthBypass, when true, skips real session/OIDC authentication
	// entirely and treats every request as a fixed local-dev org-admin
	// identity. This must NEVER be true outside a developer's own machine.
	// Logged loudly at startup when enabled; never the default.
	DevAuthBypass bool `yaml:"devAuthBypass"`

	// SessionJWTSecret signs session tokens for both internal-login and
	// guest sessions. Must match the `collab` service's SESSION_JWT_SECRET
	// exactly (SPEC.md §4) — collab validates the same tokens api issues.
	// Required unless DevAuthBypass is true.
	SessionJWTSecret string `yaml:"sessionJWTSecret"`
	// SessionTTL is how long an issued session token stays valid.
	SessionTTL time.Duration `yaml:"sessionTTL"`

	// InternalLoginBreakGlass overrides instance_settings.internal_login_disabled,
	// re-enabling internal/password login regardless of that DB setting
	// (Decision 14) — an env var/CLI flag on the pod, deliberately not a DB
	// row, so a misconfigured OIDC setup can't lock every admin out.
	InternalLoginBreakGlass bool `yaml:"internalLoginBreakGlass"`

	// BootstrapAdminEmail/Password: if set and no org admin exists yet, an
	// internal org-admin account is created with these credentials at
	// startup (idempotent — a no-op once any org admin exists). This is the
	// only way to get a first admin account without OIDC configured
	// (Decision 11: admin-invite-only signup — someone has to send the
	// first invite).
	BootstrapAdminEmail    string `yaml:"bootstrapAdminEmail"`
	BootstrapAdminPassword string `yaml:"bootstrapAdminPassword"`

	// OIDC (Decision 14: single instance-wide issuer).
	OIDCEnabled      bool   `yaml:"oidcEnabled"`
	OIDCIssuer       string `yaml:"oidcIssuer"`
	OIDCClientID     string `yaml:"oidcClientID"`
	OIDCClientSecret string `yaml:"oidcClientSecret"`
	// OIDCRedirectURL is this deployment's own callback URL, registered
	// with the identity provider — e.g. https://docs.example.com/api/v1/auth/oidc/callback.
	OIDCRedirectURL string `yaml:"oidcRedirectURL"`

	// StorageBackend selects where attachment bytes live: "s3" (default,
	// Decision 5) or "filesystem" (a directory, typically a PVC mount).
	StorageBackend string `yaml:"storageBackend"`
	StorageDir     string `yaml:"storageDir"`
	// OIDCPostLoginURL is where the browser lands after a successful OIDC
	// login; the session token is appended as a URL fragment. Defaults to
	// /auth/callback on the origin of OIDCRedirectURL.
	OIDCPostLoginURL string `yaml:"oidcPostLoginURL"`
	// OIDCAdminGroup, when set, makes members of that IdP group org admins
	// (re-evaluated on every login, so removal from the group demotes).
	OIDCAdminGroup string `yaml:"oidcAdminGroup"`
	// OIDCGroupsClaim is the ID-token claim holding the user's group names.
	OIDCGroupsClaim string `yaml:"oidcGroupsClaim"`
	// DefaultMemberRole is the space role every signed-in (non-guest) user
	// implicitly has in every space: "", "viewer" or "editor". An explicit
	// space membership still applies when it is higher.
	DefaultMemberRole string `yaml:"defaultMemberRole"`
	// S3-compatible object storage for attachments (Decision 5).
	S3Endpoint     string `yaml:"s3Endpoint"`
	S3Bucket       string `yaml:"s3Bucket"`
	S3Region       string `yaml:"s3Region"`
	S3AccessKey    string `yaml:"s3AccessKey"`
	S3SecretKey    string `yaml:"s3SecretKey"`
	S3UsePathStyle bool   `yaml:"s3UsePathStyle"` // true for MinIO and most non-AWS S3-compatible stores

	// Attachment allowlist + max size (Decision 17). These are the
	// deployment *defaults*; an org admin can override both at runtime via
	// PATCH /admin/settings (stored in instance_settings), and the override
	// wins until reset. Defaults follow SPEC.md §7.2 (images, PDF, common
	// Office formats, zip, plain text/code).
	AttachmentAllowlist    []string `yaml:"attachmentAllowlist"`
	AttachmentMaxSizeBytes int64    `yaml:"attachmentMaxSizeBytes"`

	// TrustedProxies are the CIDRs (or bare IPs) of reverse proxies whose
	// X-Forwarded-For header is believed (TRUSTED_PROXIES, comma-separated).
	// Empty means X-Forwarded-For is ignored and the TCP peer address is the
	// client address — the safe default when the api is directly exposed.
	TrustedProxies []string `yaml:"trustedProxies"`

	// CORSAllowedOrigins (CORS_ALLOWED_ORIGINS, comma-separated origins such as
	// https://docs.example.com) lets a SPA served from another origin call the
	// api. Empty (default) = no CORS support. Exact origins only, never "*".
	CORSAllowedOrigins []string `yaml:"corsAllowedOrigins"`

	// RateLimitEnabled turns the login/redeem rate limiter on (default true).
	// Backed by Redis when REDIS_ADDR is set, with an in-process fallback.
	RateLimitEnabled bool `yaml:"rateLimitEnabled"`
	// RateLimitLoginPerMinute caps POST /auth/login attempts per client IP.
	RateLimitLoginPerMinute int `yaml:"rateLimitLoginPerMinute"`
	// RateLimitPublicPerMinute caps the other unauthenticated, credential-
	// or token-taking endpoints (share-link resolve/redeem, invite
	// resolve/redeem, OIDC login) per client IP.
	RateLimitPublicPerMinute int `yaml:"rateLimitPublicPerMinute"`

	// HistoryCleanupSchedule is a standard 5-field cron expression (or a
	// descriptor such as "@every 6h" / "@daily") for the in-process history
	// cleanup job (Decisions 9, 28). "off" disables the job.
	HistoryCleanupSchedule string `yaml:"historyCleanupSchedule"`
	// RevisionDebounce is the minimum gap between two auto-created revisions
	// of one page by the same author (SPEC.md §6.7: not every save).
	RevisionDebounce time.Duration `yaml:"revisionDebounce"`

	// MetricsAddr, when set (e.g. ":9090"), serves Prometheus /metrics on a
	// separate listener that is not routed through the public ingress
	// (Decision 27). Empty disables it.
	MetricsAddr string `yaml:"metricsAddr"`
	// OTelEndpoint (OTEL_EXPORTER_OTLP_ENDPOINT) enables OTLP trace export;
	// OTelProtocol is "http/protobuf" (default) or "grpc".
	OTelEndpoint string `yaml:"otelEndpoint"`
	OTelProtocol string `yaml:"otelProtocol"`
}

// Defaults returns the config defaults before any file/env overrides —
// exported mainly so tests (e.g. attachments.Validate) can exercise the real
// default allowlist/size limit rather than duplicating them.
func Defaults() Config {
	return Config{
		HTTPAddr:               ":8080",
		LogLevel:               "info",
		SessionTTL:             24 * time.Hour,
		S3Region:               "us-east-1",
		S3UsePathStyle:         true,
		OIDCGroupsClaim:        "groups",
		AttachmentMaxSizeBytes: 25 * 1024 * 1024, // 25MB

		RateLimitEnabled:         true,
		RateLimitLoginPerMinute:  10,
		RateLimitPublicPerMinute: 30,
		HistoryCleanupSchedule:   "0 3 * * *",
		RevisionDebounce:         5 * time.Minute,
		OTelProtocol:             "http/protobuf",
		AttachmentAllowlist: []string{
			"image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml",
			"application/pdf",
			"application/msword",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			"application/vnd.openxmlformats-officedocument.presentationml.presentation",
			"application/zip",
			"text/plain", "text/csv", "text/markdown",
		},
	}
}

// Load builds a Config from defaults, an optional YAML file referenced by the
// CONFIG_FILE environment variable, and environment variable overrides — in
// that order, so env vars always win. It returns an error if the resulting
// config is invalid.
func Load() (Config, error) {
	cfg := Defaults()

	if path := os.Getenv("CONFIG_FILE"); path != "" {
		if err := loadYAMLFile(path, &cfg); err != nil {
			return Config{}, fmt.Errorf("loading config file %q: %w", path, err)
		}
	}

	if v := os.Getenv("HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		cfg.RedisAddr = v
	}
	// Same inputs and precedence as the collab service (collab/src/redisConfig.ts):
	// REDIS_URL (redis:// or rediss://) wins over REDIS_ADDR, and the
	// separate REDIS_USERNAME / REDIS_PASSWORD / REDIS_TLS override it.
	if v := os.Getenv("REDIS_URL"); v != "" {
		if err := applyRedisURL(&cfg, v); err != nil {
			return Config{}, err
		}
	} else if cfg.RedisAddr != "" {
		addr, err := normalizeRedisAddr(cfg.RedisAddr)
		if err != nil {
			return Config{}, err
		}
		cfg.RedisAddr = addr
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		cfg.RedisPassword = v
	}
	if v := os.Getenv("REDIS_USERNAME"); v != "" {
		cfg.RedisUsername = v
	}
	if v := os.Getenv("REDIS_TLS"); v != "" {
		// Same spelling as collab: only "true"/"1" enable it; a rediss:// URL
		// still forces TLS on.
		cfg.RedisTLS = v == "true" || v == "1" || strings.HasPrefix(os.Getenv("REDIS_URL"), "rediss://")
	}
	if v := os.Getenv("DEV_AUTH_BYPASS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("DEV_AUTH_BYPASS must be true/false: %w", err)
		}
		cfg.DevAuthBypass = b
	}
	if v := os.Getenv("SESSION_JWT_SECRET"); v != "" {
		cfg.SessionJWTSecret = v
	}
	if v := os.Getenv("SESSION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("SESSION_TTL must be a duration (e.g. 24h): %w", err)
		}
		cfg.SessionTTL = d
	}
	if v := os.Getenv("INTERNAL_LOGIN_BREAK_GLASS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("INTERNAL_LOGIN_BREAK_GLASS must be true/false: %w", err)
		}
		cfg.InternalLoginBreakGlass = b
	}
	if v := os.Getenv("BOOTSTRAP_ADMIN_EMAIL"); v != "" {
		cfg.BootstrapAdminEmail = v
	}
	if v := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"); v != "" {
		cfg.BootstrapAdminPassword = v
	}
	if v := os.Getenv("OIDC_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("OIDC_ENABLED must be true/false: %w", err)
		}
		cfg.OIDCEnabled = b
	}
	if v := os.Getenv("OIDC_ISSUER"); v != "" {
		cfg.OIDCIssuer = v
	}
	if v := os.Getenv("OIDC_CLIENT_ID"); v != "" {
		cfg.OIDCClientID = v
	}
	if v := os.Getenv("OIDC_CLIENT_SECRET"); v != "" {
		cfg.OIDCClientSecret = v
	}
	if v := os.Getenv("OIDC_REDIRECT_URL"); v != "" {
		cfg.OIDCRedirectURL = v
	}
	if v := os.Getenv("OIDC_ADMIN_GROUP"); v != "" {
		cfg.OIDCAdminGroup = v
	}
	if v := os.Getenv("OIDC_GROUPS_CLAIM"); v != "" {
		cfg.OIDCGroupsClaim = v
	}
	if v := os.Getenv("DEFAULT_MEMBER_ROLE"); v != "" {
		cfg.DefaultMemberRole = v
	}
	if v := os.Getenv("STORAGE_BACKEND"); v != "" {
		cfg.StorageBackend = v
	}
	if v := os.Getenv("STORAGE_DIR"); v != "" {
		cfg.StorageDir = v
	}
	if v := os.Getenv("OIDC_POST_LOGIN_URL"); v != "" {
		cfg.OIDCPostLoginURL = v
	}
	if v := os.Getenv("S3_ENDPOINT"); v != "" {
		cfg.S3Endpoint = v
	}
	if v := os.Getenv("S3_BUCKET"); v != "" {
		cfg.S3Bucket = v
	}
	if v := os.Getenv("S3_REGION"); v != "" {
		cfg.S3Region = v
	}
	if v := os.Getenv("S3_ACCESS_KEY"); v != "" {
		cfg.S3AccessKey = v
	}
	if v := os.Getenv("S3_SECRET_KEY"); v != "" {
		cfg.S3SecretKey = v
	}
	if v := os.Getenv("S3_USE_PATH_STYLE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("S3_USE_PATH_STYLE must be true/false: %w", err)
		}
		cfg.S3UsePathStyle = b
	}
	if v := os.Getenv("ATTACHMENT_ALLOWLIST"); v != "" {
		// Trimmed so "image/png, application/pdf" (a natural way to write
		// it in a values file) doesn't silently produce " application/pdf",
		// which would never match a sniffed type.
		cfg.AttachmentAllowlist = splitList(v)
	}
	if v := os.Getenv("ATTACHMENT_MAX_SIZE_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("ATTACHMENT_MAX_SIZE_BYTES must be an integer: %w", err)
		}
		cfg.AttachmentMaxSizeBytes = n
	}

	if v := os.Getenv("TRUSTED_PROXIES"); v != "" {
		cfg.TrustedProxies = splitList(v)
	}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		cfg.CORSAllowedOrigins = splitList(v) // replaces any YAML list, like other env overrides
	}
	if v := os.Getenv("RATE_LIMIT_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("RATE_LIMIT_ENABLED must be true/false: %w", err)
		}
		cfg.RateLimitEnabled = b
	}
	for _, e := range []struct {
		name string
		dst  *int
	}{
		{"RATE_LIMIT_LOGIN_PER_MINUTE", &cfg.RateLimitLoginPerMinute},
		{"RATE_LIMIT_PUBLIC_PER_MINUTE", &cfg.RateLimitPublicPerMinute},
	} {
		if v := os.Getenv(e.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return Config{}, fmt.Errorf("%s must be an integer: %w", e.name, err)
			}
			*e.dst = n
		}
	}
	if v := os.Getenv("HISTORY_CLEANUP_SCHEDULE"); v != "" {
		cfg.HistoryCleanupSchedule = v
	}
	if v := os.Getenv("HISTORY_REVISION_DEBOUNCE"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("HISTORY_REVISION_DEBOUNCE must be a duration (e.g. 5m): %w", err)
		}
		cfg.RevisionDebounce = d
	}
	if v := os.Getenv("METRICS_ADDR"); v != "" {
		cfg.MetricsAddr = v
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); v != "" {
		cfg.OTelEndpoint = v
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"); v != "" {
		cfg.OTelProtocol = v
	}

	// Origins from YAML and env alike: validate and canonicalize.
	for i, o := range cfg.CORSAllowedOrigins {
		n, err := cors.NormalizeOrigin(o)
		if err != nil {
			return Config{}, err
		}
		cfg.CORSAllowedOrigins[i] = n
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// splitList splits a comma-separated env value, trimming blanks.
func splitList(v string) []string {
	var out []string
	for _, t := range strings.Split(v, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func loadYAMLFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, cfg)
}

func (c Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL (or databaseURL in CONFIG_FILE) is required")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL must be one of debug|info|warn|error, got %q", c.LogLevel)
	}
	switch c.DefaultMemberRole {
	case "", "viewer", "editor":
	default:
		return fmt.Errorf("DEFAULT_MEMBER_ROLE must be empty, viewer or editor, got %q", c.DefaultMemberRole)
	}
	switch c.StorageBackend {
	case "", "s3":
		if c.S3Bucket == "" {
			return fmt.Errorf("S3_BUCKET (or s3Bucket in CONFIG_FILE) is required")
		}
	case "filesystem":
		if c.StorageDir == "" {
			return fmt.Errorf("STORAGE_DIR is required when STORAGE_BACKEND=filesystem")
		}
	default:
		return fmt.Errorf("STORAGE_BACKEND must be s3 or filesystem, got %q", c.StorageBackend)
	}
	if len(c.AttachmentAllowlist) == 0 {
		return fmt.Errorf("ATTACHMENT_ALLOWLIST must name at least one content type")
	}
	if c.AttachmentMaxSizeBytes <= 0 {
		return fmt.Errorf("ATTACHMENT_MAX_SIZE_BYTES must be positive")
	}
	if !c.DevAuthBypass && c.SessionJWTSecret == "" {
		return fmt.Errorf("SESSION_JWT_SECRET is required unless DEV_AUTH_BYPASS=true")
	}
	if (c.BootstrapAdminEmail == "") != (c.BootstrapAdminPassword == "") {
		return fmt.Errorf("BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD must be set together")
	}
	if c.InternalLoginBreakGlass && c.BootstrapAdminEmail == "" {
		// Break-glass restores login for the bootstrap admin only
		// (Decision 14), and that account is identified by this email.
		return fmt.Errorf("INTERNAL_LOGIN_BREAK_GLASS requires BOOTSTRAP_ADMIN_EMAIL (break-glass is restricted to the bootstrap admin)")
	}
	if _, err := clientip.New(c.TrustedProxies); err != nil {
		return err
	}
	if c.RateLimitLoginPerMinute <= 0 || c.RateLimitPublicPerMinute <= 0 {
		return fmt.Errorf("RATE_LIMIT_LOGIN_PER_MINUTE and RATE_LIMIT_PUBLIC_PER_MINUTE must be positive")
	}
	if c.RevisionDebounce < 0 {
		return fmt.Errorf("HISTORY_REVISION_DEBOUNCE must not be negative")
	}
	if s := c.HistoryCleanupSchedule; s != "" && s != "off" {
		if _, err := cron.ParseStandard(s); err != nil {
			return fmt.Errorf("HISTORY_CLEANUP_SCHEDULE %q is not a valid cron expression (use 5 fields, @daily, @every 6h, or \"off\"): %w", s, err)
		}
	}
	switch c.OTelProtocol {
	case "http/protobuf", "grpc":
	default:
		return fmt.Errorf("OTEL_EXPORTER_OTLP_PROTOCOL must be http/protobuf or grpc, got %q", c.OTelProtocol)
	}
	if c.OIDCEnabled && (c.OIDCIssuer == "" || c.OIDCClientID == "" || c.OIDCRedirectURL == "") {
		// OIDC_CLIENT_SECRET is deliberately optional: the client is a public
		// client authenticating with PKCE alone.
		return fmt.Errorf("OIDC_ISSUER, OIDC_CLIENT_ID, and OIDC_REDIRECT_URL are all required when OIDC_ENABLED=true")
	}
	return nil
}

// applyRedisURL fills the Redis settings from a redis:// or rediss:// URL
// (rediss implies TLS).
func applyRedisURL(cfg *Config, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		// Don't echo the URL: it may carry a password.
		return fmt.Errorf("REDIS_URL is not a valid URL")
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return fmt.Errorf("REDIS_URL must use redis:// or rediss://")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("REDIS_URL has no host")
	}
	port := u.Port()
	if port == "" {
		port = "6379"
	}
	cfg.RedisAddr = net.JoinHostPort(u.Hostname(), port)
	if _, err := normalizeRedisAddr(cfg.RedisAddr); err != nil {
		return err
	}
	if u.User != nil {
		cfg.RedisUsername = u.User.Username()
		cfg.RedisPassword, _ = u.User.Password()
	}
	if db := strings.TrimPrefix(u.Path, "/"); db != "" {
		n, err := strconv.Atoi(db)
		if err != nil || n < 0 {
			return fmt.Errorf("REDIS_URL database must be a non-negative integer")
		}
		cfg.RedisDB = n
	}
	cfg.RedisTLS = cfg.RedisTLS || u.Scheme == "rediss"
	return nil
}

// normalizeRedisAddr turns the forms REDIS_ADDR accepts (host, host:port,
// [v6]:port, bare IPv6 literal) into host:port.
func normalizeRedisAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("empty REDIS_ADDR")
	}
	if strings.HasPrefix(addr, "[") {
		if _, port, err := net.SplitHostPort(addr); err == nil {
			return addr, checkPort(addr, port)
		}
		if strings.HasSuffix(addr, "]") && len(addr) > 2 { // [v6] without a port
			return addr + ":6379", nil
		}
		return "", fmt.Errorf("invalid REDIS_ADDR %q", addr)
	}
	switch strings.Count(addr, ":") {
	case 0:
		return addr + ":6379", nil
	case 1:
		host, port, err := net.SplitHostPort(addr)
		if err != nil || host == "" {
			return "", fmt.Errorf("invalid REDIS_ADDR %q", addr)
		}
		return addr, checkPort(addr, port)
	default: // unbracketed IPv6 literal: no port can be expressed
		return net.JoinHostPort(addr, "6379"), nil
	}
}

func checkPort(addr, port string) error {
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid REDIS_ADDR %q: bad port", addr)
	}
	return nil
}
