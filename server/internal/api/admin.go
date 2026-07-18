package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/patali/tome/server/internal/auth"
	"github.com/patali/tome/server/internal/store"
)

// adminMux serves /admin/*; the caller wraps it in RequireAdmin.
func (s *Server) adminMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/invites", s.adminCreateInvite)
	mux.HandleFunc("GET /admin/invites", s.adminListInvites)
	mux.HandleFunc("DELETE /admin/invites/{code}", s.adminDeleteInvite)
	mux.HandleFunc("GET /admin/users", s.adminListUsers)
	mux.HandleFunc("POST /admin/users/{id}/disable", s.adminSetDisabled(true))
	mux.HandleFunc("POST /admin/users/{id}/enable", s.adminSetDisabled(false))
	mux.HandleFunc("POST /admin/users/{id}/rotate-key", s.adminRotateKey)
	mux.HandleFunc("GET /admin/settings", s.adminGetSettings)
	mux.HandleFunc("PUT /admin/settings", s.adminPutSettings)
	return mux
}

func (s *Server) adminCreateInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EmailHint string `json:"emailHint"`
		TTLHours  int    `json:"ttlHours"`
		Send      bool   `json:"send"`
	}
	if !decodeJSON(w, r, maxAuthBody, &req) {
		return
	}
	if req.TTLHours <= 0 {
		req.TTLHours = 168 // one week
	}

	rc, err := s.resendClient()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Send {
		if req.EmailHint == "" {
			errJSON(w, http.StatusBadRequest, "send requires emailHint")
			return
		}
		if !rc.Configured() {
			errJSON(w, http.StatusBadRequest, "cannot email the invite: Resend is not configured")
			return
		}
	}

	inv, err := s.Store.CreateInvite(auth.NewInviteCode(), req.EmailHint, time.Duration(req.TTLHours)*time.Hour)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	emailed := false
	if req.Send {
		if err := rc.SendText(req.EmailHint, "You're invited to Tome", inviteEmailBody(inv.Code, rc.From)); err != nil {
			// The invite exists either way; report the send failure alongside it.
			writeJSON(w, http.StatusOK, map[string]any{
				"code": inv.Code, "emailHint": inv.EmailHint, "expiresAt": inv.ExpiresAt,
				"emailed": false, "emailError": err.Error(),
			})
			return
		}
		emailed = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code": inv.Code, "emailHint": inv.EmailHint, "expiresAt": inv.ExpiresAt, "emailed": emailed,
	})
}

func inviteEmailBody(code, from string) string {
	return fmt.Sprintf(`You've been invited to Tome — send web articles straight to your Kindle.

To join:

1. Install the Tome browser extension (ask the person who invited you for the link).
2. Click the Tome toolbar button -> Server settings, and set the server URL they gave you.
3. Enter this invite code with your email and your Kindle address (yourname@kindle.com):

   %s

4. Important: add %s to your Amazon "Approved Personal Document E-mail List"
   (amazon.com -> Manage Your Content and Devices -> Preferences -> Personal
   Document Settings), or Amazon will reject the deliveries.

The code is single-use and expires — redeem it soon.
`, code, from)
}

func (s *Server) adminListInvites(w http.ResponseWriter, _ *http.Request) {
	invites, err := s.Store.ListInvites()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if invites == nil {
		invites = []store.Invite{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
}

func (s *Server) adminDeleteInvite(w http.ResponseWriter, r *http.Request) {
	err := s.Store.DeleteInvite(r.PathValue("code"))
	if errors.Is(err, store.ErrNotFound) {
		errJSON(w, http.StatusNotFound, "no such invite")
		return
	}
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) adminListUsers(w http.ResponseWriter, _ *http.Request) {
	users, err := s.Store.ListUsers()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	if users == nil {
		users = []store.User{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		errJSON(w, http.StatusBadRequest, "bad user id")
		return 0, false
	}
	return id, true
}

func (s *Server) adminSetDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		if disabled && id == auth.UserFrom(r.Context()).ID {
			errJSON(w, http.StatusBadRequest, "cannot disable yourself")
			return
		}
		err := s.Store.SetDisabled(id, disabled)
		if errors.Is(err, store.ErrNotFound) {
			errJSON(w, http.StatusNotFound, "no such user")
			return
		}
		if err != nil {
			errJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (s *Server) adminRotateKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	key, hash, prefix := auth.NewAPIKey()
	err := s.Store.RotateKey(id, hash, prefix)
	if errors.Is(err, store.ErrNotFound) {
		errJSON(w, http.StatusNotFound, "no such user")
		return
	}
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"apiKey": key})
}

func (s *Server) adminGetSettings(w http.ResponseWriter, _ *http.Request) {
	set, err := s.Store.GetSettings()
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The API key is write-only: report presence, never the value.
	writeJSON(w, http.StatusOK, map[string]any{
		"resendFrom":      set.ResendFrom,
		"resendApiKeySet": set.ResendAPIKey != "",
	})
}

func (s *Server) adminPutSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ResendAPIKey *string `json:"resendApiKey"` // nil = unchanged, "" = clear
		ResendFrom   *string `json:"resendFrom"`
	}
	if !decodeJSON(w, r, maxAuthBody, &req) {
		return
	}
	if req.ResendFrom != nil && *req.ResendFrom != "" && !looksLikeEmail(*req.ResendFrom) {
		errJSON(w, http.StatusBadRequest, "resendFrom doesn't look like an email address")
		return
	}
	if err := s.Store.SetSettings(req.ResendAPIKey, req.ResendFrom); err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.adminGetSettings(w, r)
}
