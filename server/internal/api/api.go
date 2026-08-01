// Package api is Tome's HTTP surface: public status/invite-redemption,
// authed convert/send endpoints, and the admin JSON API.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/patali/tome/server/internal/article"
	"github.com/patali/tome/server/internal/auth"
	"github.com/patali/tome/server/internal/epubgen"
	"github.com/patali/tome/server/internal/pdfgen"
	"github.com/patali/tome/server/internal/resend"
	"github.com/patali/tome/server/internal/store"
)

const Version = "0.2.0"

const maxBody = 8 << 20 // 8 MiB for article payloads
const maxAuthBody = 4 << 10

type Server struct {
	Store         *store.Store
	ResendBase    string // override for tests; "" = resend.DefaultBaseURL
	ExtensionPath string // extension zip (or source dir) served at /extension.zip
	PrivacyPath   string // PRIVACY.md, served at /privacy
	limiter       *auth.Limiter
}

func New(st *store.Store, resendBase string) *Server {
	return &Server{
		Store:      st,
		ResendBase: resendBase,
		limiter:    auth.NewLimiter(10, time.Hour),
	}
}

// Handler builds the full route table (Go 1.22 method patterns handle 405s).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", s.handleStatus)
	// {$} matches only "/" — unknown paths still 404 rather than land here.
	mux.HandleFunc("GET /{$}", s.handleInstallPage)
	mux.HandleFunc("GET /install", s.handleInstallPage)
	mux.HandleFunc("GET /extension.zip", s.handleExtensionZip)
	mux.HandleFunc("GET /privacy", s.handlePrivacy)
	mux.Handle("POST /auth/accept-invite", s.limiter.Wrap(http.HandlerFunc(s.handleAcceptInvite)))

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
