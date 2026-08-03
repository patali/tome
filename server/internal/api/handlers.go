package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/patali/tome/server/internal/article"
	"github.com/patali/tome/server/internal/auth"
	"github.com/patali/tome/server/internal/pdfgen"
	"github.com/patali/tome/server/internal/store"
)

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"service":       "tome",
		"version":       Version,
		"authRequired":  true,
		"defaultFormat": defaultFormat(),
		"pdfAvailable":  pdfgen.Available(),
		// For the manual ("Load unpacked") install, which nothing updates: it
		// compares this against its own manifest and tells the user. A store
		// install reads it too, but only to show in settings — Chrome is
		// already keeping that one current.
		"extensionVersion": s.extensionVersion(),
	})
}

func (s *Server) meResponse(u *store.User) (map[string]any, error) {
	rc, err := s.resendClient()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"email":            u.Email,
		"kindleEmail":      u.KindleEmail,
		"isAdmin":          u.IsAdmin,
		"resendConfigured": rc.Configured(),
		"approvedSender":   rc.From,
	}, nil
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	resp, err := s.meResponse(auth.UserFrom(r.Context()))
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePutMe(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	var req struct {
		KindleEmail string `json:"kindleEmail"`
	}
	if !decodeJSON(w, r, maxAuthBody, &req) {
		return
	}
	req.KindleEmail = strings.TrimSpace(req.KindleEmail)
	if !looksLikeEmail(req.KindleEmail) {
		errJSON(w, http.StatusBadRequest, "kindleEmail doesn't look like an email address")
		return
	}
	if err := s.Store.UpdateKindleEmail(u.ID, req.KindleEmail); err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	u.KindleEmail = req.KindleEmail
	resp, err := s.meResponse(u)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// decodeArticle reads and validates the article JSON body.
func decodeArticle(w http.ResponseWriter, r *http.Request) (article.Article, bool) {
	var a article.Article
	if !decodeJSON(w, r, maxBody, &a) {
		return a, false
	}
	if a.Content == "" {
		errJSON(w, http.StatusBadRequest, "article content is empty")
		return a, false
	}
	return a, true
}

// handleConvert returns the rendered document as a download (no email).
// Format: ?format=pdf|epub, else the article's format field, else the default.
func (s *Server) handleConvert(w http.ResponseWriter, r *http.Request) {
	a, ok := decodeArticle(w, r)
	if !ok {
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = a.Format
	}
	started := time.Now()
	data, filename, contentType, err := buildDoc(a, format)
	if err != nil {
		s.record(r, "convert", format, false, 0, started)
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.record(r, "convert", format, true, int64(len(data)), started)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleSendToKindle delivers the rendered document straight to the
// authenticated user's Kindle address via Resend.
func (s *Server) handleSendToKindle(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	a, ok := decodeArticle(w, r)
	if !ok {
		return
	}
	rc, err := s.resendClient()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !rc.Configured() {
		errJSON(w, http.StatusBadGateway,
			"email delivery not configured: the admin must set Resend settings (tome admin settings set)")
		return
	}
	started := time.Now()
	data, filename, _, err := buildDoc(a, a.Format)
	if err != nil {
		s.record(r, "send", a.Format, false, 0, started)
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := rc.SendAttachment(u.KindleEmail, a.Title, "Delivered by Tome.", filename, data); err != nil {
		// Recorded as failed: from the reader's point of view nothing arrived,
		// and a run that rendered but never delivered is the one worth seeing.
		s.record(r, "send", a.Format, false, int64(len(data)), started)
		errJSON(w, http.StatusBadGateway, err.Error())
		return
	}
	s.record(r, "send", a.Format, true, int64(len(data)), started)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "method": "resend", "sentTo": u.KindleEmail,
		"filename": filename, "bytes": len(data),
	})
}

// looksLikeEmail is a sanity check, not RFC validation — Amazon and Resend do
// the real validation downstream.
func looksLikeEmail(s string) bool {
	at := strings.Index(s, "@")
	return at > 0 && at < len(s)-3 && strings.Contains(s[at:], ".") && !strings.ContainsAny(s, " \t\n")
}

// record appends a conversion row. Bookkeeping must never fail a user's
// request, so an error here is logged and swallowed rather than surfaced.
func (s *Server) record(r *http.Request, kind, format string, ok bool, bytes int64, started time.Time) {
	u := auth.UserFrom(r.Context())
	if u == nil {
		return
	}
	if format == "" {
		format = defaultFormat()
	}
	if err := s.Store.RecordConversion(u.ID, kind, format, ok, bytes,
		time.Since(started).Milliseconds()); err != nil {
		log.Printf("record conversion: %v", err)
	}
}
