package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/patali/tome/server/internal/auth"
	"github.com/patali/tome/server/internal/store"
)

// handleAcceptInvite redeems a single-use invite code and returns the new
// account's API key (shown exactly once). Rate-limited per IP by the caller.
func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code        string `json:"code"`
		Email       string `json:"email"`
		KindleEmail string `json:"kindleEmail"`
	}
	if !decodeJSON(w, r, maxAuthBody, &req) {
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	req.Email = strings.TrimSpace(req.Email)
	req.KindleEmail = strings.TrimSpace(req.KindleEmail)

	if req.Code == "" || req.Email == "" || req.KindleEmail == "" {
		errJSON(w, http.StatusBadRequest, "code, email, and kindleEmail are required")
		return
	}
	if !looksLikeEmail(req.Email) || !looksLikeEmail(req.KindleEmail) {
		errJSON(w, http.StatusBadRequest, "email or kindleEmail doesn't look like an email address")
		return
	}

	key, hash, prefix := auth.NewAPIKey()
	if _, err := s.Store.Redeem(req.Code, req.Email, req.KindleEmail, hash, prefix); err != nil {
		switch {
		case errors.Is(err, store.ErrEmailExists):
			errJSON(w, http.StatusConflict, "an account with this email already exists")
		case errors.Is(err, store.ErrInviteInvalid):
			errJSON(w, http.StatusBadRequest, store.ErrInviteInvalid.Error())
		default:
			errJSON(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	set, err := s.Store.GetSettings()
	if err != nil {
		set = store.Settings{} // account exists; don't fail the response over this
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"apiKey":         key,
		"email":          req.Email,
		"kindleEmail":    req.KindleEmail,
		"approvedSender": set.ResendFrom,
	})
}
