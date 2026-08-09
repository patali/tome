// Package api is Tome's HTTP surface: public status/invite-redemption,
// authed convert/send endpoints, and the admin JSON API.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/patali/tome/server/internal/article"
	"github.com/patali/tome/server/internal/auth"
	"github.com/patali/tome/server/internal/epubgen"
	"github.com/patali/tome/server/internal/pdfgen"
	"github.com/patali/tome/server/internal/posthog"
	"github.com/patali/tome/server/internal/resend"
	"github.com/patali/tome/server/internal/store"
)

const Version = "0.2.0"

const maxBody = 8 << 20 // 8 MiB for article payloads
const maxAuthBody = 4 << 10

type Server struct {
	Store         *store.Store
	ResendBase    string // override for tests; "" = resend.DefaultBaseURL
	PostHogBase   string // override for tests; "" = posthog.DefaultHost
	ExtensionPath string // extension zip (or source dir) served at /extension.zip
	PrivacyPath   string // PRIVACY.md, served at /privacy
	limiter       *auth.Limiter
	inviteLimiter *auth.Limiter
}

func New(st *store.Store, resendBase string) *Server {
	return &Server{
		Store:      st,
		ResendBase: resendBase,
		limiter:    auth.NewLimiter(10, time.Hour),
		// Counts every attempt, including ones rejected for a malformed
		// address, so this has to leave room for a person fumbling their own
		// email a couple of times before it starts refusing them.
		inviteLimiter: auth.NewLimiter(5, time.Hour),
	}
}

// TrackServerStart records that this build came up, so an operator can see in
// their own analytics when a deploy landed and whether PDF rendering is
// actually available on the machine — the single most common way a self-hosted
// server is quietly degraded (no Chrome, so everything silently falls to EPUB).
//
// Called by the serve command rather than from New so that CLI subcommands and
// tests don't report themselves as server starts.
func (s *Server) TrackServerStart() {
	s.analytics().Capture("server_started", "server", map[string]any{
		"version":        Version,
		"pdf_available":  pdfgen.Available(),
		"default_format": defaultFormat(),
	})
}

// Handler builds the full route table (Go 1.22 method patterns handle 405s).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", s.handleStatus)
	// {$} matches only "/" — unknown paths still 404 rather than land here.
	mux.HandleFunc("GET /{$}", s.handleLandingPage)
	mux.HandleFunc("GET /install", s.handleInstallPage)
	mux.HandleFunc("GET /fonts/{name}", s.handleFont)
	mux.HandleFunc("GET /icon.png", s.handleIcon)
	mux.HandleFunc("GET /extension.zip", s.handleExtensionZip)
	mux.HandleFunc("GET /privacy", s.handlePrivacy)
	mux.Handle("POST /auth/accept-invite", s.limiter.Wrap(http.HandlerFunc(s.handleAcceptInvite)))
	// Tighter than invite redemption: this one sends mail, and a person only
	// ever needs it once.
	mux.Handle("POST /invite-request", s.inviteLimiter.Wrap(http.HandlerFunc(s.handleInviteRequest)))

	mux.Handle("POST /convert", auth.RequireUser(s.Store, http.HandlerFunc(s.handleConvert)))
	mux.Handle("POST /send-to-kindle", auth.RequireUser(s.Store, http.HandlerFunc(s.handleSendToKindle)))
	mux.Handle("GET /me", auth.RequireUser(s.Store, http.HandlerFunc(s.handleMe)))
	mux.Handle("PUT /me", auth.RequireUser(s.Store, http.HandlerFunc(s.handlePutMe)))

	mux.Handle("/admin/", auth.RequireAdmin(s.Store, s.adminMux()))

	return cors(mux)
}

// resendClient snapshots the stored settings into a ready-to-use client.
func (s *Server) resendClient() (resend.Client, error) {
	set, err := s.Store.GetSettings()
	if err != nil {
		return resend.Client{}, err
	}
	return resend.Client{APIKey: set.ResendAPIKey, From: set.ResendFrom, BaseURL: s.ResendBase}, nil
}

// analytics snapshots the stored settings into a PostHog client. A settings
// read that fails yields a disabled client rather than an error: analytics is
// never worth failing a request over.
func (s *Server) analytics() posthog.Client {
	set, err := s.Store.GetSettings()
	if err != nil {
		return posthog.Client{}
	}
	host := set.PostHogHost
	if s.PostHogBase != "" {
		host = s.PostHogBase
	}
	return posthog.Client{APIKey: set.PostHogAPIKey, Host: host}
}

// analyticsID is the pseudonymous subject of an event.
//
// A bare account number, never an email — PostHog has no way to resolve it, and
// person profiles are disabled anyway, so it serves only to distinguish "ten
// conversions by one person" from "one each by ten". The operator can already
// see which account converted what in their own database; this discloses
// strictly less than that.
func analyticsID(userID int64) string {
	return "u" + strconv.FormatInt(userID, 10)
}

// bytesBucket coarsens a document size into an order of magnitude.
//
// Exact byte counts are close to a fingerprint — a specific size, at a specific
// minute, identifies a specific article to anyone holding a copy of it. The
// useful signal here is "are people sending long reads or short ones", and a
// bucket carries that without carrying the rest.
func bytesBucket(n int64) string {
	switch {
	case n <= 0:
		return "none"
	case n < 256<<10:
		return "<256k"
	case n < 1<<20:
		return "256k-1m"
	case n < 4<<20:
		return "1m-4m"
	default:
		return ">4m"
	}
}

// cors allows the browser extension (a chrome-extension:// origin) to call
// in. Wildcard origin is safe here: auth is a Bearer header (never cookies),
// so cross-site requests can't ride an ambient credential.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// decodeJSON reads a size-capped JSON body into v.
func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		errJSON(w, http.StatusBadRequest, "read body: "+err.Error())
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		errJSON(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// defaultFormat prefers PDF (faithful, images load) when Chrome is available.
func defaultFormat() string {
	if pdfgen.Available() {
		return "pdf"
	}
	return "epub"
}

// buildDoc renders the article to the requested format ("pdf" | "epub" | "").
func buildDoc(a article.Article, format string) (data []byte, filename, contentType string, err error) {
	if format == "" {
		format = defaultFormat()
	}
	switch format {
	case "pdf":
		data, filename, err = pdfgen.Build(a)
		return data, filename, "application/pdf", err
	case "epub":
		data, filename, err = epubgen.Build(a)
		return data, filename, "application/epub+zip", err
	default:
		return nil, "", "", fmt.Errorf("unknown format %q (use pdf or epub)", format)
	}
}
