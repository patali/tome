package posthog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// sink captures one event and hands it back, so the fire-and-forget goroutine
// can be awaited without sleeping.
func sink(t *testing.T) (*httptest.Server, <-chan map[string]any) {
	t.Helper()
	got := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		got <- body
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func await(t *testing.T, ch <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case body := <-ch:
		return body
	case <-time.After(3 * time.Second):
		t.Fatal("no event received")
		return nil
	}
}

func props(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	p, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %#v", body["properties"])
	}
	return p
}

// Analytics is opt-in. Without a key nothing should ever leave the server —
// this is the difference between "Tome has no telemetry" being true and false.
func TestDisabledWithoutAPIKey(t *testing.T) {
	srv, got := sink(t)
	c := Client{Host: srv.URL} // no APIKey
	if c.Enabled() {
		t.Fatal("client with no API key reports enabled")
	}
	c.Capture("conversion", "u1", map[string]any{"format": "pdf"})

	select {
	case body := <-got:
		t.Fatalf("event sent despite no API key: %#v", body)
	case <-time.After(300 * time.Millisecond):
	}
}

// The guard that keeps a promise: PRIVACY.md says the server records no title,
// no URL and no domain. A future call site that tries to add one should lose
// the property rather than the policy.
func TestDropsURLAndEmailProperties(t *testing.T) {
	srv, got := sink(t)
	c := Client{APIKey: "phc_test", Host: srv.URL}

	c.Capture("conversion", "u1", map[string]any{
		"format": "pdf",
		"url":    "https://example.com/private-article",
		"email":  "reader@example.com",
		"bytes":  "256k-1m",
	})

	p := props(t, await(t, got))
	for _, banned := range []string{"url", "email"} {
		if _, present := p[banned]; present {
			t.Errorf("property %q was sent; it looks identifying and must be dropped", banned)
		}
	}
	if p["format"] != "pdf" {
		t.Errorf("format = %v, want pdf — safe properties must survive", p["format"])
	}
	if p["bytes"] != "256k-1m" {
		t.Errorf("bytes = %v, want 256k-1m", p["bytes"])
	}
}

// Person profiles and GeoIP are disabled in the client, not at call sites, so
// no caller can forget.
func TestPrivacyFlagsAlwaysSet(t *testing.T) {
	srv, got := sink(t)
	c := Client{APIKey: "phc_test", Host: srv.URL}
	c.Capture("server_started", "server", nil)

	p := props(t, await(t, got))
	if p["$process_person_profile"] != false {
		t.Errorf("$process_person_profile = %v, want false", p["$process_person_profile"])
	}
	if p["$geoip_disable"] != true {
		t.Errorf("$geoip_disable = %v, want true", p["$geoip_disable"])
	}
}

func TestPayloadShape(t *testing.T) {
	srv, got := sink(t)
	c := Client{APIKey: "phc_test", Host: srv.URL}
	c.Capture("conversion", "u42", map[string]any{"ok": true})

	body := await(t, got)
	if body["api_key"] != "phc_test" {
		t.Errorf("api_key = %v", body["api_key"])
	}
	if body["event"] != "conversion" {
		t.Errorf("event = %v", body["event"])
	}
	if body["distinct_id"] != "u42" {
		t.Errorf("distinct_id = %v", body["distinct_id"])
	}
	if _, ok := body["timestamp"].(string); !ok {
		t.Errorf("timestamp missing: %#v", body["timestamp"])
	}
}

// A failing analytics endpoint must never become a failing conversion.
func TestCaptureSurvivesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := Client{APIKey: "phc_test", Host: srv.URL}
	c.Capture("conversion", "u1", nil) // must not panic; returns immediately
	time.Sleep(200 * time.Millisecond)
}

func TestLooksIdentifying(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"https://example.com/a", true},
		{"http://x.com/1/article/2", true},
		{"reader@example.com", true},
		{"pdf", false},
		{"scribe", false},
		{"256k-1m", false},
		{"render", false},
		{"", false},
	} {
		if got := looksIdentifying(tc.in); got != tc.want {
			t.Errorf("looksIdentifying(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
