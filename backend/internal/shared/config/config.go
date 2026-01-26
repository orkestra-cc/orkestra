package config

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	Auth      AuthConfig
	Rate      RateLimitConfig
	Billing   BillingConfig
	Documents DocumentsConfig
}

// BillingConfig holds configuration for the billing/invoicing module (OpenAPI SDI integration)
type BillingConfig struct {
	// OpenAPI SDI configuration
	OpenAPIBaseURL       string        // Base URL: https://sdi.openapi.it (prod) or https://test.sdi.openapi.it (sandbox)
	OpenAPIBearerToken   string        // OAuth Bearer Token for authentication
	OpenAPIFiscalID      string        // Company fiscal ID (P.IVA with country code, e.g., IT12345678901)
	OpenAPIRecipientCode string        // SDI recipient code (OpenAPI's code: JKKZDGR)
	ApplySignature       bool          // Enable digital signature for invoices
	ApplyStorage         bool          // Enable legal storage (conservazione sostitutiva)
	Timeout              time.Duration // HTTP client timeout
	RetryAttempts        int           // Number of retry attempts for failed requests
	PollingInterval      time.Duration // Interval between polling for notifications
	PollingEnabled       bool          // Enable automatic SDI polling (default: false to stay under API limits)
	SandboxMode          bool          // Use sandbox environment
}

// DocumentsConfig holds configuration for the documents/PDF generation module (Gotenberg integration)
type DocumentsConfig struct {
	GotenbergURL   string        // Gotenberg service URL (e.g., http://gotenberg:3000)
	Timeout        time.Duration // HTTP client timeout for PDF generation
	RetryAttempts  int           // Number of retry attempts for failed requests
	DefaultMargins PDFMargins    // Default page margins in millimeters
}

// PDFMargins defines page margins for PDF generation
type PDFMargins struct {
	Top    float64 // Top margin in millimeters
	Bottom float64 // Bottom margin in millimeters
	Left   float64 // Left margin in millimeters
	Right  float64 // Right margin in millimeters
}

type ServerConfig struct {
	Port            string
	Environment     string
	LogLevel        string
	FrontendURL     string
	CORSOrigins     []string // Allowed CORS origins
	MaxBodySize     int64    // Maximum request body size in bytes (default 10MB)
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
	Secret   string
	Name     string
	Domain   string
	HttpOnly bool
	Secure   bool
	SameSite string
	MaxAge   int
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
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found, using environment variables")
	}

	config := &Config{}

	// Default CORS origins for development
	defaultCORSOrigins := []string{"http://localhost:8080", "http://localhost:5173"}
	corsOrigins := getEnvAsSlice("CORS_ORIGINS", defaultCORSOrigins)

	config.Server = ServerConfig{
		Port:            getEnv("PORT", "3000"),
		Environment:     getEnv("ENV", "development"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		FrontendURL:     getEnv("FRONTEND_URL", "http://localhost:8080"),
		CORSOrigins:     corsOrigins,
		MaxBodySize:     getEnvAsInt64("MAX_BODY_SIZE", 10*1024*1024), // Default 10MB
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
			Secret:   getEnv("COOKIE_SECRET", "default-cookie-secret"),
			Name:     getEnv("COOKIE_NAME", "orkestra_cookie"),
			Domain:   getEnv("COOKIE_DOMAIN", ""),
			HttpOnly: getEnvAsBool("COOKIE_HTTP_ONLY", true),
			Secure:   getEnvAsBool("COOKIE_SECURE", false), // Default false for development
			SameSite: getEnv("COOKIE_SAME_SITE", "lax"),
			MaxAge:   getEnvAsInt("COOKIE_MAX_AGE", 86400000), // 24 hours in milliseconds
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

	// Billing/Invoicing configuration (OpenAPI SDI)
	sandboxMode := getEnvAsBool("OPENAPI_SANDBOX_MODE", true)
	defaultBaseURL := "https://test.sdi.openapi.it"
	if !sandboxMode {
		defaultBaseURL = "https://sdi.openapi.it"
	}

	config.Billing = BillingConfig{
		OpenAPIBaseURL:       getEnv("OPENAPI_BASE_URL", defaultBaseURL),
		OpenAPIBearerToken:   getEnv("OPENAPI_BEARER_TOKEN", ""),
		OpenAPIFiscalID:      getEnv("OPENAPI_FISCAL_ID", ""),
		OpenAPIRecipientCode: getEnv("OPENAPI_RECIPIENT_CODE", "JKKZDGR"),
		ApplySignature:       getEnvAsBool("OPENAPI_APPLY_SIGNATURE", true),
		ApplyStorage:         getEnvAsBool("OPENAPI_APPLY_STORAGE", true),
		Timeout:              getEnvAsDuration("OPENAPI_TIMEOUT", "30s"),
		RetryAttempts:        getEnvAsInt("OPENAPI_RETRY_ATTEMPTS", 3),
		PollingInterval:      getEnvAsDuration("OPENAPI_POLLING_INTERVAL", "12h"),
		PollingEnabled:       getEnvAsBool("OPENAPI_POLLING_ENABLED", true),
		SandboxMode:          sandboxMode,
	}

	// Documents/PDF generation configuration (Gotenberg)
	config.Documents = DocumentsConfig{
		GotenbergURL:  getEnv("GOTENBERG_URL", "http://gotenberg:3000"),
		Timeout:       getEnvAsDuration("GOTENBERG_TIMEOUT", "60s"),
		RetryAttempts: getEnvAsInt("GOTENBERG_RETRY_ATTEMPTS", 3),
		DefaultMargins: PDFMargins{
			Top:    20.0,
			Bottom: 20.0,
			Left:   20.0,
			Right:  20.0,
		},
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
