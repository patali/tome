package main

import (
	"log"
	"net/http"
	"os"

	"github.com/patali/tome/server/internal/api"
	"github.com/patali/tome/server/internal/pdfgen"
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

	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}
