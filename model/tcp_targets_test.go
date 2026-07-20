package model

import "testing"

func TestAllTCPTargetsMergesAndDeduplicates(t *testing.T) {
	targets := AllTCPTargets()
	if len(targets) <= len(PopularWebsites) {
		t.Fatalf("expected TCP registry to add targets: got %d, websites %d", len(targets), len(PopularWebsites))
	}

	seen := make(map[string]TCPTarget, len(targets))
	for _, target := range targets {
		if target.Host == "" || target.Port != 443 {
			t.Fatalf("invalid target: %+v", target)
		}
		key := targetKey(target)
		if previous, exists := seen[key]; exists {
			t.Fatalf("duplicate target %q: %+v and %+v", key, previous, target)
		}
		seen[key] = target
	}

	for _, key := range []string{"www.google.com:443", "vercel.com:443", "www.cloudflare.com:443", "www.salesforce.com:443"} {
		if _, exists := seen[key]; !exists {
			t.Errorf("expected registry target %q", key)
		}
	}
	if source := seen["www.google.com:443"].Source; source != "popular-websites" {
		t.Errorf("existing website should take precedence, got source %q", source)
	}
}

func TestWebsiteTCPTargetsOnlyReturnsNormalizedWebsiteRegistry(t *testing.T) {
	targets := WebsiteTCPTargets()
	if len(targets) == 0 || len(targets) > len(PopularWebsites) {
		t.Fatalf("unexpected website target count: %d", len(targets))
	}
	for _, target := range targets {
		if target.Host == "" || target.Port == 0 || target.Source != "popular-websites" {
			t.Fatalf("invalid website target: %#v", target)
		}
	}
}

func TestAllTCPTargetsReturnsCopy(t *testing.T) {
	first := AllTCPTargets()
	first[0].Host = "changed.invalid"
	second := AllTCPTargets()
	if second[0].Host == "changed.invalid" {
		t.Fatal("registry returned shared mutable state")
	}
}
