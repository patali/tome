// Package resend is a minimal client for the Resend email API
// (https://resend.com/docs/api-reference/emails/send-email). Plain net/http —
// the two calls Tome makes don't justify an SDK dependency.
package resend

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const DefaultBaseURL = "https://api.resend.com"

type Client struct {
	APIKey  string
	From    string
	BaseURL string // DefaultBaseURL unless overridden (tests use a local sink)
}

// Configured reports whether the client can send.
func (c Client) Configured() bool { return c.APIKey != "" && c.From != "" }

type attachment struct {
	Filename string `json:"filename"`
	Content  string `json:"content"` // base64
}

type payload struct {
	From        string       `json:"from"`
	To          []string     `json:"to"`
	Subject     string       `json:"subject"`
	Text        string       `json:"text"`
	HTML        string       `json:"html,omitempty"`
	Attachments []attachment `json:"attachments,omitempty"`
}

func (c Client) send(p payload) error {
	if !c.Configured() {
		return fmt.Errorf("resend not configured")
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, base+"/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second} // attachments can be MBs
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var apiErr struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&apiErr)
	if apiErr.Message == "" {
		apiErr.Message = "request failed"
	}
	return fmt.Errorf("resend: %s: %s", resp.Status, apiErr.Message)
}

// SendAttachment emails a single file (e.g. the rendered PDF) to `to`.
func (c Client) SendAttachment(to, subject, text, filename string, data []byte) error {
	return c.send(payload{
		From: c.From, To: []string{to}, Subject: subject, Text: text,
		Attachments: []attachment{{Filename: filename, Content: base64.StdEncoding.EncodeToString(data)}},
	})
}

// SendText emails a plain-text message (used for invites).
func (c Client) SendText(to, subject, text string) error {
	return c.send(payload{From: c.From, To: []string{to}, Subject: subject, Text: text})
}

// SendHTML emails an HTML message with a plain-text alternative. Both are
// required, not optional: a text part is what keeps the message readable in
// clients that refuse HTML, and its absence is itself a spam signal.
func (c Client) SendHTML(to, subject, text, html string) error {
	return c.send(payload{From: c.From, To: []string{to}, Subject: subject, Text: text, HTML: html})
}
