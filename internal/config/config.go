package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/WithAutonomi/indelible/internal/crypto"
	"github.com/WithAutonomi/indelible/internal/secrets"
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
	// JWTSecretsPrevious are verify-only secrets kept during a JWT-secret
	// rotation: tokens signed under a former secret keep validating until they
	// expire. New tokens are always signed with JWTSecret. See
	// docs/guides/key-rotation.md.
	JWTSecretsPrevious []string `toml:"jwt_secrets_previous"`
	Debug          bool     `toml:"debug"`
	CORSOrigins    []string `toml:"cors_allowed_origins"`
	TrustedProxies []string `toml:"trusted_proxies"`
	BaseURL             string   `toml:"base_url"` // External URL (e.g. https://files.acme.com)
	WalletEncryptionKey string   `toml:"wallet_encryption_key"` // Hex-encoded 32-byte AES key for wallet private keys
	// WalletEncryptionKeysPrevious are former wallet/OIDC encryption keys kept
	// decrypt-only during a rotation window, mirroring JWTSecretsPrevious: the
	// running service can read rows still encrypted under an old key until the
	// `rotate-keys` CLI has re-encrypted them. See docs/guides/key-rotation.md.
	WalletEncryptionKeysPrevious []string `toml:"wallet_encryption_keys_previous"`
	// SecretsBackend selects where key material is sourced from. Empty or "env"
	// (the default) sources from env / config-file / _FILE. Future values
	// (Vault, cloud KMS) plug in behind the secrets.Provider seam (V2-450).
	SecretsBackend string `toml:"secrets_backend"`

	// WorkersEnabled gates the background worker tier (upload processing, log
	// retention, disk alerts, audit anchoring, system monitor, idempotency
	// cleanup) and DB migrations. Default true = a normal all-in-one instance
	// or the single "writer" in a read/write role split (V2-515). Set false for
	// stateless "reader" replicas that only serve HTTP (downloads): they run no
	// workers, need no wallet, and skip migrations (the writer owns schema). See
	// the reader-fleet design in V2-513.
	WorkersEnabled bool `toml:"workers_enabled"`

	// Managed antd — spawn and monitor antd as a child process
	AntdManaged bool   `toml:"antd_managed"` // Spawn and manage antd (default: false)
	AntdBin     string `toml:"antd_bin"`     // Path to antd binary (default: "antd" — searches PATH)

	// EVM configuration — populated automatically from first PrepareUpload,
	// from the Network preset (see ApplyNetworkPreset), or set manually.
	// Explicit values always win over the preset.
	Network         string `toml:"network"`           // "arbitrum-one" (default), "arbitrum-sepolia", or "custom"
	EvmRPCURL       string `toml:"evm_rpc_url"`       // EVM RPC endpoint
	EvmTokenAddress string `toml:"evm_token_address"` // Payment token contract address

	// SMTP configuration for transactional emails (password reset, email verification)
	SMTP SMTPConfig `toml:"smtp"`

	// Bootstrap admin — seeded at startup when the instance has no admin yet.
	// This is how a fresh instance gets its first administrator: self-
	// registration is disabled by default (see the registration_enabled
	// setting) and never grants admin, so the first admin comes from here.
	// Set via INDELIBLE_ADMIN_EMAIL / INDELIBLE_ADMIN_PASSWORD (or
	// INDELIBLE_ADMIN_PASSWORD_FILE for Docker/K8s secrets).
	AdminEmail    string `toml:"admin_email"`
	AdminPassword string `toml:"admin_password"`

	// Resolved secrets seam, populated by Load (V2-450). secrets is the
	// Provider; walletKeyring and jwtKeyring are its pre-built keyrings cached
	// so request-path accessors never fail.
	secrets       secrets.Provider
	walletKeyring *crypto.Keyring
	jwtKeyring    *crypto.Keyring
}

// Secrets returns the configured secrets Provider (V2-450). Use it to fetch a
// keyring by logical name (e.g. secrets.WalletEncryption).
func (c *Config) Secrets() secrets.Provider { return c.secrets }

// WalletKeyring returns the active wallet/OIDC encryption keyring (active key +
// decrypt-only history). Load pre-builds it via the provider. As a fallback for
// a Config assembled directly (e.g. in tests, bypassing Load), it builds a
// fresh keyring from the raw fields — pure, so concurrent callers don't race.
func (c *Config) WalletKeyring() *crypto.Keyring {
	if c.walletKeyring != nil {
		return c.walletKeyring
	}
	kr, _ := crypto.NewKeyring(c.WalletEncryptionKey, c.WalletEncryptionKeysPrevious...)
	return kr
}

// JWTKeyring returns the JWT signing keyring (active secret + verify-only
// history). Sign with Primary(); verify against Primary()+Previous(). Same
// Load-pre-built / direct-construction fallback as WalletKeyring.
func (c *Config) JWTKeyring() *crypto.Keyring {
	if c.jwtKeyring != nil {
		return c.jwtKeyring
	}
	kr, _ := crypto.NewKeyringRaw(c.JWTSecret, c.JWTSecretsPrevious...)
	return kr
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

// Network identifiers for ApplyNetworkPreset.
const (
	NetworkArbitrumOne     = "arbitrum-one"
	NetworkArbitrumSepolia = "arbitrum-sepolia"
	NetworkCustom          = "custom"
	// NetworkLocal is antd's name for a local devnet. It is accepted as an
	// alias for "custom" so the two components share the same network vocab —
	// a local antd devnet uses `--network local`, and pointing Indelible at it
	// with INDELIBLE_NETWORK=local should not be rejected.
	NetworkLocal = "local"
)

// Preset values mirror the canonical constants in autonomi/evmlib/src/lib.rs
// so Indelible reads/writes the same on-chain state as every other Autonomi
// component pointed at the same network.
const (
	arbitrumOneRPCURL       = "https://arb1.arbitrum.io/rpc"
	arbitrumOneTokenAddress = "0xa78d8321B20c4Ef90eCd72f2588AA985A4BDb684"

	arbitrumSepoliaRPCURL       = "https://sepolia-rollup.arbitrum.io/rpc"
	arbitrumSepoliaTokenAddress = "0x4bc1aCE0E66170375462cB4E6Af42Ad4D5EC689C"
)

// ApplyNetworkPreset fills EvmRPCURL and EvmTokenAddress from the named
// Network when they were not set explicitly. Explicit values (TOML, env, or
// previously assigned) always win. An empty Network defaults to mainnet
// ("arbitrum-one"); "custom" leaves the EVM fields untouched so the
// upload-time auto-population path can fill them later.
func (c *Config) ApplyNetworkPreset() error {
	if c.Network == "" {
		c.Network = NetworkArbitrumOne
	}
	// "local" (antd's term for a local devnet) is an alias for "custom"; fold
	// it in so downstream readers see a single canonical value.
	if c.Network == NetworkLocal {
		c.Network = NetworkCustom
	}
	var rpc, token string
	switch c.Network {
	case NetworkArbitrumOne:
		rpc, token = arbitrumOneRPCURL, arbitrumOneTokenAddress
	case NetworkArbitrumSepolia:
		rpc, token = arbitrumSepoliaRPCURL, arbitrumSepoliaTokenAddress
	case NetworkCustom:
		return nil
	default:
		return fmt.Errorf("unknown network %q (expected %q, %q, %q, or %q)",
			c.Network, NetworkArbitrumOne, NetworkArbitrumSepolia, NetworkCustom, NetworkLocal)
	}
	if c.EvmRPCURL == "" {
		c.EvmRPCURL = rpc
	}
	if c.EvmTokenAddress == "" {
		c.EvmTokenAddress = token
	}
	return nil
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
		Port:           8080,
		DBURL:          "sqlite:///var/lib/indelible/data.db",
		AntdURL:        "http://localhost:8082",
		DataDir:        "/var/lib/indelible",
		WorkersEnabled: true, // default = run workers (all-in-one / writer role)
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
	// _FILE variant (Docker / K8s secrets) takes precedence over the inline var
	// so the signing secret need never sit in the compose file.
	if v := os.Getenv("INDELIBLE_JWT_SECRET_FILE"); v != "" {
		b, err := os.ReadFile(v)
		if err != nil {
			return nil, fmt.Errorf("reading INDELIBLE_JWT_SECRET_FILE: %w", err)
		}
		cfg.JWTSecret = strings.TrimSpace(string(b))
	}
	if v := os.Getenv("INDELIBLE_JWT_SECRET_PREVIOUS"); v != "" {
		cfg.JWTSecretsPrevious = nil
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				cfg.JWTSecretsPrevious = append(cfg.JWTSecretsPrevious, s)
			}
		}
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
	if v := os.Getenv("INDELIBLE_WALLET_ENCRYPTION_KEY"); v != "" {
		cfg.WalletEncryptionKey = v
	}
	// _FILE variant (Docker / K8s secrets), as for the admin password and JWT secret.
	if v := os.Getenv("INDELIBLE_WALLET_ENCRYPTION_KEY_FILE"); v != "" {
		b, err := os.ReadFile(v)
		if err != nil {
			return nil, fmt.Errorf("reading INDELIBLE_WALLET_ENCRYPTION_KEY_FILE: %w", err)
		}
		cfg.WalletEncryptionKey = strings.TrimSpace(string(b))
	}
	if v := os.Getenv("INDELIBLE_WALLET_ENCRYPTION_KEY_PREVIOUS"); v != "" {
		cfg.WalletEncryptionKeysPrevious = nil
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				cfg.WalletEncryptionKeysPrevious = append(cfg.WalletEncryptionKeysPrevious, s)
			}
		}
	}
	if v := os.Getenv("INDELIBLE_SECRETS_BACKEND"); v != "" {
		cfg.SecretsBackend = v
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
	if v := os.Getenv("INDELIBLE_ADMIN_EMAIL"); v != "" {
		cfg.AdminEmail = strings.TrimSpace(strings.ToLower(v))
	}
	if v := os.Getenv("INDELIBLE_ADMIN_PASSWORD"); v != "" {
		cfg.AdminPassword = v
	}
	// _FILE variant (Docker / K8s secrets) takes precedence over the inline
	// var so the bootstrap password need never sit in the compose file.
	if v := os.Getenv("INDELIBLE_ADMIN_PASSWORD_FILE"); v != "" {
		b, err := os.ReadFile(v)
		if err != nil {
			return nil, fmt.Errorf("reading INDELIBLE_ADMIN_PASSWORD_FILE: %w", err)
		}
		cfg.AdminPassword = strings.TrimSpace(string(b))
	}
	// Default-true flag: only an explicit false/0 disables the worker tier, so a
	// reader replica opts out with INDELIBLE_WORKERS_ENABLED=false; any unset or
	// truthy value keeps the default (workers on).
	if v := os.Getenv("INDELIBLE_WORKERS_ENABLED"); v != "" {
		cfg.WorkersEnabled = v == "true" || v == "1"
	}
	if v := os.Getenv("INDELIBLE_ANTD_MANAGED"); v != "" {
		cfg.AntdManaged = v == "true" || v == "1"
	}
	if v := os.Getenv("INDELIBLE_ANTD_BIN"); v != "" {
		cfg.AntdBin = v
	}

	if v := os.Getenv("INDELIBLE_NETWORK"); v != "" {
		cfg.Network = v
	}
	if v := os.Getenv("INDELIBLE_EVM_RPC_URL"); v != "" {
		cfg.EvmRPCURL = v
	}
	if v := os.Getenv("INDELIBLE_EVM_TOKEN_ADDRESS"); v != "" {
		cfg.EvmTokenAddress = v
	}

	// Default antd binary
	if cfg.AntdBin == "" {
		cfg.AntdBin = "antd"
	}

	// Default SMTP port
	if cfg.SMTP.Host != "" && cfg.SMTP.Port == 0 {
		cfg.SMTP.Port = 587
	}

	// Require JWT secret with adequate entropy. HMAC-SHA256 signing security
	// rests entirely on the secret's strength (verification already rejects
	// non-HMAC algs), so a short/guessable secret is forgeable. Enforce a floor
	// of 32 bytes, mirroring the wallet-key UX below.
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("jwt_secret is required (set INDELIBLE_JWT_SECRET or jwt_secret in config); generate with: openssl rand -hex 32")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("jwt_secret must be at least 32 characters (got %d); generate with: openssl rand -hex 32", len(cfg.JWTSecret))
	}
	// Previous (verify-only) secrets must clear the same entropy floor — a token
	// signed under a weak former secret is just as forgeable while it lingers in
	// the rotation window.
	for i, prev := range cfg.JWTSecretsPrevious {
		if len(prev) < 32 {
			return nil, fmt.Errorf("jwt_secrets_previous[%d] must be at least 32 characters (got %d); set INDELIBLE_JWT_SECRET_PREVIOUS only to former jwt_secret values", i, len(prev))
		}
	}

	// Require wallet encryption key
	if cfg.WalletEncryptionKey == "" || cfg.WalletEncryptionKey == "0000000000000000000000000000000000000000000000000000000000000000" {
		return nil, fmt.Errorf("wallet_encryption_key is required (set INDELIBLE_WALLET_ENCRYPTION_KEY or wallet_encryption_key in config); generate with: openssl rand -hex 32")
	}

	// Build the secrets provider and cache its keyrings (V2-450). This also
	// validates the wallet keys are well-formed hex, surfacing a bad key (or an
	// unimplemented backend) at startup rather than on the first request.
	provider, err := secrets.NewProvider(cfg.SecretsBackend, secrets.EnvConfig{
		WalletKey:          cfg.WalletEncryptionKey,
		WalletKeysPrevious: cfg.WalletEncryptionKeysPrevious,
		JWTSecret:          cfg.JWTSecret,
		JWTSecretsPrevious: cfg.JWTSecretsPrevious,
	})
	if err != nil {
		return nil, err
	}
	cfg.secrets = provider
	if cfg.walletKeyring, err = provider.Keyring(secrets.WalletEncryption); err != nil {
		return nil, err
	}
	if cfg.jwtKeyring, err = provider.Keyring(secrets.JWT); err != nil {
		return nil, err
	}

	return cfg, nil
}
