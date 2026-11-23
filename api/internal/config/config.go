// Package config loads and validates api service configuration from environment
// variables, with an optional YAML file for values better expressed as structured
// config (see SPEC.md §3: "env vars + .yaml, validated at startup").
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

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

	// Attachment allowlist + max size (Decision 17), both meant to be
	// admin-configurable instance settings eventually; env-configured for
	// now with the defaults SPEC.md §7.2 names (images, PDF, common Office
	// formats, zip, plain text/code).
	AttachmentAllowlist    []string `yaml:"attachmentAllowlist"`
	AttachmentMaxSizeBytes int64    `yaml:"attachmentMaxSizeBytes"`
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
		cfg.AttachmentAllowlist = nil
		for _, t := range strings.Split(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				cfg.AttachmentAllowlist = append(cfg.AttachmentAllowlist, t)
			}
		}
	}
	if v := os.Getenv("ATTACHMENT_MAX_SIZE_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("ATTACHMENT_MAX_SIZE_BYTES must be an integer: %w", err)
		}
		cfg.AttachmentMaxSizeBytes = n
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
	if c.OIDCEnabled && (c.OIDCIssuer == "" || c.OIDCClientID == "" || c.OIDCRedirectURL == "") {
		// OIDC_CLIENT_SECRET is deliberately optional: the client is a public
		// client authenticating with PKCE alone.
		return fmt.Errorf("OIDC_ISSUER, OIDC_CLIENT_ID, and OIDC_REDIRECT_URL are all required when OIDC_ENABLED=true")
	}
	return nil
}
