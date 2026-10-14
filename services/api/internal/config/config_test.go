package config

import (
	"strings"
	"testing"
)

func validEnv() map[string]string {
	return map[string]string{"MINIO_BUCKET": "ifri-documents", "S3_ACCESS_KEY": "test", "S3_SECRET_KEY": "test-secret", "APP_ENV": "test", "API_HOST": "127.0.0.1", "API_PORT": "8080", "FRONTEND_ORIGIN": "http://localhost:5173", "POSTGRES_HOST": "localhost", "POSTGRES_PORT": "5432", "POSTGRES_DB": "ifri", "POSTGRES_USER": "local", "POSTGRES_PASSWORD": "secret:@/#", "POSTGRES_SSLMODE": "disable", "REDIS_HOST": "localhost", "REDIS_PORT": "6379", "REDIS_PASSWORD": "secret", "REDIS_TLS": "false", "MINIO_ENDPOINT": "http://localhost:9000", "ADMIN_SECRETS_KEY": ExampleAdminSecretsKey}
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

func TestAdminConfig(t *testing.T) {
	env := validEnv()
	c, err := load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if !c.AdminMFARequired || c.AdminSecretsKeyVersion != 1 || c.AdminSessionIdle.Minutes() != 30 || c.AdminSessionAbsolute.Hours() != 8 || c.AdminActivationTTL.Hours() != 72 || len(c.AdminSecretsKey) < 32 {
		t.Fatalf("admin defaults: %+v", c)
	}
	env["ADMIN_MFA_REQUIRED"] = "false"
	if c, err = load(func(k string) string { return env[k] }); err != nil || c.AdminMFARequired {
		t.Fatal("MFA must be optional outside production", err)
	}
	for _, tc := range []struct{ name, key, value string }{
		{"missing key", "ADMIN_SECRETS_KEY", ""},
		{"short key", "ADMIN_SECRETS_KEY", "c2hvcnQ="},
		{"not base64", "ADMIN_SECRETS_KEY", "!!!"},
		{"key version", "ADMIN_SECRETS_KEY_VERSION", "0"},
		{"mfa flag", "ADMIN_MFA_REQUIRED", "maybe"},
		{"idle too short", "ADMIN_SESSION_IDLE_SECONDS", "60"},
		{"absolute shorter than idle", "ADMIN_SESSION_ABSOLUTE_SECONDS", "1800"},
		{"activation too long", "ADMIN_ACTIVATION_TTL_SECONDS", "9999999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := validEnv()
			env["ADMIN_SESSION_IDLE_SECONDS"] = "3600"
			env[tc.key] = tc.value
			if _, err := load(func(k string) string { return env[k] }); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	// Production refuses MFA off and the example key, before any other production check matters.
	for key, value := range map[string]string{"ADMIN_MFA_REQUIRED": "false", "ADMIN_SECRETS_KEY": ExampleAdminSecretsKey} {
		env := validEnv()
		env["ADMIN_SECRETS_KEY"] = "c3Ryb25nLXByb2R1Y3Rpb24ta2V5LWZvci10ZXN0cy0xMjM0NTY3OA=="
		env[key] = value
		var admin Config
		admin.Environment = "production"
		if err := loadAdmin(func(k string) string { return env[k] }, &admin); err == nil {
			t.Fatal("production accepted", key)
		}
	}
}
