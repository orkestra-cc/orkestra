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
	"github.com/orkestra/backend/internal/shared/utils"
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
	Endpoint       string // e.g. http://rustfs:9000 (compose SERVICE name, stable across stacks; empty = AWS S3 default endpoint resolution)
	PublicEndpoint string // browser-reachable endpoint baked into presigned URLs; empty = use Endpoint. Set when Endpoint is a docker-internal host the SPA can't reach (e.g. RustFS behind a public proxy) — see blob.S3Config.PublicEndpoint
	Region         string // e.g. us-east-1 — placeholder for RustFS, real region for AWS S3
	Bucket         string // DEPRECATED single bucket. Superseded by BucketPrefix + per-domain buckets (<prefix>-<domain>). Kept for config compat; honored only to warn on a custom value.
	AccessKey      string
	SecretKey      string
	ForcePathStyle bool   // true for RustFS / MinIO; false for AWS S3 virtual-hosted style
	EnsureBucket   bool   // true → backend creates the bucket on boot if missing; safe for self-hosted
	BucketPrefix   string // per-domain bucket namespace; bucket = <BucketPrefix>-<domain> (e.g. orkestra-avatars, orkestra-crm-photos)
	// CORSAllowedOrigins are the browser origins allowed to run a presigned
	// upload straight against the storage host. Defaults to the API's own
	// CORS origins: the SPAs allowed to call the API are exactly the ones
	// that perform these uploads, and a separate list is one more thing to
	// forget. STORAGE_CORS_ALLOWED_ORIGINS narrows or widens it; an explicit
	// empty value leaves every bucket policy untouched.
	CORSAllowedOrigins []string
}

type ServerConfig struct {
	Port        string
	Environment string
	LogLevel    string
	FrontendURL string
	CORSOrigins []string // Allowed CORS origins (legacy single-host fallback)
	MaxBodySize int64    // Maximum request body size in bytes (default 10MB)

	// TrustedProxyCount / TrustedProxyCIDRs describe the reverse proxies
	// between the internet and this process, and are what makes
	// X-Forwarded-For believable. See shared/middleware/realip.go.
	//
	// Prefer TRUSTED_PROXY_CIDRS (the networks our proxies live in) —
	// it stays correct if the chain length changes. TRUSTED_PROXY_COUNT
	// (how many hops sit in front) is the simpler alternative.
	//
	// Both unset means "trust no forwarding header": every request is
	// attributed to its direct peer. That is the safe default but it is
	// WRONG for any deployment behind a proxy — the IP allowlist, the
	// login geo-block, and the per-IP rate limiter would all see the
	// proxy's address for every caller. Set one of these in any
	// environment that terminates TLS somewhere other than this process.
	TrustedProxyCount int
	TrustedProxyCIDRs []string

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
	// PublicURL is the public origin of this audience's API (scheme +
	// host, no path). The auth module redirects a client-tier OAuth
	// callback to `{Client.PublicURL}/v1/auth/client/oauth/complete` so
	// the refresh cookie is set by the host that owns it. Read from
	// CLIENT_API_URL; empty means no client surface.
	PublicURL string
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
	// OperatorPasswordLoginBreakGlass mirrors AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS:
	// a boot-time, operator-login-only override of passwordLoginEnabledAdmin
	// (spec §4.2). It never opens client login, registration, resets,
	// password-confirm or unlink decisions, and never bypasses the
	// loginEnabledAdmin maintenance switch or the MFA/low-risk/RBAC gates
	// on the subsequent config repair.
	OperatorPasswordLoginBreakGlass bool
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
	// Name is the refresh-token cookie name — the only cookie Orkestra
	// sets in the browser (the SPA holds the access token in memory, so
	// there is no access-token cookie). Read from `COOKIE_NAME_REFRESH`,
	// defaulting to `orkestra_cookie`.
	Name string
	// OperatorDomain scopes refresh-token cookies minted on the operator
	// host (`console.*`) — set via `OPERATOR_COOKIE_DOMAIN`.
	OperatorDomain string
	// ClientDomain scopes refresh-token cookies minted on the client API
	// host — set via `CLIENT_COOKIE_DOMAIN`. Empty by default (host-only),
	// like OperatorDomain.
	ClientDomain string
	HttpOnly     bool
	Secure       bool
	SameSite     string
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
		// The client API answers on the client SPA's OWN hostname. Every
		// client-tier cookie is SameSite=Lax with an empty Domain, and
		// `localhost` is not a public suffix, so client.localhost and
		// api.localhost are different *sites* to a browser: an api.* client
		// API cannot store or send them. Ports play no part in a site, so
		// the SPA on :8081 and the API on :3000 are same-site (and still
		// cross-origin). See docker/CLAUDE.md, "Client tier: the SPA and
		// the client API must be same-site".
		defaultClientHost = "client.localhost:3000"
	}

	config.Server = ServerConfig{
		Port:        getEnv("PORT", "3000"),
		Environment: env,
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		FrontendURL: getEnv("FRONTEND_URL", "http://localhost:8080"),
		CORSOrigins: corsOrigins,
		MaxBodySize: getEnvAsInt64("MAX_BODY_SIZE", 10*1024*1024), // Default 10MB

		TrustedProxyCount: getEnvAsInt("TRUSTED_PROXY_COUNT", 0),
		TrustedProxyCIDRs: getEnvAsSlice("TRUSTED_PROXY_CIDRS", nil),
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

	// CLIENT_API_URL is the client API's public origin — where the auth
	// module relays a client-tier OAuth callback so the refresh cookie is
	// set by the host that owns it (spec §4.10). Derived from
	// CLIENT_API_HOST when unset: https in production-like environments,
	// http in development. Empty when no client surface exists.
	config.Server.Client.PublicURL = getEnv("CLIENT_API_URL", derivedPublicURL(config.Server.Client.Host, config.IsProductionLike()))

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
			Name:   getEnv("COOKIE_NAME_REFRESH", "orkestra_cookie"),
			// ADR-0003 PR-D D-9: per-audience cookie domains. BOTH default
			// to "" in every environment since bdcbb7ab — see
			// defaultOperatorCookieDomain / defaultClientCookieDomain below.
			// An empty value writes no Domain attribute at all, so the
			// cookie is host-only: scoped to whatever host minted it, which
			// round-trips on localhost, on *.localhost and on a LAN IP.
			// Operators set one explicitly only for a cross-subdomain
			// deployment; a domain that crosses both audiences is the thing
			// to avoid. Note these are NOT a lever for a cross-site
			// SPA/API layout: SameSite is computed from the request's site,
			// never from the cookie's Domain.
			OperatorDomain: getEnv("OPERATOR_COOKIE_DOMAIN", defaultOperatorCookieDomain(env)),
			ClientDomain:   getEnv("CLIENT_COOKIE_DOMAIN", defaultClientCookieDomain(env)),
			HttpOnly:       getEnvAsBool("COOKIE_HTTP_ONLY", true),
			Secure:         getEnvAsBool("COOKIE_SECURE", false), // Default false for development
			SameSite:       getEnv("COOKIE_SAME_SITE", "lax"),
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
		AllowLocalhostRedirects:         getEnvAsBool("ALLOW_LOCALHOST_REDIRECTS", true), // Default true for development
		OperatorPasswordLoginBreakGlass: getEnvAsBool("AUTH_OPERATOR_PASSWORD_LOGIN_BREAK_GLASS", false),
	}

	config.Rate = RateLimitConfig{
		RequestsPerMinute: getEnvAsInt("RATE_LIMIT_REQUESTS_PER_MINUTE", 60),
		Burst:             getEnvAsInt("RATE_LIMIT_BURST", 10),
	}

	// Object storage configuration (RustFS default — S3-compatible).
	// Defaults below match docker-compose.infra.yml's rustfs service so
	// `docker compose -f infra.yml up -d` + a fresh boot just works. Uses the
	// `rustfs` compose SERVICE name (stable across stacks) rather than the
	// old namespaced container-name default, which stopped resolving once
	// containers became per-stack (`${APP_NAME}-rustfs-${ENV}`).
	config.Storage = StorageConfig{
		Endpoint:       getEnv("STORAGE_ENDPOINT", "http://rustfs:9000"),
		PublicEndpoint: getEnv("STORAGE_PUBLIC_ENDPOINT", ""),
		Region:         getEnv("STORAGE_REGION", "us-east-1"),
		Bucket:         getEnv("STORAGE_BUCKET", "orkestra-avatars"),
		AccessKey:      getEnv("STORAGE_ACCESS_KEY", ""),
		SecretKey:      getEnv("STORAGE_SECRET_KEY", ""),
		ForcePathStyle: getEnvAsBool("STORAGE_FORCE_PATH_STYLE", true),
		EnsureBucket:   getEnvAsBool("STORAGE_ENSURE_BUCKET", true),
		BucketPrefix:   getEnv("STORAGE_BUCKET_PREFIX", "orkestra"),
		// Defaults to every origin allowed to call the API — see the field
		// comment. The union matters: a deployment that has moved to the
		// per-audience lists leaves CORS_ORIGINS on its localhost default,
		// so defaulting to that alone would miss the real console.
		CORSAllowedOrigins: getEnvAsSlice("STORAGE_CORS_ALLOWED_ORIGINS",
			mergeOrigins(corsOrigins, config.Server.Operator.CORSOrigins, config.Server.Client.CORSOrigins)),
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

// defaultOperatorCookieDomain returns the fallback for the operator
// refresh-cookie scope. Empty everywhere by default so the cookie is
// host-only (no Domain attribute) — scoped to whatever host minted it.
//
// In dev the frontend and backend are co-located on a single host (localhost,
// a *.localhost alias, OR a LAN IP for multi-device testing), so a host-only
// cookie round-trips on every access pattern. A fixed "console.localhost"
// default silently broke LAN-IP / hostname access: a cookie scoped to
// console.localhost is never sent to 192.168.x.x, and an IP literal cannot be
// a cookie Domain at all — so the browser dropped the refresh cookie and every
// page refresh bounced the user to /login. Set OPERATOR_COOKIE_DOMAIN
// explicitly only for a cross-subdomain deployment (e.g. console.example.com
// sharing a parent-domain cookie with api.example.com). Non-dev is likewise
// empty so a misconfigured prod deploy fails closed rather than leaking across
// hosts.
func defaultOperatorCookieDomain(_ string) string {
	return ""
}

// defaultClientCookieDomain mirrors defaultOperatorCookieDomain for the
// client API surface.
func defaultClientCookieDomain(_ string) string {
	return ""
}

// derivedPublicURL builds "scheme://host" for an audience whose public
// origin was not configured explicitly. The scheme follows the environment
// (a production-like deployment terminates TLS in front of the API).
func derivedPublicURL(host string, secure bool) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		return strings.TrimRight(host, "/")
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return scheme + "://" + host
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

// getEnvAsDuration reads a Go duration, additionally accepting a "d"
// (day) suffix that time.ParseDuration does not support.
//
// The day suffix is not a convenience — it is what the config actually
// uses. JWT_REFRESH_TOKEN_EXPIRY is written as "7d"/"30d" in every
// compose file and in .env.example. Without day support that parsed to
// an error, the "7d" fallback default hit the same error, and this
// function returned 0 — which NewJWTService reads as "unset" and
// replaces with 30 days. The refresh-token lifetime was therefore 30
// days on every deployment no matter what was configured, and nothing
// surfaced it because a zero return looks exactly like "not set".
func getEnvAsDuration(key string, defaultValue string) time.Duration {
	if value, ok := parseDuration(getEnv(key, defaultValue)); ok {
		return value
	}
	if defaultDuration, ok := parseDuration(defaultValue); ok {
		return defaultDuration
	}
	return 0
}

// parseDuration delegates to utils.ParseDuration so environment
// variables and admin-UI values are read by one parser. See ADR-0017.
func parseDuration(raw string) (time.Duration, bool) {
	return utils.ParseDuration(raw)
}

// mergeOrigins unions the API's CORS origin lists, preserving order and
// dropping duplicates and blanks. Used to derive the default set of browser
// origins allowed to upload straight to object storage.
func mergeOrigins(lists ...[]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, list := range lists {
		for _, origin := range list {
			origin = strings.TrimSpace(origin)
			if origin == "" || seen[origin] {
				continue
			}
			seen[origin] = true
			out = append(out, origin)
		}
	}
	return out
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
