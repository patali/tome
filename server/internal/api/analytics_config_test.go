package api

import (
	"testing"

	"github.com/patali/tome/server/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st, "")
}

func setStored(t *testing.T, s *Server, key, host string) {
	t.Helper()
	if err := s.Store.SetSettings(nil, nil, &key, &host); err != nil {
		t.Fatalf("store settings: %v", err)
	}
}

// Analytics stays opt-in: neither source configured means nothing is sent.
func TestAnalyticsOffByDefault(t *testing.T) {
	s := newTestServer(t)
	enabled, _, src := s.AnalyticsStatus()
	if enabled {
		t.Error("analytics enabled with nothing configured")
	}
	if src != AnalyticsOff {
		t.Errorf("source = %q, want %q", src, AnalyticsOff)
	}
	if s.analytics().Enabled() {
		t.Error("client enabled with nothing configured")
	}
}

func TestAnalyticsFromStoredSettings(t *testing.T) {
	s := newTestServer(t)
	setStored(t, s, "phc_stored", "https://eu.i.posthog.com")

	enabled, host, src := s.AnalyticsStatus()
	if !enabled || src != AnalyticsFromStorage {
		t.Fatalf("enabled=%v source=%q, want true/%q", enabled, src, AnalyticsFromStorage)
	}
	if host != "https://eu.i.posthog.com" {
		t.Errorf("host = %q", host)
	}
	if got := s.analytics().APIKey; got != "phc_stored" {
		t.Errorf("key = %q, want phc_stored", got)
	}
}

// The environment wins, and supplies both halves — a deployment that declares
// analytics in its compose .env must be readable from that file alone.
func TestEnvironmentOverridesStoredSettings(t *testing.T) {
	s := newTestServer(t)
	setStored(t, s, "phc_stored", "https://eu.i.posthog.com")
	s.PostHogEnvKey = "phc_env"
	s.PostHogEnvHost = "https://us.i.posthog.com"

	enabled, host, src := s.AnalyticsStatus()
	if !enabled || src != AnalyticsFromEnv {
		t.Fatalf("enabled=%v source=%q, want true/%q", enabled, src, AnalyticsFromEnv)
	}
	if host != "https://us.i.posthog.com" {
		t.Errorf("host = %q — the env host must win, not the stored one", host)
	}
	if got := s.analytics().APIKey; got != "phc_env" {
		t.Errorf("key = %q, want phc_env", got)
	}
}

// The stored host must not leak into an environment-configured deployment:
// half the answer in a database nobody thought to check is the failure this
// precedence rule exists to prevent.
func TestEnvKeyWithoutHostDoesNotInheritStoredHost(t *testing.T) {
	s := newTestServer(t)
	setStored(t, s, "phc_stored", "https://eu.i.posthog.com")
	s.PostHogEnvKey = "phc_env" // no env host

	_, host, src := s.AnalyticsStatus()
	if src != AnalyticsFromEnv {
		t.Fatalf("source = %q, want %q", src, AnalyticsFromEnv)
	}
	if host != "https://us.i.posthog.com" {
		t.Errorf("host = %q, want the default — a stored host must not apply here", host)
	}
}

// An env host alone configures nothing; the key is what turns analytics on.
func TestEnvHostAloneDoesNotEnable(t *testing.T) {
	s := newTestServer(t)
	s.PostHogEnvHost = "https://eu.i.posthog.com"

	enabled, _, src := s.AnalyticsStatus()
	if enabled || src != AnalyticsOff {
		t.Errorf("enabled=%v source=%q, want false/%q", enabled, src, AnalyticsOff)
	}
}

// Clearing the env var falls back to whatever was stored, so a stack can hand
// control back to the admin CLI without losing its configuration.
func TestFallsBackToStoredWhenEnvCleared(t *testing.T) {
	s := newTestServer(t)
	setStored(t, s, "phc_stored", "")
	s.PostHogEnvKey = "phc_env"
	if _, _, src := s.AnalyticsStatus(); src != AnalyticsFromEnv {
		t.Fatalf("source = %q, want %q", src, AnalyticsFromEnv)
	}

	s.PostHogEnvKey = ""
	enabled, _, src := s.AnalyticsStatus()
	if !enabled || src != AnalyticsFromStorage {
		t.Errorf("enabled=%v source=%q, want true/%q", enabled, src, AnalyticsFromStorage)
	}
}
