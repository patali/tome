package main

import (
	"log"
	"net/http"
	"os"

	"github.com/patali/tome/server/internal/api"
	"github.com/patali/tome/server/internal/pdfgen"
	"github.com/patali/tome/server/internal/store"
)

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
