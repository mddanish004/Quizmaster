package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the typed, validated application configuration.
// All values come from environment variables; see .env.example.
type Config struct {
	Env             string
	Origin          string
	Domain          string
	HTTPAddr        string
	LogLevel        string
	LogFormat       string
	ShutdownTimeout time.Duration

	DatabaseURL           string
	MigrationsDatabaseURL string
	DBMaxConns            int32
	DBMinConns            int32
	DBMaxConnLifetime     time.Duration
	DBMaxConnIdleTime     time.Duration

	SessionCookieName     string
	ParticipantCookieName string
	SessionIdleTTL        time.Duration
	SessionAbsoluteTTL    time.Duration
	CSRFSecret            string
	PasswordPepper        string

	// TrustedProxies are CIDRs allowed to set X-Forwarded-For (Caddy/Docker
	// networks). Direct clients' forwarding headers are ignored.
	TrustedProxyCIDRs string
	TrustedProxies    []*net.IPNet

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string

	AIProvider        string
	HFToken           string
	HFBaseURL         string
	HFModel           string
	HFProvider        string
	GroqAPIKey        string
	GroqBaseURL       string
	GroqModel         string
	AIRequestTimeout  time.Duration
	AIEndpointTimeout time.Duration
	AIMaxInFlight     int
	AIDailyUserLimit  int
	AIDailyIPLimit    int
	AIStoreContent    bool

	MailProvider string
	MailFrom     string
	AWSRegion    string

	JoinCodeTTL        time.Duration
	LiveSessionIdleTTL time.Duration
	LiveSessionMaxTTL  time.Duration
	WSAllowedOrigins   []string
	WSMaxMessageBytes  int
	WSOutboundQueue    int
	RateLimitEnabled   bool

	MetricsEnabled bool
	MetricsAddr    string
	BuildVersion   string
	BuildCommit    string

	MockAI   bool
	MockOIDC bool
	MockMail bool
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func getDuration(key, def string) (time.Duration, error) {
	raw := getenv(key, def)
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, raw, err)
	}
	return d, nil
}

func getInt(key, def string) (int, error) {
	raw := getenv(key, def)
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, raw, err)
	}
	return n, nil
}

// Load reads and validates configuration, failing fast on bad input.
func Load() (*Config, error) {
	c := &Config{
		Env:                   getenv("APP_ENV", "development"),
		Origin:                getenv("APP_ORIGIN", "http://localhost:5173"),
		Domain:                getenv("APP_DOMAIN", "localhost"),
		HTTPAddr:              getenv("HTTP_ADDR", ":8080"),
		LogLevel:              strings.ToLower(getenv("LOG_LEVEL", "info")),
		LogFormat:             strings.ToLower(getenv("LOG_FORMAT", "json")),
		SessionCookieName:     getenv("SESSION_COOKIE_NAME", "__Host-qf_session"),
		ParticipantCookieName: getenv("PARTICIPANT_COOKIE_NAME", "__Host-qf_participant"),
		GoogleClientID:        os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:    os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:     getenv("GOOGLE_REDIRECT_URL", "http://localhost:8080/api/v1/auth/google/callback"),
		AIProvider:            getenv("AI_PROVIDER", "huggingface"),
		HFToken:               os.Getenv("HF_TOKEN"),
		HFBaseURL:             getenv("HF_BASE_URL", "https://router.huggingface.co/v1"),
		HFModel:               getenv("HF_MODEL", "Qwen/Qwen3-32B"),
		HFProvider:            getenv("HF_PROVIDER", "auto"),
		GroqAPIKey:            os.Getenv("GROQ_API_KEY"),
		GroqBaseURL:           getenv("GROQ_BASE_URL", "https://api.groq.com/openai/v1"),
		GroqModel:             getenv("GROQ_MODEL", "openai/gpt-oss-120b"),
		AIMaxInFlight:         4,
		AIDailyUserLimit:      5,
		AIDailyIPLimit:        20,
		MailProvider:          getenv("MAIL_PROVIDER", "mock"),
		MailFrom:              getenv("MAIL_FROM", "QuizForge AI <no-reply@example.com>"),
		AWSRegion:             getenv("AWS_REGION", "ap-southeast-1"),
		WSAllowedOrigins:      strings.Split(getenv("WS_ALLOWED_ORIGINS", "http://localhost:5173"), ","),
		WSMaxMessageBytes:     16384,
		WSOutboundQueue:       64,
		RateLimitEnabled:      getenv("RATE_LIMIT_ENABLED", "true") == "true",
		MetricsEnabled:        getenv("METRICS_ENABLED", "true") == "true",
		MetricsAddr:           getenv("METRICS_ADDR", "127.0.0.1:9090"),
		BuildVersion:          getenv("BUILD_VERSION", "dev"),
		BuildCommit:           getenv("BUILD_COMMIT", "unknown"),
		MockAI:                getenv("MOCK_AI", "true") == "true",
		MockOIDC:              getenv("MOCK_OIDC", "true") == "true",
		MockMail:              getenv("MOCK_MAIL", "true") == "true",
	}
	var err error
	if c.ShutdownTimeout, err = getDuration("SHUTDOWN_TIMEOUT", "25s"); err != nil {
		return nil, err
	}
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.MigrationsDatabaseURL = os.Getenv("MIGRATIONS_DATABASE_URL")
	if n, err := getInt("DB_MAX_CONNS", "10"); err != nil {
		return nil, err
	} else {
		c.DBMaxConns = int32(n)
	}
	if n, err := getInt("DB_MIN_CONNS", "0"); err != nil {
		return nil, err
	} else {
		c.DBMinConns = int32(n)
	}
	if c.DBMaxConnLifetime, err = getDuration("DB_MAX_CONN_LIFETIME", "30m"); err != nil {
		return nil, err
	}
	if c.DBMaxConnIdleTime, err = getDuration("DB_MAX_CONN_IDLE_TIME", "5m"); err != nil {
		return nil, err
	}
	if c.SessionIdleTTL, err = getDuration("SESSION_IDLE_TTL", "24h"); err != nil {
		return nil, err
	}
	if c.SessionAbsoluteTTL, err = getDuration("SESSION_ABSOLUTE_TTL", "720h"); err != nil {
		return nil, err
	}
	c.CSRFSecret = os.Getenv("CSRF_SECRET")
	c.PasswordPepper = os.Getenv("PASSWORD_PEPPER")
	c.TrustedProxyCIDRs = getenv("TRUSTED_PROXY_CIDRS", "172.16.0.0/12")
	for _, part := range strings.Split(c.TrustedProxyCIDRs, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		_, network, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS %q: %w", part, err)
		}
		c.TrustedProxies = append(c.TrustedProxies, network)
	}
	if c.AIRequestTimeout, err = getDuration("AI_REQUEST_TIMEOUT", "45s"); err != nil {
		return nil, err
	}
	if c.AIEndpointTimeout, err = getDuration("AI_ENDPOINT_TIMEOUT", "60s"); err != nil {
		return nil, err
	}
	if c.AIMaxInFlight, err = getInt("AI_MAX_IN_FLIGHT", "4"); err != nil {
		return nil, err
	}
	if c.AIDailyUserLimit, err = getInt("AI_DAILY_USER_LIMIT", "5"); err != nil {
		return nil, err
	}
	if c.AIDailyIPLimit, err = getInt("AI_DAILY_IP_LIMIT", "20"); err != nil {
		return nil, err
	}
	c.AIStoreContent = getenv("AI_STORE_CONTENT", "false") == "true"
	if c.JoinCodeTTL, err = getDuration("JOIN_CODE_TTL", "6h"); err != nil {
		return nil, err
	}
	if c.LiveSessionIdleTTL, err = getDuration("LIVE_SESSION_IDLE_TTL", "2h"); err != nil {
		return nil, err
	}
	if c.LiveSessionMaxTTL, err = getDuration("LIVE_SESSION_MAX_TTL", "6h"); err != nil {
		return nil, err
	}
	if n, err := getInt("WS_MAX_MESSAGE_BYTES", "16384"); err != nil {
		return nil, err
	} else {
		c.WSMaxMessageBytes = n
	}
	if n, err := getInt("WS_OUTBOUND_QUEUE", "64"); err != nil {
		return nil, err
	} else {
		c.WSOutboundQueue = n
	}
	for i := range c.WSAllowedOrigins {
		c.WSAllowedOrigins[i] = strings.TrimSpace(c.WSAllowedOrigins[i])
	}
	return c, c.Validate()
}

// Validate enforces cross-field invariants.
func (c *Config) Validate() error {
	if c.Env != "development" && c.Env != "test" && c.Env != "staging" && c.Env != "production" {
		return fmt.Errorf("APP_ENV must be development|test|staging|production, got %q", c.Env)
	}
	if _, err := url.ParseRequestURI(c.Origin); err != nil {
		return fmt.Errorf("APP_ORIGIN invalid: %w", err)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL must be debug|info|warn|error, got %q", c.LogLevel)
	}
	if c.Env == "production" {
		if strings.HasPrefix(c.DatabaseURL, "postgresql://localhost") || strings.Contains(c.DatabaseURL, "localhost") {
			return errors.New("production DATABASE_URL must not point to localhost")
		}
		if !strings.Contains(c.DatabaseURL, "sslmode=require") && !strings.Contains(c.DatabaseURL, "sslmode=verify") {
			return errors.New("production DATABASE_URL must require TLS")
		}
		if c.CSRFSecret == "" {
			return errors.New("CSRF_SECRET is required in production")
		}
		if len(c.CSRFSecret) < 32 {
			return errors.New("CSRF_SECRET must be at least 32 characters in production")
		}
	}
	if c.DBMaxConns < 1 || c.DBMaxConns > 100 {
		return fmt.Errorf("DB_MAX_CONNS out of range 1..100: %d", c.DBMaxConns)
	}
	if c.AIMaxInFlight < 1 || c.AIMaxInFlight > 32 {
		return fmt.Errorf("AI_MAX_IN_FLIGHT out of range: %d", c.AIMaxInFlight)
	}
	return nil
}

// IsProd reports production mode.
func (c *Config) IsProd() bool { return c.Env == "production" }
