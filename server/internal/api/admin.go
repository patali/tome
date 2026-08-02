package api

import (
	"errors"
	"fmt"
	"html/template"
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
	mux.HandleFunc("GET /admin/stats", s.adminStats)
	mux.HandleFunc("GET /admin/conversions", s.adminListConversions)
	mux.HandleFunc("GET /admin/requests", s.adminListRequests)
	mux.HandleFunc("POST /admin/requests/{ref}/invite", s.adminInviteRequest)
	mux.HandleFunc("POST /admin/requests/{ref}/dismiss", s.adminDismissRequest)
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
	base, baseWarning := emailBase(r)
	if req.Send {
		if err := rc.SendHTML(req.EmailHint, "You're invited to Tome",
			inviteEmailText(inv.Code, rc.From, base),
			string(inviteEmailHTML(inv.Code, rc.From, base))); err != nil {
			// The invite exists either way; report the send failure alongside it.
			writeJSON(w, http.StatusOK, map[string]any{
				"code": inv.Code, "emailHint": inv.EmailHint, "expiresAt": inv.ExpiresAt,
				"emailed": false, "emailError": err.Error(),
			})
			return
		}
		emailed = true
	}
	out := map[string]any{
		"code": inv.Code, "emailHint": inv.EmailHint, "expiresAt": inv.ExpiresAt, "emailed": emailed,
	}
	if emailed && baseWarning != "" {
		out["warning"] = baseWarning
	}
	writeJSON(w, http.StatusOK, out)
}

// inviteEmailText is the plain-text part. Not a fallback afterthought: some
// people read mail as text by choice, and a missing text part is a spam signal.
func inviteEmailText(code, from, base string) string {
	return fmt.Sprintf(`You've been invited to Tome — send web articles straight to your Kindle.

Your invite code: %s

To join:

1. Download the extension and follow the install steps here:

   %s/install

2. In the extension's settings, set the server URL to %s and redeem the code
   above with your email and your Kindle address (yourname@kindle.com).

3. Important: add %s to your Amazon "Approved Personal Document E-mail List"
   (amazon.com -> Manage Your Content and Devices -> Preferences -> Personal
   Document Settings), or Amazon accepts the deliveries and discards them.

The code is single-use and expires — redeem it soon.
`, code, base, base, from)
}

// inviteEmailHTML matches the site: same paper, ink and accent, same editorial
// shape. Written the way email demands rather than the way the site is —
// table-based layout, every style inline, no webfont. Literata cannot be
// loaded here, so the stack falls to Georgia, which is on essentially every
// client and shares the same bookish proportions.
func inviteEmailHTML(code, from, base string) template.HTML {
	const (
		paper  = "#fbf6ec"
		tile   = "#f3ebda"
		ink    = "#211d17"
		body   = "#3a352b"
		muted  = "#6b6357"
		accent = "#bb4a1f"
		rule   = "#ece3d2"
		serif  = "Georgia, 'Times New Roman', Times, serif"
	)
	step := func(n int, html string) string {
		return fmt.Sprintf(`<tr>
      <td width="34" valign="top" style="padding:0 0 18px;font-family:%s;font-size:20px;font-style:italic;color:#c9a24a;line-height:1.3;">%d</td>
      <td valign="top" style="padding:0 0 18px;font-family:%s;font-size:16px;line-height:1.6;color:%s;">%s</td>
    </tr>`, serif, n, serif, body, html)
	}

	return template.HTML(fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>You're invited to Tome</title></head>
<body style="margin:0;padding:0;background:%s;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="background:%s;">
<tr><td align="center" style="padding:36px 16px;">
<table role="presentation" width="560" cellpadding="0" cellspacing="0" border="0" style="width:560px;max-width:100%%;">

  <tr><td style="padding:0 0 26px;font-family:%s;font-size:21px;font-style:italic;color:%s;">Tome</td></tr>

  <tr><td style="padding:0 0 6px;font-family:%s;font-size:12px;letter-spacing:3px;text-transform:uppercase;color:#9a7a55;">You're invited</td></tr>
  <tr><td style="padding:0 0 14px;font-family:%s;font-size:30px;line-height:1.2;color:%s;font-weight:bold;">Read it properly.</td></tr>
  <tr><td style="padding:0 0 26px;font-family:%s;font-size:17px;line-height:1.6;color:%s;">Tome turns the article you're reading into a typeset document and puts it on your Kindle — one click.</td></tr>

  <tr><td style="padding:0 0 8px;">
    <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="background:%s;border-radius:8px;">
      <tr><td align="center" style="padding:22px 20px;font-family:%s;font-size:12px;letter-spacing:2px;text-transform:uppercase;color:%s;">Your invite code</td></tr>
      <tr><td align="center" style="padding:0 20px 24px;font-family:'SFMono-Regular',Menlo,Consolas,monospace;font-size:20px;letter-spacing:1px;line-height:1.4;word-break:break-all;color:%s;font-weight:bold;">%s</td></tr>
    </table>
  </td></tr>

  <tr><td style="padding:26px 0 0;border-top:1px solid %s;"></td></tr>

  <tr><td>
    <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0">
      %s
      %s
      %s
    </table>
  </td></tr>

  <tr><td style="padding:6px 0 26px;">
    <a href="%s/install" style="display:inline-block;background:%s;color:%s;font-family:%s;font-size:16px;text-decoration:none;padding:13px 26px;border-radius:8px;">Install the extension</a>
  </td></tr>

  <tr><td style="padding:22px 0 0;border-top:1px solid %s;font-family:%s;font-size:14px;line-height:1.6;color:%s;">
    The code is single-use and expires — redeem it soon. If you weren't expecting this, you can ignore it.
  </td></tr>

</table>
</td></tr>
</table>
</body></html>`,
		paper, paper,
		serif, ink,
		serif,
		serif, ink,
		serif, body,
		tile, serif, muted, ink, template.HTMLEscapeString(code),
		rule,
		step(1, fmt.Sprintf(`Install the extension — the steps are at <a href="%s/install" style="color:%s;">%s/install</a>.`, base, accent, base)),
		step(2, fmt.Sprintf(`In the extension's settings set the server to <b>%s</b>, then redeem the code above with your email and your Kindle address.`, base)),
		step(3, fmt.Sprintf(`Add <b>%s</b> to your Amazon <b>Approved Personal Document E-mail List</b>. Skip this and Amazon accepts the deliveries and quietly discards them.`, template.HTMLEscapeString(from))),
		base, accent, paper, serif,
		rule, serif, muted))
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
