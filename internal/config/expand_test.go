package config

import (
	"os"
	"testing"
)

func TestExpandSecrets(t *testing.T) {
	// Set env vars for testing
	os.Setenv("TEST_STRIPE_SECRET", "whsec_test123")
	os.Setenv("TEST_OIDC_SECRET", "oidc_secret_val")
	os.Setenv("TEST_SMTP_PASS", "smtp_pass_val")
	os.Setenv("TEST_REDIS_PASS", "redis_pass_val")
	os.Setenv("TEST_QDRANT_KEY", "qdrant_key_val")
	os.Setenv("TEST_WEBHOOK_SECRET", "webhook_secret_val")
	os.Setenv("TEST_SEMANTIC_KEY", "semantic_key_val")
	os.Setenv("TEST_PROVIDER_KEY", "provider_key_val")
	defer func() {
		os.Unsetenv("TEST_STRIPE_SECRET")
		os.Unsetenv("TEST_OIDC_SECRET")
		os.Unsetenv("TEST_SMTP_PASS")
		os.Unsetenv("TEST_REDIS_PASS")
		os.Unsetenv("TEST_QDRANT_KEY")
		os.Unsetenv("TEST_WEBHOOK_SECRET")
		os.Unsetenv("TEST_SEMANTIC_KEY")
		os.Unsetenv("TEST_PROVIDER_KEY")
	}()

	cfg := &Config{
		Providers: []ProviderConfig{
			{APIKey: "${TEST_PROVIDER_KEY}"},
		},
		Billing: BillingConfig{
			StripeWebhookSecret: "${TEST_STRIPE_SECRET}",
		},
		Security: SecurityConfig{
			OIDC: OIDCYAMLConfig{
				ClientSecret: "${TEST_OIDC_SECRET}",
			},
		},
		Notification: NotificationConfig{
			SMTPPass: "${TEST_SMTP_PASS}",
		},
		Storage: StorageYAMLConfig{
			RedisPassword: "${TEST_REDIS_PASS}",
			QdrantAPIKey:  "${TEST_QDRANT_KEY}",
		},
		Events: EventsConfig{
			WebhookSecret: "${TEST_WEBHOOK_SECRET}",
		},
		Cache: CacheConfig{
			L2Semantic: L2SemanticConfig{
				APIKey: "${TEST_SEMANTIC_KEY}",
			},
		},
	}

	cfg.ExpandSecrets()

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"provider api_key", cfg.Providers[0].APIKey, "provider_key_val"},
		{"stripe secret", cfg.Billing.StripeWebhookSecret, "whsec_test123"},
		{"oidc secret", cfg.Security.OIDC.ClientSecret, "oidc_secret_val"},
		{"smtp password", cfg.Notification.SMTPPass, "smtp_pass_val"},
		{"redis password", cfg.Storage.RedisPassword, "redis_pass_val"},
		{"qdrant api key", cfg.Storage.QdrantAPIKey, "qdrant_key_val"},
		{"webhook secret", cfg.Events.WebhookSecret, "webhook_secret_val"},
		{"semantic api key", cfg.Cache.L2Semantic.APIKey, "semantic_key_val"},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("ExpandSecrets %s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
