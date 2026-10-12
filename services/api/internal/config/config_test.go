package config

import (
	"strings"
	"testing"
)

func validEnv() map[string]string {
	return map[string]string{"APP_ENV": "test", "API_HOST": "127.0.0.1", "API_PORT": "8080", "FRONTEND_ORIGIN": "http://localhost:5173", "POSTGRES_HOST": "localhost", "POSTGRES_PORT": "5432", "POSTGRES_DB": "ifri", "POSTGRES_USER": "local", "POSTGRES_PASSWORD": "secret:@/#", "POSTGRES_SSLMODE": "disable", "REDIS_HOST": "localhost", "REDIS_PORT": "6379", "REDIS_PASSWORD": "secret", "REDIS_TLS": "false", "MINIO_ENDPOINT": "http://localhost:9000"}
}
func TestConfig(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"invalid session duration", "DRAFT_SESSION_TTL_SECONDS", "0"},
		{"invalid listen host", "API_HOST", "*"},
		{"missing secret", "POSTGRES_PASSWORD", ""}, {"invalid port", "API_PORT", "0"}, {"wildcard CORS", "FRONTEND_ORIGIN", "*"}, {"path CORS", "FRONTEND_ORIGIN", "http://localhost/path"}, {"invalid TLS", "REDIS_TLS", "yes"}, {"unknown environment", "APP_ENV", "prod"}, {"insecure production", "APP_ENV", "production"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := validEnv()
			env[tc.key] = tc.value
			if _, err := load(func(k string) string { return env[k] }); err == nil {
				t.Fatal("expected configuration rejection")
			}
		})
	}
	env := validEnv()
	c, err := load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.DatabaseURL(), "secret%3A%40%2F%23") {
		t.Fatal("database password must be URL-escaped")
	}
}
