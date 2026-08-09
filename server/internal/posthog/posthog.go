// Package posthog is a minimal, deliberately constrained client for PostHog's
// capture API (https://posthog.com/docs/api/capture). Plain net/http, like the
// resend package — the one call Tome makes doesn't justify an SDK.
//
// This exists to answer product questions the admin analytics can't: which
// devices people actually target, which body faces get used, how often a render
// fails and why. The conversion rows in the database answer "is my server
// healthy"; this answers "what is worth building next".
//
// It is off unless an operator sets a key, and it is **their** PostHog project.
// Nothing here reports to the authors of Tome — that promise is load-bearing in
// PRIVACY.md and this package must never break it.
//
// Three privacy constraints are enforced here rather than left to call sites:
//
//   - Person profiles are disabled ($process_person_profile), so PostHog stores
//     events without building a per-person record.
//   - GeoIP enrichment is disabled. Capture is server-side, so the only address
//     PostHog ever sees is the server's own — never a reader's — and there is no
//     reason to have it resolved to a location either.
//   - Property values that look like a URL or an email are dropped before
//     sending. Tome must never disclose what someone read, and a comment asking
//     future callers to remember that is weaker than code that refuses.
package posthog

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// DefaultHost is PostHog Cloud (US). Operators can point at the EU cloud
// (https://eu.i.posthog.com) or their own instance.
const DefaultHost = "https://us.i.posthog.com"

type Client struct {
	APIKey string
	Host   string // DefaultHost unless overridden (tests use a local sink)

	// HTTP lets tests inject a client; nil means a sane default.
	HTTP *http.Client
}

// Enabled reports whether events should be sent at all. An unset key is the
// normal state: analytics is opt-in.
func (c Client) Enabled() bool { return c.APIKey != "" }

// Capture sends one event, fire-and-forget.
//
// Never returns an error and never blocks the caller: analytics must not be
// able to slow down or fail a reader's conversion. This mirrors how conversion
// bookkeeping is already treated in the API layer — logged, then swallowed.
func (c Client) Capture(event, distinctID string, props map[string]any) {
	if !c.Enabled() || event == "" {
		return
	}
	go c.capture(event, distinctID, props)
}

func (c Client) capture(event, distinctID string, props map[string]any) {
	safe := make(map[string]any, len(props)+3)
	for k, v := range props {
		if s, ok := v.(string); ok && looksIdentifying(s) {
			// Loud on purpose: this should never fire, and if it does it means
			// a call site is trying to send something it shouldn't.
			log.Printf("posthog: refusing to send property %q — looks like a URL or address", k)
			continue
		}
		safe[k] = v
	}
	// Aggregate events only: no per-person records built on PostHog's side.
	safe["$process_person_profile"] = false
	safe["$geoip_disable"] = true

	body, err := json.Marshal(map[string]any{
		"api_key":     c.APIKey,
		"event":       event,
		"distinct_id": distinctID,
		"properties":  safe,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		log.Printf("posthog: encode %s: %v", event, err)
		return
	}

	host := c.Host
	if host == "" {
		host = DefaultHost
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(host, "/")+"/i/v0/e/", bytes.NewReader(body))
	if err != nil {
		log.Printf("posthog: request %s: %v", event, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.HTTP
	if client == nil {
		// Short: a slow analytics endpoint should give up quickly rather than
		// hold a goroutine open behind a reader's request.
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("posthog: send %s: %v", event, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("posthog: send %s: %s", event, resp.Status)
	}
}

// looksIdentifying catches the two shapes that would disclose what someone
// read or who they are: a URL and an email address. Conservative by design —
// every property Tome legitimately sends is an enum, a number, or a bool, so a
// false positive costs a datapoint while a false negative costs a promise.
func looksIdentifying(s string) bool {
	return strings.Contains(s, "://") || strings.Contains(s, "@")
}
