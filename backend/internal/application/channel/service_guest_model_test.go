package channel

import "testing"

func TestListGuestModelsFollowsWhitelistOrderAndIgnoresUnknownRefs(t *testing.T) {
	views := []ModelView{
		{ID: 1, PlatformModelName: "gpt-4o"},
		{ID: 2, PlatformModelName: "claude-sonnet"},
		{ID: 3, PlatformModelName: "gemini-flash"},
	}
	order, allowed := normalizeGuestModelRefs([]string{" Claude-Sonnet ", "gpt-4o", "gpt-4o", "not-configured", ""})
	if len(order) != 3 || order[0] != "claude-sonnet" || order[1] != "gpt-4o" || order[2] != "not-configured" {
		t.Fatalf("unexpected normalized refs: %#v", order)
	}
	results := filterGuestModelViews(views, order, allowed)
	if len(results) != 2 {
		t.Fatalf("expected two whitelisted models, got %#v", results)
	}
	if results[0].PlatformModelName != "claude-sonnet" || results[1].PlatformModelName != "gpt-4o" {
		t.Fatalf("whitelist order must be preserved: %#v", results)
	}
	if got := filterGuestModelViews(views, nil, map[string]struct{}{}); len(got) != 0 {
		t.Fatalf("empty whitelist must hide every model, got %#v", got)
	}
	order, allowed = normalizeGuestModelRefs([]string{"gemini-flash"})
	results = filterGuestModelViews(views, order, allowed)
	if len(results) != 1 || results[0].ID != 3 {
		t.Fatalf("expected only the whitelisted model, got %#v", results)
	}
}
