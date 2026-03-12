package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds all application configuration. Values can be set via
// config file (TOML) or environment variables (INDELIBLE_ prefix).
// Environment variables take precedence over file values.
type Config struct {
	Port           int      `toml:"port"`
	DBURL          string   `toml:"db_url"`
	AntdURL        string   `toml:"antd_url"`
	DataDir        string   `toml:"data_dir"`
	JWTSecret      string   `toml:"jwt_secret"`
	Debug          bool     `toml:"debug"`
	CORSOrigins    []string `toml:"cors_allowed_origins"`
	TrustedProxies []string `toml:"trusted_proxies"`
	BaseURL        string   `toml:"base_url"` // External URL (e.g. https://files.acme.com)

	// SMTP configuration for transactional emails (password reset, email verification)
	SMTP SMTPConfig `toml:"smtp"`
}

type SMTPConfig struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	Username string `toml:"username"`
	Password string `toml:"password"`
	From     string `toml:"from"`     // Sender address (e.g. noreply@acme.com)
	UseTLS   bool   `toml:"use_tls"`  // STARTTLS
}

// SMTPConfigured returns true if SMTP is configured enough to send mail.
func (c *Config) SMTPConfigured() bool {
	return c.SMTP.Host != "" && c.SMTP.From != ""
}

// DBDriver returns "sqlite" or "postgres" based on the DB URL.
func (c *Config) DBDriver() string {
	if strings.HasPrefix(c.DBURL, "postgres") {
		return "postgres"
	}
	return "sqlite"
}

// Load reads configuration from an optional TOML file and overlays
// environment variables. Defaults are applied for any unset values.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Port:    8080,
		DBURL:   "sqlite:///var/lib/indelible/data.db",
		AntdURL: "http://localhost:8081",
		DataDir: "/var/lib/indelible",
	}

	// Load from TOML file if provided
	if path != "" {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, fmt.Errorf("reading config %s: %w", path, err)
		}
	}

	// Environment variable overrides
	if v := os.Getenv("INDELIBLE_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid INDELIBLE_PORT: %w", err)
		}
		cfg.Port = port
	}
	if v := os.Getenv("INDELIBLE_DB_URL"); v != "" {
		cfg.DBURL = v
	}
	if v := os.Getenv("INDELIBLE_ANTD_URL"); v != "" {
		cfg.AntdURL = v
	}
	if v := os.Getenv("INDELIBLE_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("INDELIBLE_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("INDELIBLE_DEBUG"); v != "" {
		cfg.Debug = v == "true" || v == "1"
	}
	if v := os.Getenv("INDELIBLE_CORS_ORIGINS"); v != "" {
		cfg.CORSOrigins = strings.Split(v, ",")
	}
	if v := os.Getenv("INDELIBLE_TRUSTED_PROXIES"); v != "" {
		cfg.TrustedProxies = strings.Split(v, ",")
	}
	if v := os.Getenv("INDELIBLE_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("INDELIBLE_SMTP_HOST"); v != "" {
		cfg.SMTP.Host = v
	}
	if v := os.Getenv("INDELIBLE_SMTP_PORT"); v != "" {
		p, _ := strconv.Atoi(v)
		cfg.SMTP.Port = p
	}
	if v := os.Getenv("INDELIBLE_SMTP_USERNAME"); v != "" {
		cfg.SMTP.Username = v
	}
	if v := os.Getenv("INDELIBLE_SMTP_PASSWORD"); v != "" {
		cfg.SMTP.Password = v
	}
	if v := os.Getenv("INDELIBLE_SMTP_FROM"); v != "" {
		cfg.SMTP.From = v
	}
	if v := os.Getenv("INDELIBLE_SMTP_USE_TLS"); v != "" {
		cfg.SMTP.UseTLS = v == "true" || v == "1"
	}

	// Default SMTP port
	if cfg.SMTP.Host != "" && cfg.SMTP.Port == 0 {
		cfg.SMTP.Port = 587
	}

	// Generate JWT secret if not set
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("jwt_secret is required (set INDELIBLE_JWT_SECRET or jwt_secret in config)")
	}

	return cfg, nil
}
