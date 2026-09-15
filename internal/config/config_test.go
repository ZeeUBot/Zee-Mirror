package config

import (
	"strings"
	"testing"
)

func TestValidate_MissingBotTokens(t *testing.T) {
	cfg := &Config{
		OwnerID:    12345,
		RcloneDest: "gdrive:/test",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for missing BotTokens")
	}
	if err.Error() != "BOT_TOKEN or BOT_TOKENS must be set" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidate_MissingOwnerID(t *testing.T) {
	cfg := &Config{
		BotTokens:  []string{"token1"},
		RcloneDest: "gdrive:/test",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for missing OwnerID")
	}
	if err.Error() != "OWNER_ID must be set" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestValidate_EmptyRcloneDest_Defaults(t *testing.T) {
	cfg := &Config{
		BotTokens: []string{"token1"},
		OwnerID:   12345,
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cfg.RcloneDest != "gdrive:/MirrorBot" {
		t.Errorf("expected default rclone dest, got %s", cfg.RcloneDest)
	}
}

func TestValidate_AllFieldsPresent(t *testing.T) {
	cfg := &Config{
		BotTokens:  []string{"token1"},
		OwnerID:    12345,
		RcloneDest: "gdrive:/MyFolder",
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cfg.RcloneDest != "gdrive:/MyFolder" {
		t.Errorf("RcloneDest should not be overridden, got %s", cfg.RcloneDest)
	}
}

func TestValidate_SingleBotToken(t *testing.T) {
	cfg := &Config{
		BotTokens: []string{"single-token"},
		OwnerID:   111,
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("single token should be valid: %v", err)
	}
}

func TestValidate_MultipleBotTokens(t *testing.T) {
	cfg := &Config{
		BotTokens: []string{"token1", "token2", "token3"},
		OwnerID:   111,
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("multiple tokens should be valid: %v", err)
	}
}

func TestValidate_ProductionDefaultDashboardToken(t *testing.T) {
	cfg := &Config{
		BotTokens:      []string{"token1"},
		OwnerID:        12345,
		AppEnv:         "production",
		DashboardToken: "zee-mirror-secret",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for default WEB_DASHBOARD_TOKEN in production")
	}
}

func TestValidate_ProductionEmptyDashboardToken(t *testing.T) {
	cfg := &Config{
		BotTokens:      []string{"token1"},
		OwnerID:        12345,
		AppEnv:         "production",
		DashboardToken: "",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for empty WEB_DASHBOARD_TOKEN in production")
	}
}

func TestValidate_ProductionCustomDashboardToken(t *testing.T) {
	cfg := &Config{
		BotTokens:      []string{"token1"},
		OwnerID:        12345,
		AppEnv:         "production",
		DashboardToken: "my-super-secret-token",
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("custom token should be valid in production: %v", err)
	}
}

func TestValidate_UnsetAppEnvDefaultTokenAllowed(t *testing.T) {
	cfg := &Config{
		BotTokens:      []string{"token1"},
		OwnerID:        12345,
		AppEnv:         "",
		DashboardToken: "zee-mirror-secret",
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("default token should still work when APP_ENV is unset (backward compat): %v", err)
	}
}

func TestValidate_DevelopmentDefaultTokenAllowed(t *testing.T) {
	cfg := &Config{
		BotTokens:      []string{"token1"},
		OwnerID:        12345,
		AppEnv:         "development",
		DashboardToken: "zee-mirror-secret",
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("default token should be allowed outside production: %v", err)
	}
}

func TestValidate_WebhookRequiresSecret(t *testing.T) {
	cfg := &Config{
		BotTokens:  []string{"token1"},
		OwnerID:    12345,
		UseWebhook: true,
		WebhookURL: "https://example.com/hook",
		RcloneDest: "gdrive:/test",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "WEBHOOK_SECRET") {
		t.Errorf("expected WEBHOOK_SECRET error, got: %v", err)
	}
	cfg.WebhookSecret = "s3cret"
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error with secret set: %v", err)
	}
}
