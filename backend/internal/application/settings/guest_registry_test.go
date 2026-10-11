package settings

import "testing"

// TestGuestSettingsRegisteredWithDocumentedDefaults 防止游客配置键与实施文档脱节。
func TestGuestSettingsRegisteredWithDocumentedDefaults(t *testing.T) {
	if !IsValidNamespace("guest") {
		t.Fatal("guest must be a valid settings namespace")
	}
	expected := map[string]string{
		"enabled":                      "false",
		"model_whitelist":              "",
		"per_visitor_daily_budget_usd": "0.10",
		"global_daily_budget_usd":      "1.00",
		"daily_generation_limit":       "20",
		"rate_limit_per_minute":        "20",
		"per_visitor_concurrency":      "1",
		"global_concurrency":           "2",
		"input_token_limit":            "8000",
		"output_token_limit":           "2048",
		"budget_timezone":              "Europe/Berlin",
		"billing_retention_days":       "30",
	}
	for key, want := range expected {
		spec, ok := lookupSettingSpec("guest", key)
		if !ok {
			t.Fatalf("guest:%s is not registered", key)
		}
		if spec.Default != want {
			t.Fatalf("guest:%s default = %q, want %q", key, spec.Default, want)
		}
		if spec.Validate == nil {
			t.Fatalf("guest:%s must have a validator", key)
		}
	}
	if err := validateSettingValue("guest", "budget_timezone", "Mars/Olympus"); err == nil {
		t.Fatal("invalid timezone must be rejected")
	}
	if err := validateSettingValue("guest", "model_whitelist", "gpt-4o,claude"); err != nil {
		t.Fatalf("valid whitelist rejected: %v", err)
	}
}

func TestGuestSettingsValidationRules(t *testing.T) {
	cases := []struct {
		key     string
		value   string
		wantErr bool
	}{
		{key: "per_visitor_daily_budget_usd", value: "-1", wantErr: true},
		{key: "global_daily_budget_usd", value: "1", wantErr: false},
		{key: "daily_generation_limit", value: "-1", wantErr: true},
		{key: "rate_limit_per_minute", value: "0", wantErr: true},
		{key: "per_visitor_concurrency", value: "0", wantErr: true},
		{key: "global_concurrency", value: "0", wantErr: true},
		{key: "input_token_limit", value: "0", wantErr: true},
		{key: "output_token_limit", value: "0", wantErr: true},
		{key: "billing_retention_days", value: "0", wantErr: false},
		{key: "budget_timezone", value: "", wantErr: true},
	}
	for _, tc := range cases {
		err := validateSettingValue("guest", tc.key, tc.value)
		if tc.wantErr && err == nil {
			t.Fatalf("guest:%s accepted %q", tc.key, tc.value)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("guest:%s rejected %q: %v", tc.key, tc.value, err)
		}
	}
}
