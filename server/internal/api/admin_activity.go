package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/patali/tome/server/internal/auth"
	"github.com/patali/tome/server/internal/store"
)

// parseSince accepts "7d", "24h", "30m" or an RFC3339 instant. Empty means no
// lower bound.
func parseSince(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	if n := len(v); n > 1 && v[n-1] == 'd' {
		days, err := strconv.Atoi(v[:n-1])
		if err != nil {
			return time.Time{}, err
		}
		return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
	}
	if d, err := time.ParseDuration(v); err == nil {
		return time.Now().Add(-d), nil
	}
	return time.Parse(time.RFC3339, v)
}

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Stats(r.URL.Query().Get("perUser") == "true")
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) adminListConversions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	since, err := parseSince(q.Get("since"))
	if err != nil {
		errJSON(w, http.StatusBadRequest, "bad since: want 7d, 24h, or RFC3339")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	// Accept an email as well as an id: the operator has addresses to hand,
	// not row ids.
	var userID int64
	if u := strings.TrimSpace(q.Get("user")); u != "" {
		if id, err := strconv.ParseInt(u, 10, 64); err == nil {
			userID = id
		} else {
			users, err := s.Store.ListUsers()
			if err != nil {
				errJSON(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, cand := range users {
				if strings.EqualFold(cand.Email, u) {
					userID = cand.ID
					break
				}
			}
			if userID == 0 {
				errJSON(w, http.StatusNotFound, "no such user: "+u)
				return
			}
		}
	}

	list, err := s.Store.ListConversions(since, userID, limit)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []store.Conversion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversions": list})
}

func (s *Server) adminListRequests(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListInviteRequests(r.URL.Query().Get("all") == "true")
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []store.InviteRequest{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": list})
}

// adminInviteRequest turns a waiting request into a sent invite. One call
// because the two were always one decision, and doing it by hand meant
// retyping an address that was already on screen.
func (s *Server) adminInviteRequest(w http.ResponseWriter, r *http.Request) {
	req, err := s.Store.InviteRequestByRef(r.PathValue("ref"))
	if errors.Is(err, store.ErrNotFound) {
		errJSON(w, http.StatusNotFound, "no pending request matches that id or address")
		return
	}
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	rc, err := s.resendClient()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !rc.Configured() {
		errJSON(w, http.StatusBadRequest, "cannot email the invite: Resend is not configured")
		return
	}

	// An empty body is the normal case — a TTL override is rare enough that
	// requiring JSON just to omit it would be the wrong default.
	var body struct {
		TTLHours int `json:"ttlHours"`
	}
	if raw, err := io.ReadAll(io.LimitReader(r.Body, maxAuthBody)); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	if body.TTLHours <= 0 {
		body.TTLHours = 168
	}

	inv, err := s.Store.CreateInvite(auth.NewInviteCode(), req.Email,
		time.Duration(body.TTLHours)*time.Hour)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := rc.SendHTML(req.Email, "You're invited to Tome",
		inviteEmailText(inv.Code, rc.From, baseURL(r)),
		string(inviteEmailHTML(inv.Code, rc.From, baseURL(r)))); err != nil {
		// The invite exists; leave the request pending so a retry is possible
		// rather than marking it done on a send that never landed.
		writeJSON(w, http.StatusOK, map[string]any{
			"code": inv.Code, "email": req.Email, "expiresAt": inv.ExpiresAt,
			"emailed": false, "emailError": err.Error(),
		})
		return
	}
	if err := s.Store.SetInviteRequestStatus(req.ID, "invited"); err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code": inv.Code, "email": req.Email, "expiresAt": inv.ExpiresAt, "emailed": true,
	})
}

func (s *Server) adminDismissRequest(w http.ResponseWriter, r *http.Request) {
	req, err := s.Store.InviteRequestByRef(r.PathValue("ref"))
	if errors.Is(err, store.ErrNotFound) {
		errJSON(w, http.StatusNotFound, "no pending request matches that id or address")
		return
	}
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.Store.SetInviteRequestStatus(req.ID, "dismissed"); err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": req.Email})
}
