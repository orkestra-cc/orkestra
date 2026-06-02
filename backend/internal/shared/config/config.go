package config

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Auth     AuthConfig
	Rate     RateLimitConfig
	Storage  StorageConfig
}

// StorageConfig holds the S3-compatible object-storage connection
// parameters consumed by internal/shared/blob. Process-scoped (read
// once at boot) because rotating credentials at runtime would
// invalidate every in-flight presigned URL — storage stays out of the
// admin-UI ConfigService bucket. Default endpoint targets the rustfs
// service in docker-compose.infra.yml; production deploys swap to AWS
// S3 / managed equivalent.
type StorageConfig struct {
	Endpoint       string // e.g. http://orkestra-rustfs:9000 (empty = AWS S3 default endpoint resolution)
	Region         string // e.g. us-east-1 — placeholder for RustFS, real region for AWS S3
	Bucket         string // e.g. orkestra-avatars
	AccessKey      string
	SecretKey      string
	ForcePathStyle bool // true for RustFS / MinIO; false for AWS S3 virtual-hosted style
	EnsureBucket   bool // true → backend creates the bucket on boot if missing; safe for self-hosted
}

type ServerConfig struct {
	Port        string
	Environment string
	LogLevel    string
	FrontendURL string
	CORSOrigins []string // Allowed CORS origins (legacy single-host fallback)
	MaxBodySize int64    // Maximum request body size in bytes (default 10MB)

	// ADR-0003 per-audience host split. Both audiences are served from the
	// same Go binary, dispatched by Host header at the application layer.
	// Empty Host disables that audience's mux (the host mux returns 421 for
	// requests targeting it). Empty CORS / Rate falls back to the legacy
	// CORSOrigins / Rate values so deployments that haven't set the new
	// env vars keep their current behaviour.
	Operator AudienceConfig
	Client   AudienceConfig
}

// AudienceConfig groups the per-audience routing settings introduced by
// ADR-0003 PR-C: the public hostname the audience answers on, the CORS
// allowlist for cross-origin browser calls, and the rate-limit policy.
// Populated from ${AUDIENCE_PREFIX}_HOST / ${AUDIENCE_PREFIX}_CORS_ORIGINS
// / ${AUDIENCE_PREFIX}_RATE_LIMIT_* env vars.
type AudienceConfig struct {
	Host        string          // e.g. "console.orkestra.com" — empty disables this audience's mux
	CORSOrigins []string        // empty falls back to ServerConfig.CORSOrigins
	Rate        RateLimitConfig // zero values fall back to top-level Rate
	// FrontendURL is the public origin of the SPA serving this audience —
	// used to build verification + reset links in transactional email so a
	// signup on the client SPA gets a verify URL on the client host (not
	// the operator console). Empty falls back to ServerConfig.FrontendURL.
	FrontendURL string
}

type DatabaseConfig struct {
	MongoURI        string
	DatabaseName    string
	MaxPoolSize     uint64
	MinPoolSize     uint64
	MaxConnIdleTime time.Duration
	ConnectTimeout  time.Duration
}

type RedisConfig struct {
	URL             string
	MaxRetries      int
	MinIdleConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
}

type AuthConfig struct {
	JWT                     JWTConfig
	Cookie                  CookieConfig
	Google                  GoogleOAuthConfig
	Apple                   AppleOAuthConfig
	Discord                 DiscordOAuthConfig
	GitHub                  GitHubOAuthConfig
	AllowLocalhostRedirects bool // Allow localhost OAuth redirects (should be false in production)
}

type JWTConfig struct {
	PrivateKeyPath     string
	PublicKeyPath      string
	PrivateKey         *rsa.PrivateKey
	PublicKey          *rsa.PublicKey
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration
	KeysLoaded         bool // Indicates if JWT keys were successfully loaded
}

type CookieConfig struct {
	Secret string
	Name   string
	// Domain is the legacy single-tier cookie domain (`COOKIE_DOMAIN`).
	// Kept as the fallback when the audience-specific values below are
	// empty so single-host deployments keep working without changes.
	// Per-audience deployments (ADR-0003 PR-D D-9) should leave this
	// empty and set OperatorDomain / ClientDomain instead.
	Domain string
	// OperatorDomain scopes refresh-token cookies minted on the operator
	// host (`console.*`) — set via `OPERATOR_COOKIE_DOMAIN`. Empty falls
	// back to Domain.
	OperatorDomain string
	// ClientDomain scopes refresh-token cookies minted on the client
	// host (`api.*`) — set via `CLIENT_COOKIE_DOMAIN`. Empty falls back
	// to Domain.
	ClientDomain string
	HttpOnly     bool
	Secure       bool
	SameSite     string
	MaxAge       int
}

type GoogleOAuthConfig struct {
	ClientID        string
	ClientSecret    string
	RedirectURL     string
	AndroidClientID string
	IOSClientID     string
}

type AppleOAuthConfig struct {
	TeamID          string
	ClientID        string
	KeyID           string
	PrivateKey      string // Direct PEM key content
	PrivateKeyPath  string // Path to PEM key file
	RedirectURL     string
	IOSClientID     string
	AndroidClientID string
}

type DiscordOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type GitHubOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type RateLimitConfig struct {
	RequestsPerMinute int
	Burst             int
}

func Load() (*Config, error) {
	// `.env` is a local-dev convenience. In containers the runtime
	// injects env vars directly, so a missing file is the normal case —
	// quiet debug log instead of stdout noise.
	if err := godotenv.Load(); err != nil {
		slog.Debug("no .env file found, using process environment", slog.String("error", err.Error()))
	}

	config := &Config{}

	// Default CORS origins for development
	defaultCORSOrigins := []string{"http://localhost:8080", "http://localhost:5173"}
	corsOrigins := getEnvAsSlice("CORS_ORIGINS", defaultCORSOrigins)

	// ADR-0003 per-audience defaults. Dev defaults use *.localhost which
	// resolves to 127.0.0.1 on most modern OSes (Linux, macOS via mDNS,
	// Windows since 10) so contributors don't need to edit /etc/hosts.
	// Prod defaults are intentionally left empty — operators must set
	// CONSOLE_HOST / CLIENT_API_HOST explicitly so a misconfigured deploy
	// fails the host-mux check rather than serving the wrong audience.
	env := getEnv("ENV", "development")
	defaultConsoleHost := ""
	defaultClientHost := ""
	if env == "development" {
		defaultConsoleHost = "console.localhost:3000"
		defaultClientHost = "api.localhost:3000"
	}

	config.Server = ServerConfig{
		Port:        getEnv("PORT", "3000"),
		Environment: env,
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		FrontendURL: getEnv("FRONTEND_URL", "http://localhost:8080"),
		CORSOrigins: corsOrigins,
		MaxBodySize: getEnvAsInt64("MAX_BODY_SIZE", 10*1024*1024), // Default 10MB
		Operator: AudienceConfig{
			Host:        getEnv("CONSOLE_HOST", defaultConsoleHost),
			CORSOrigins: getEnvAsSlice("OPERATOR_CORS_ORIGINS", nil),
			Rate: RateLimitConfig{
				RequestsPerMinute: getEnvAsInt("OPERATOR_RATE_LIMIT_REQUESTS_PER_MINUTE", 0),
				Burst:             getEnvAsInt("OPERATOR_RATE_LIMIT_BURST", 0),
			},
			FrontendURL: getEnv("OPERATOR_FRONTEND_URL", ""),
		},
		Client: AudienceConfig{
			Host:        getEnv("CLIENT_API_HOST", defaultClientHost),
			CORSOrigins: getEnvAsSlice("CLIENT_CORS_ORIGINS", nil),
			Rate: RateLimitConfig{
				RequestsPerMinute: getEnvAsInt("CLIENT_RATE_LIMIT_REQUESTS_PER_MINUTE", 0),
				Burst:             getEnvAsInt("CLIENT_RATE_LIMIT_BURST", 0),
			},
			FrontendURL: getEnv("CLIENT_FRONTEND_URL", ""),
		},
	}

	config.Database = DatabaseConfig{
		MongoURI:        getEnv("MONGO_URI", "mongodb://localhost:27017/orkestra"),
		DatabaseName:    getEnv("MONGO_DATABASE", "orkestra"),
		MaxPoolSize:     getEnvAsUint64("MONGO_MAX_POOL_SIZE", 100),
		MinPoolSize:     getEnvAsUint64("MONGO_MIN_POOL_SIZE", 10),
		MaxConnIdleTime: getEnvAsDuration("MONGO_MAX_CONN_IDLE_TIME", "5m"),
		ConnectTimeout:  getEnvAsDuration("MONGO_CONNECT_TIMEOUT", "10s"),
	}

	config.Redis = RedisConfig{
		URL:             getEnv("REDIS_URL", "redis://localhost:6379"),
		MaxRetries:      getEnvAsInt("REDIS_MAX_RETRIES", 3),
		MinIdleConns:    getEnvAsInt("REDIS_MIN_IDLE_CONNS", 5),
		MaxIdleConns:    getEnvAsInt("REDIS_MAX_IDLE_CONNS", 10),
		ConnMaxLifetime: getEnvAsDuration("REDIS_CONN_MAX_LIFETIME", "0"),
		ReadTimeout:     getEnvAsDuration("REDIS_READ_TIMEOUT", "3s"),
		WriteTimeout:    getEnvAsDuration("REDIS_WRITE_TIMEOUT", "3s"),
	}

	jwtConfig := JWTConfig{
		PrivateKeyPath:     getEnv("JWT_PRIVATE_KEY_PATH", ""),
		PublicKeyPath:      getEnv("JWT_PUBLIC_KEY_PATH", ""),
		AccessTokenExpiry:  getEnvAsDuration("JWT_ACCESS_TOKEN_EXPIRY", "15m"),
		RefreshTokenExpiry: getEnvAsDuration("JWT_REFRESH_TOKEN_EXPIRY", "7d"),
	}

	// Load RSA keys (non-fatal in development)
	if err := loadJWTKeys(&jwtConfig); err != nil {
		printJWTWarning(err)
		jwtConfig.KeysLoaded = false
	} else {
		jwtConfig.KeysLoaded = true
	}

	config.Auth = AuthConfig{
		JWT: jwtConfig,
		Cookie: CookieConfig{
			Secret: getEnv("COOKIE_SECRET", "default-cookie-secret"),
			Name:   getEnv("COOKIE_NAME", "orkestra_cookie"),
			Domain: getEnv("COOKIE_DOMAIN", ""),
			// ADR-0003 PR-D D-9: per-audience cookie domains. Dev defaults
			// align with the per-audience host defaults above so the
			// browser scopes refresh cookies to the matching subdomain
			// without contributors having to set anything. Prod defaults
			// are left empty — operators set them explicitly so a stale
			// COOKIE_DOMAIN does not accidentally cross the audiences.
			OperatorDomain: getEnv("OPERATOR_COOKIE_DOMAIN", defaultOperatorCookieDomain(env)),
			ClientDomain:   getEnv("CLIENT_COOKIE_DOMAIN", defaultClientCookieDomain(env)),
			HttpOnly:       getEnvAsBool("COOKIE_HTTP_ONLY", true),
			Secure:         getEnvAsBool("COOKIE_SECURE", false), // Default false for development
			SameSite:       getEnv("COOKIE_SAME_SITE", "lax"),
			MaxAge:         getEnvAsInt("COOKIE_MAX_AGE", 86400000), // 24 hours in milliseconds
		},
		Google: GoogleOAuthConfig{
			ClientID:        getEnv("OAUTH_GOOGLE_CLIENT_ID", ""),
			ClientSecret:    getEnv("OAUTH_GOOGLE_CLIENT_SECRET", ""),
			RedirectURL:     getEnv("OAUTH_GOOGLE_REDIRECT_URL", "http://localhost:3000/auth/oauth/google/callback"),
			AndroidClientID: getEnv("OAUTH_GOOGLE_ANDROID_CLIENT_ID", ""),
			IOSClientID:     getEnv("OAUTH_GOOGLE_IOS_CLIENT_ID", ""),
		},
		Apple: AppleOAuthConfig{
			TeamID:          getEnv("OAUTH_APPLE_TEAM_ID", ""),
			ClientID:        getEnv("OAUTH_APPLE_CLIENT_ID", ""),
			KeyID:           getEnv("OAUTH_APPLE_KEY_ID", ""),
			PrivateKey:      getEnv("OAUTH_APPLE_PRIVATE_KEY", ""),
			PrivateKeyPath:  getEnv("OAUTH_APPLE_PRIVATE_KEY_PATH", ""),
			RedirectURL:     getEnv("OAUTH_APPLE_REDIRECT_URL", "http://localhost:3000/auth/oauth/apple/callback"),
			IOSClientID:     getEnv("OAUTH_APPLE_IOS_CLIENT_ID", ""),
			AndroidClientID: getEnv("OAUTH_APPLE_ANDROID_CLIENT_ID", ""),
		},
		Discord: DiscordOAuthConfig{
			ClientID:     getEnv("OAUTH_DISCORD_CLIENT_ID", ""),
			ClientSecret: getEnv("OAUTH_DISCORD_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("OAUTH_DISCORD_REDIRECT_URL", "http://localhost:3000/auth/oauth/discord/callback"),
		},
		GitHub: GitHubOAuthConfig{
			ClientID:     getEnv("OAUTH_GITHUB_CLIENT_ID", ""),
			ClientSecret: getEnv("OAUTH_GITHUB_CLIENT_SECRET", ""),
			RedirectURL:  getEnv("OAUTH_GITHUB_REDIRECT_URL", "http://localhost:3000/auth/oauth/github/callback"),
		},
		AllowLocalhostRedirects: getEnvAsBool("ALLOW_LOCALHOST_REDIRECTS", true), // Default true for development
	}

	config.Rate = RateLimitConfig{
		RequestsPerMinute: getEnvAsInt("RATE_LIMIT_REQUESTS_PER_MINUTE", 60),
		Burst:             getEnvAsInt("RATE_LIMIT_BURST", 10),
	}

	// Object storage configuration (RustFS default — S3-compatible).
	// Defaults below match docker-compose.infra.yml's rustfs service so
	// `docker compose -f infra.yml up -d` + a fresh boot just works.
	config.Storage = StorageConfig{
		Endpoint:       getEnv("STORAGE_ENDPOINT", "http://orkestra-rustfs:9000"),
		Region:         getEnv("STORAGE_REGION", "us-east-1"),
		Bucket:         getEnv("STORAGE_BUCKET", "orkestra-avatars"),
		AccessKey:      getEnv("STORAGE_ACCESS_KEY", ""),
		SecretKey:      getEnv("STORAGE_SECRET_KEY", ""),
		ForcePathStyle: getEnvAsBool("STORAGE_FORCE_PATH_STYLE", true),
		EnsureBucket:   getEnvAsBool("STORAGE_ENSURE_BUCKET", true),
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

func (c *Config) Validate() error {
	// Production/Staging security validations
	if c.IsProductionLike() {
		// JWT keys are REQUIRED in production
		if !c.Auth.JWT.KeysLoaded {
			return fmt.Errorf("JWT keys are required in production - set JWT_PRIVATE_KEY_PATH and JWT_PUBLIC_KEY_PATH")
		}

		// Cookie security is REQUIRED in production
		if !c.Auth.Cookie.Secure {
			return fmt.Errorf("COOKIE_SECURE must be true in production/staging environments")
		}

		// Localhost redirects must be disabled in production
		if c.Auth.AllowLocalhostRedirects {
			return fmt.Errorf("ALLOW_LOCALHOST_REDIRECTS must be false in production/staging environments")
		}

		// OAuth is required in production
		if c.Auth.Google.ClientID == "" {
			return fmt.Errorf("OAUTH_GOOGLE_CLIENT_ID is required in production")
		}
		if c.Auth.Google.ClientSecret == "" {
			return fmt.Errorf("OAUTH_GOOGLE_CLIENT_SECRET is required in production")
		}
	}

	return nil
}

// printJWTWarning prints a prominent warning when JWT keys are not loaded
func printJWTWarning(err error) {
	warning := `
╔══════════════════════════════════════════════════════════════════════════════╗
║                                                                              ║
║   ██╗    ██╗ █████╗ ██████╗ ███╗   ██╗██╗███╗   ██╗ ██████╗                  ║
║   ██║    ██║██╔══██╗██╔══██╗████╗  ██║██║████╗  ██║██╔════╝                  ║
║   ██║ █╗ ██║███████║██████╔╝██╔██╗ ██║██║██╔██╗ ██║██║  ███╗                 ║
║   ██║███╗██║██╔══██║██╔══██╗██║╚██╗██║██║██║╚██╗██║██║   ██║                 ║
║   ╚███╔███╔╝██║  ██║██║  ██║██║ ╚████║██║██║ ╚████║╚██████╔╝                 ║
║    ╚══╝╚══╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═══╝╚═╝╚═╝  ╚═══╝ ╚═════╝                  ║
║                                                                              ║
║   JWT KEYS NOT LOADED - AUTHENTICATION WILL NOT WORK!                        ║
║                                                                              ║
║   Error: %s
║                                                                              ║
║   To fix this, generate JWT keys:                                            ║
║     openssl genrsa -out docker/keys/jwt-private.pem 4096                     ║
║     openssl rsa -in docker/keys/jwt-private.pem -pubout \                    ║
║       -out docker/keys/jwt-public.pem                                        ║
║                                                                              ║
║   Server will continue running, but auth endpoints will fail.                ║
║                                                                              ║
╚══════════════════════════════════════════════════════════════════════════════╝
`
	fmt.Printf(warning, err)
}

// defaultOperatorCookieDomain returns the dev fallback for the operator
// refresh-cookie scope. Empty in non-dev so a misconfigured prod deploy
// fails closed (the cookie is set without a Domain attribute, scoped to
// whatever host minted it) rather than silently leaking across hosts.
func defaultOperatorCookieDomain(env string) string {
	if env == "development" {
		return "console.localhost"
	}
	return ""
}

// defaultClientCookieDomain mirrors defaultOperatorCookieDomain for the
// client surface (api.*).
func defaultClientCookieDomain(env string) string {
	if env == "development" {
		return "api.localhost"
	}
	return ""
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsUint64(key string, defaultValue uint64) uint64 {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseUint(valueStr, 10, 64); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsInt64(key string, defaultValue int64) int64 {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseInt(valueStr, 10, 64); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue string) time.Duration {
	valueStr := getEnv(key, defaultValue)
	if value, err := time.ParseDuration(valueStr); err == nil {
		return value
	}
	if defaultDuration, err := time.ParseDuration(defaultValue); err == nil {
		return defaultDuration
	}
	return 0
}

func getEnvAsSlice(key string, defaultValue []string) []string {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}

	values := []string{}
	for _, v := range strings.Split(valueStr, ",") {
		values = append(values, strings.TrimSpace(v))
	}
	return values
}

func (c *Config) IsDevelopment() bool {
	return c.Server.Environment == "development"
}

func (c *Config) IsProduction() bool {
	return c.Server.Environment == "production"
}

func (c *Config) IsStaging() bool {
	return c.Server.Environment == "staging"
}

// IsProductionLike returns true for staging and production environments.
// Use this for security, logging, and behavior that should match production.
func (c *Config) IsProductionLike() bool {
	return c.Server.Environment == "production" || c.Server.Environment == "staging"
}

func (c *Config) GetEnvironment() string {
	return c.Server.Environment
}

// FrontendURL returns the public origin of the SPA. Used by notification
// templates and password-reset/verification links. Satisfies the
// module.PlatformInfo SDK contract so addons can read this without
// importing shared/config.
func (c *Config) FrontendURL() string {
	return c.Server.FrontendURL
}

// loadJWTKeys loads RSA keys from the file system
func loadJWTKeys(jwt *JWTConfig) error {
	if jwt.PrivateKeyPath == "" {
		return fmt.Errorf("JWT_PRIVATE_KEY_PATH is required")
	}
	if jwt.PublicKeyPath == "" {
		return fmt.Errorf("JWT_PUBLIC_KEY_PATH is required")
	}

	// Load private key
	privateKeyData, err := ioutil.ReadFile(jwt.PrivateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read private key file %s: %w", jwt.PrivateKeyPath, err)
	}

	privateKeyBlock, _ := pem.Decode(privateKeyData)
	if privateKeyBlock == nil {
		return fmt.Errorf("failed to parse PEM block containing private key")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(privateKeyBlock.Bytes)
	if err != nil {
		// Try PKCS#8 format if PKCS#1 fails
		privateKeyInterface, err := x509.ParsePKCS8PrivateKey(privateKeyBlock.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse private key: %w", err)
		}
		var ok bool
		privateKey, ok = privateKeyInterface.(*rsa.PrivateKey)
		if !ok {
			return fmt.Errorf("private key is not an RSA key")
		}
	}

	// Load public key
	publicKeyData, err := ioutil.ReadFile(jwt.PublicKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key file %s: %w", jwt.PublicKeyPath, err)
	}

	publicKeyBlock, _ := pem.Decode(publicKeyData)
	if publicKeyBlock == nil {
		return fmt.Errorf("failed to parse PEM block containing public key")
	}

	publicKeyInterface, err := x509.ParsePKIXPublicKey(publicKeyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse public key: %w", err)
	}

	publicKey, ok := publicKeyInterface.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("public key is not an RSA key")
	}

	// Verify key pair matches
	if privateKey.PublicKey.N.Cmp(publicKey.N) != 0 || privateKey.PublicKey.E != publicKey.E {
		return fmt.Errorf("private and public keys do not match")
	}

	jwt.PrivateKey = privateKey
	jwt.PublicKey = publicKey

	return nil
}
