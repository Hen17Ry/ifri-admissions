package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"ifri-admissions/services/api/internal/platform/secrets"
)

type Config struct {
	Environment, Host, Port, FrontendOrigin                                                 string
	PostgresHost, PostgresPort, PostgresDB, PostgresUser, PostgresPassword, PostgresSSLMode string
	RedisHost, RedisPort, RedisPassword                                                     string
	RedisTLS                                                                                bool
	MinioEndpoint                                                                           string
	DraftSessionTTL                                                                         time.Duration
	S3Endpoint, S3Bucket, S3Region, S3AccessKey, S3SecretKey                                string
	DocumentMaxBytes                                                                        int64
	// Administration (phase 7). The master key never leaves the process: HKDF subkeys only.
	AdminSecretsKey                        []byte
	AdminSecretsKeyVersion                 int
	AdminMFARequired                       bool
	AdminSessionIdle, AdminSessionAbsolute time.Duration
	AdminActivationTTL                     time.Duration
}

// ExampleAdminSecretsKey is the documented development key of .env.example, refused in production.
const ExampleAdminSecretsKey = "ZGV2LW9ubHktYWRtaW4tc2VjcmV0cy1rZXktMzItYnl0ZXMhIQ=="

func Load() (Config, error) { return load(os.Getenv) }

func load(get func(string) string) (Config, error) {
	var c Config
	required := []struct {
		key    string
		target *string
	}{
		{"APP_ENV", &c.Environment}, {"API_HOST", &c.Host}, {"API_PORT", &c.Port}, {"FRONTEND_ORIGIN", &c.FrontendOrigin},
		{"POSTGRES_HOST", &c.PostgresHost}, {"POSTGRES_PORT", &c.PostgresPort}, {"POSTGRES_DB", &c.PostgresDB},
		{"POSTGRES_USER", &c.PostgresUser}, {"POSTGRES_PASSWORD", &c.PostgresPassword}, {"POSTGRES_SSLMODE", &c.PostgresSSLMode},
		{"REDIS_HOST", &c.RedisHost}, {"REDIS_PORT", &c.RedisPort}, {"REDIS_PASSWORD", &c.RedisPassword},
		{"MINIO_ENDPOINT", &c.MinioEndpoint},
	}
	for _, item := range required {
		value := get(item.key)
		if strings.TrimSpace(value) == "" {
			return c, fmt.Errorf("%s is required", item.key)
		}
		*item.target = value
	}
	if c.Environment != "development" && c.Environment != "test" && c.Environment != "production" {
		return c, fmt.Errorf("APP_ENV must be development, test or production")
	}
	if net.ParseIP(c.Host) == nil {
		return c, fmt.Errorf("API_HOST must be an explicit listen IP address")
	}
	for _, item := range []struct{ name, value string }{{"API_PORT", c.Port}, {"POSTGRES_PORT", c.PostgresPort}, {"REDIS_PORT", c.RedisPort}} {
		n, err := strconv.Atoi(item.value)
		if err != nil || n < 1 || n > 65535 {
			return c, fmt.Errorf("%s must be a valid TCP port", item.name)
		}
	}
	tlsValue := get("REDIS_TLS")
	if tlsValue != "true" && tlsValue != "false" {
		return c, fmt.Errorf("REDIS_TLS must be true or false")
	}
	c.RedisTLS = tlsValue == "true"
	if c.PostgresSSLMode != "disable" && c.PostgresSSLMode != "verify-full" {
		return c, fmt.Errorf("POSTGRES_SSLMODE must be disable or verify-full")
	}
	for _, item := range []struct{ name, value string }{{"FRONTEND_ORIGIN", c.FrontendOrigin}, {"MINIO_ENDPOINT", c.MinioEndpoint}} {
		u, err := url.Parse(item.value)
		if err != nil || u.Hostname() == "" || strings.Contains(item.value, "*") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("%s must be an explicit HTTP(S) origin without path or credentials", item.name)
		}
		if c.Environment == "production" && u.Scheme != "https" {
			return c, fmt.Errorf("%s requires HTTPS in production", item.name)
		}
	}
	if c.Environment == "production" {
		if c.PostgresSSLMode != "verify-full" || !c.RedisTLS {
			return c, fmt.Errorf("production requires PostgreSQL verify-full and REDIS_TLS=true")
		}
		for _, value := range []string{c.PostgresPassword, c.RedisPassword} {
			if strings.HasPrefix(value, "replace-dev-") {
				return c, fmt.Errorf("development credentials are forbidden in production")
			}
		}
	}
	ttl := get("DRAFT_SESSION_TTL_SECONDS")
	if ttl == "" && c.Environment != "production" {
		ttl = "604800"
	}
	seconds, err := strconv.Atoi(ttl)
	if err != nil || seconds < 300 || seconds > 2592000 {
		return c, fmt.Errorf("DRAFT_SESSION_TTL_SECONDS must be between 300 and 2592000")
	}
	c.DraftSessionTTL = time.Duration(seconds) * time.Second

	c.S3Endpoint = get("S3_ENDPOINT")
	if c.S3Endpoint == "" {
		c.S3Endpoint = c.MinioEndpoint
	}
	c.S3Bucket = get("MINIO_BUCKET")
	c.S3Region = get("S3_REGION")
	if c.S3Region == "" {
		c.S3Region = "us-east-1"
	}
	c.S3AccessKey = get("S3_ACCESS_KEY")
	c.S3SecretKey = get("S3_SECRET_KEY")
	if c.S3Bucket == "" || c.S3AccessKey == "" || c.S3SecretKey == "" {
		return c, fmt.Errorf("MINIO_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY are required")
	}
	u, err := url.Parse(c.S3Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return c, fmt.Errorf("S3_ENDPOINT requires an explicit HTTP(S) origin")
	}
	if c.Environment == "production" && (u.Scheme != "https" || strings.HasPrefix(c.S3SecretKey, "replace-dev-")) {
		return c, fmt.Errorf("S3 requires HTTPS and non-example credentials in production")
	}
	max := get("DOCUMENT_MAX_BYTES")
	if max == "" && c.Environment != "production" {
		max = "10485760"
	}
	c.DocumentMaxBytes, err = strconv.ParseInt(max, 10, 64)
	if err != nil || c.DocumentMaxBytes < 1024 || c.DocumentMaxBytes > 52428800 {
		return c, fmt.Errorf("DOCUMENT_MAX_BYTES must be between 1024 and 52428800")
	}

	if err = loadAdmin(get, &c); err != nil {
		return c, err
	}
	return c, nil
}

func seconds(get func(string) string, key, fallback string, min, max int) (time.Duration, error) {
	value := get(key)
	if value == "" {
		value = fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be between %d and %d seconds", key, min, max)
	}
	return time.Duration(n) * time.Second, nil
}

func loadAdmin(get func(string) string, c *Config) error {
	raw := strings.TrimSpace(get("ADMIN_SECRETS_KEY"))
	if raw == "" {
		return fmt.Errorf("ADMIN_SECRETS_KEY is required")
	}
	if c.Environment == "production" && raw == ExampleAdminSecretsKey {
		return fmt.Errorf("the example ADMIN_SECRETS_KEY is forbidden in production")
	}
	key, err := secrets.ParseMaster(raw)
	if err != nil {
		return err
	}
	c.AdminSecretsKey = key
	version := get("ADMIN_SECRETS_KEY_VERSION")
	if version == "" {
		version = "1"
	}
	c.AdminSecretsKeyVersion, err = strconv.Atoi(version)
	if err != nil || c.AdminSecretsKeyVersion < 1 || c.AdminSecretsKeyVersion > 32767 {
		return fmt.Errorf("ADMIN_SECRETS_KEY_VERSION must be between 1 and 32767")
	}
	switch get("ADMIN_MFA_REQUIRED") {
	case "", "true":
		c.AdminMFARequired = true
	case "false":
		if c.Environment == "production" {
			return fmt.Errorf("ADMIN_MFA_REQUIRED=false is forbidden in production")
		}
	default:
		return fmt.Errorf("ADMIN_MFA_REQUIRED must be true or false")
	}
	if c.AdminSessionIdle, err = seconds(get, "ADMIN_SESSION_IDLE_SECONDS", "1800", 300, 7200); err != nil {
		return err
	}
	if c.AdminSessionAbsolute, err = seconds(get, "ADMIN_SESSION_ABSOLUTE_SECONDS", "28800", 1800, 86400); err != nil {
		return err
	}
	if c.AdminSessionAbsolute < c.AdminSessionIdle {
		return fmt.Errorf("ADMIN_SESSION_ABSOLUTE_SECONDS must not be shorter than ADMIN_SESSION_IDLE_SECONDS")
	}
	c.AdminActivationTTL, err = seconds(get, "ADMIN_ACTIVATION_TTL_SECONDS", "259200", 3600, 604800)
	return err
}

func (c Config) DatabaseURL() string {
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.PostgresHost, c.PostgresPort), Path: "/" + c.PostgresDB, User: url.UserPassword(c.PostgresUser, c.PostgresPassword)}
	q := url.Values{"sslmode": {c.PostgresSSLMode}}
	u.RawQuery = q.Encode()
	return u.String()
}
func (c Config) RedisAddress() string { return net.JoinHostPort(c.RedisHost, c.RedisPort) }
