package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/patali/tome/server/internal/auth"
)

// Where invite requests are delivered, and the Turnstile keys that guard the
// form. All optional: with no notify address the endpoint reports itself
// unavailable rather than silently swallowing requests, and the landing page
// omits the form entirely.
// TURNSTILE_* deliberately break this project's TOME_ prefix: they are
// Cloudflare's canonical names, and matching them means the widget's own
// documentation applies verbatim to this deployment.
var (
	inviteNotify    = strings.TrimSpace(os.Getenv("TOME_INVITE_NOTIFY"))
	turnstileSite   = strings.TrimSpace(os.Getenv("TURNSTILE_SITE_KEY"))
	turnstileSecret = strings.TrimSpace(os.Getenv("TURNSTILE_SECRET"))
)

// turnstileEndpoint is a var so tests can point it at a stub.
var turnstileEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// inviteFormEnabled reports whether the landing page should render the form.
// Without somewhere to deliver to there is nothing useful to collect.
func inviteFormEnabled() bool { return inviteNotify != "" }

type inviteRequestBody struct {
	Email string `json:"email"`
	// Website is a honeypot: hidden from people, filled by naive bots. Named
	// for what an autofilling bot expects to see, not for what it does.
	Website   string `json:"website"`
	Turnstile string `json:"turnstile"`
}

// handleInviteRequest takes an address from the landing page and mails it to
// the operator. It never emails the submitted address: doing so would turn the
// form into an open relay for sending mail to strangers, which is exactly the
// abuse a captcha is meant to prevent.
func (s *Server) handleInviteRequest(w http.ResponseWriter, r *http.Request) {
	if !inviteFormEnabled() {
		errJSON(w, http.StatusServiceUnavailable, "invite requests are not enabled on this server")
		return
	}

	var body inviteRequestBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBody)).Decode(&body); err != nil {
		errJSON(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Honeypot: answer exactly as success does. Telling a bot it was detected
	// only teaches whoever wrote it to stop filling the field.
	if strings.TrimSpace(body.Website) != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	addr, err := mail.ParseAddress(strings.TrimSpace(body.Email))
	if err != nil {
		errJSON(w, http.StatusBadRequest, "that doesn't look like an email address")
		return
	}
	email := addr.Address
	if len(email) > 254 { // RFC 5321 maximum
		errJSON(w, http.StatusBadRequest, "that doesn't look like an email address")
		return
	}

	if turnstileConfigured() {
		ok, err := verifyTurnstile(r.Context(), body.Turnstile, auth.ClientIP(r))
		if err != nil {
			// Cloudflare unreachable. Refusing is the safer failure: the whole
			// point of the check is that this endpoint sends mail.
			log.Printf("invite-request: turnstile verify failed: %v", err)
			errJSON(w, http.StatusServiceUnavailable, "could not verify the challenge, try again shortly")
			return
		}
		if !ok {
			errJSON(w, http.StatusForbidden, "challenge failed, please try again")
			return
		}
	}

	client, err := s.resendClient()
	if err != nil || !client.Configured() {
		log.Printf("invite-request: no delivery configured, dropped request from %s", email)
		errJSON(w, http.StatusServiceUnavailable, "email delivery is not configured on this server")
		return
	}

	// Deliberately no IP. It was here for abuse triage, but Turnstile and the
	// rate limiter already do that job before this point — so recording it
	// would mean keeping an identifier about someone who is not yet a user,
	// in a mailbox, for no decision it would actually inform.
	text := fmt.Sprintf(""+
		"Someone asked for a Tome invite.\n\n"+
		"  email: %s\n"+
		"  when:  %s\n\n"+
		"Create one with:\n"+
		"  tome admin invites create --email %s --send\n",
		email, time.Now().UTC().Format(time.RFC3339), email)

	// The submitted address goes in the subject, never the To/Reply-To: a
	// header built from unverified input is how a form becomes a spam relay.
	if err := client.SendText(inviteNotify, "Tome invite request: "+email, text); err != nil {
		log.Printf("invite-request: send failed: %v", err)
		errJSON(w, http.StatusBadGateway, "could not pass that on right now, try again shortly")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func turnstileConfigured() bool { return turnstileSite != "" && turnstileSecret != "" }

// verifyTurnstile checks a Turnstile token with Cloudflare. Reports (false,
// nil) when the token is genuinely bad, and an error when the verdict could
// not be obtained — the caller treats those differently.
func verifyTurnstile(ctx context.Context, token, ip string) (bool, error) {
	if strings.TrimSpace(token) == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	form := url.Values{"secret": {turnstileSecret}, "response": {token}}
	if ip != "" {
		form.Set("remoteip", ip)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("turnstile returned %s", resp.Status)
	}

	var out struct {
		Success bool     `json:"success"`
		Errors  []string `json:"error-codes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<16)).Decode(&out); err != nil {
		return false, err
	}
	if !out.Success && len(out.Errors) > 0 {
		log.Printf("invite-request: turnstile rejected: %v", out.Errors)
	}
	return out.Success, nil
}
