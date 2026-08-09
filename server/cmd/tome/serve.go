package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/patali/tome/server/internal/api"
	"github.com/patali/tome/server/internal/pdfgen"
	"github.com/patali/tome/server/internal/posthog"
	"github.com/patali/tome/server/internal/store"
)

// extensionPath locates the extension bundle served at /extension.zip: a
// prebuilt zip in the container image, or the source dir when run from the
// repo (`go run ./cmd/tome` inside server/).
func extensionPath() string {
	if p := os.Getenv("TOME_EXTENSION_PATH"); p != "" {
		return p
	}
	for _, p := range []string{"/opt/tome/extension.zip", "../extension"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// privacyPath locates PRIVACY.md, served at /privacy. Same shape as
// extensionPath: baked into the image, or the repo copy when run from source.
// The repo file stays the single source of truth — nothing is duplicated.
func privacyPath() string {
	if p := os.Getenv("TOME_PRIVACY_PATH"); p != "" {
		return p
	}
	for _, p := range []string{"/opt/tome/PRIVACY.md", "../PRIVACY.md"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func dataDir() string {
	if d := os.Getenv("TOME_DATA_DIR"); d != "" {
		return d
	}
	return "./data"
}

func runServe(_ []string) {
	addr := ":8080"
	if p := os.Getenv("TOME_PORT"); p != "" {
		addr = ":" + p
	}

	st, err := store.Open(dataDir())
	if err != nil {
		fatal("open store: %v", err)
	}
	defer st.Close()

	srv := api.New(st, os.Getenv("TOME_RESEND_BASE_URL"))
	srv.PostHogBase = os.Getenv("TOME_POSTHOG_BASE_URL")
	srv.ExtensionPath = extensionPath()
	srv.PrivacyPath = privacyPath()

	log.Printf("tome %s listening on http://localhost%s (data: %s)", api.Version, addr, dataDir())
	if !pdfgen.Available() {
		log.Printf("no Chrome-family browser found — PDF disabled, EPUB fallback in use (set TOME_CHROME)")
	}
	if ok, err := st.AdminExists(); err == nil && !ok {
		log.Printf("no admin account yet — create one with: tome init-admin --email you@example.com --kindle you@kindle.com")
	}
	set, err := st.GetSettings()
	switch {
	case err != nil:
		log.Printf("settings unreadable: %v", err)
	case set.ResendAPIKey != "" && set.ResendFrom != "":
		log.Printf("delivery: Resend as %s", set.ResendFrom)
	default:
		log.Printf("delivery: Resend not configured (tome admin settings set); Mail.app fallback for admin on macOS only")
	}
	// Stated at boot because it is the one setting that sends data to a third
	// party. An operator should never have to read the database to find out
	// whether their server is talking to PostHog.
	if err == nil {
		if set.PostHogAPIKey != "" {
			host := set.PostHogHost
			if host == "" {
				host = posthog.DefaultHost
			}
			log.Printf("analytics: PostHog enabled, sending to %s", host)
		} else {
			log.Printf("analytics: off (local conversion records only)")
		}
	}

	// Worth a line at boot because the failure is silent and lands on someone
	// else: pages served through a proxy get the right host from the request,
	// but an invite sent from the admin CLI is generated on a 127.0.0.1
	// request and would carry links the recipient cannot open.
	if os.Getenv("TOME_BASE_URL") == "" {
		log.Printf("TOME_BASE_URL is unset — emailed links use the requesting host, " +
			"so invites sent from the admin CLI will point at localhost")
	}

	startSweeper(st)
	srv.TrackServerStart()

	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}

// startSweeper enforces the retention periods PRIVACY.md publishes. Runs once
// at boot and daily after: this host reboots often enough that a boot-only
// sweep would be unpredictable, and a timer alone would never run on a machine
// that is restarted before it fires.
func startSweeper(st *store.Store) {
	sweep := func() {
		conversions, requests, invites, err := st.Sweep()
		if err != nil {
			log.Printf("sweep: %v", err)
			return
		}
		if conversions+requests+invites > 0 {
			log.Printf("sweep: removed %d conversions, %d invite requests, %d expired invites",
				conversions, requests, invites)
		}
	}
	sweep()
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for range t.C {
			sweep()
		}
	}()
}
