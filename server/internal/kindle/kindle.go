// Package kindle delivers a file to a Kindle, either directly via SMTP
// (Amazon Send-to-Kindle) or, when SMTP isn't configured, by opening macOS
// Mail.app with the file attached so you can review and hit Send.
package kindle

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	mail "github.com/wneessen/go-mail"
)

// DefaultKindleEmail is used when TOME_KINDLE_EMAIL is unset. It's the
// destination for both SMTP and the Mail.app fallback; override via the env var.
const DefaultKindleEmail = "spatali.scribe@kindle.com"

// Config holds SMTP + Send-to-Kindle settings, loaded from the environment.
type Config struct {
	KindleEmail string // your-kindle@kindle.com
	SenderEmail string // must be an Amazon "approved sender"
	SMTPHost    string
	SMTPPort    int
	SMTPUser    string
	SMTPPass    string
}

// LoadConfig reads configuration from TOME_* environment variables.
func LoadConfig() Config {
	port := 587
	if p := os.Getenv("TOME_SMTP_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	host := os.Getenv("TOME_SMTP_HOST")
	if host == "" {
		host = "smtp.gmail.com"
	}
	kindleEmail := os.Getenv("TOME_KINDLE_EMAIL")
	if kindleEmail == "" {
		kindleEmail = DefaultKindleEmail
	}
	return Config{
		KindleEmail: kindleEmail,
		SenderEmail: os.Getenv("TOME_SENDER_EMAIL"),
		SMTPHost:    host,
		SMTPPort:    port,
		SMTPUser:    os.Getenv("TOME_SMTP_USERNAME"),
		SMTPPass:    os.Getenv("TOME_SMTP_PASSWORD"),
	}
}

// SMTPReady reports whether direct SMTP sending is fully configured.
func (c Config) SMTPReady() bool {
	return c.KindleEmail != "" && c.SenderEmail != "" && c.SMTPUser != "" && c.SMTPPass != ""
}

// MissingSMTP names the SMTP fields still unset (for messages).
func (c Config) MissingSMTP() []string {
	var m []string
	if c.SenderEmail == "" {
		m = append(m, "TOME_SENDER_EMAIL")
	}
	if c.SMTPUser == "" {
		m = append(m, "TOME_SMTP_USERNAME")
	}
	if c.SMTPPass == "" {
		m = append(m, "TOME_SMTP_PASSWORD")
	}
	return m
}

// Send emails data as an attachment to the configured Kindle address via SMTP.
func Send(cfg Config, filename string, data []byte, subject string) error {
	if !cfg.SMTPReady() {
		return fmt.Errorf("smtp not configured: set %v", cfg.MissingSMTP())
	}

	msg := mail.NewMsg()
	if err := msg.From(cfg.SenderEmail); err != nil {
		return fmt.Errorf("sender %q: %w", cfg.SenderEmail, err)
	}
	if err := msg.To(cfg.KindleEmail); err != nil {
		return fmt.Errorf("kindle address %q: %w", cfg.KindleEmail, err)
	}
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextPlain, "Delivered by tome.")
	msg.AttachReader(filename, bytes.NewReader(data))

	client, err := mail.NewClient(cfg.SMTPHost,
		mail.WithPort(cfg.SMTPPort),
		mail.WithSMTPAuth(mail.SMTPAuthPlain),
		mail.WithUsername(cfg.SMTPUser),
		mail.WithPassword(cfg.SMTPPass),
	)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	if err := client.DialAndSend(msg); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

// asEscape escapes a string for embedding inside an AppleScript "..." literal.
func asEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// ComposeInMail opens macOS Mail.app with a new message addressed to the Kindle,
// with the file attached, and brings it to the front. The user reviews and sends
// (their Mail account must be an Amazon approved sender). macOS-only.
//
// filePath must remain on disk — Mail attaches by reference.
func ComposeInMail(kindleEmail, subject, filePath string) error {
	if subject == "" {
		subject = "tome"
	}
	script := fmt.Sprintf(`tell application "Mail"
	set newMessage to make new outgoing message with properties {subject:"%s", content:"Delivered by tome.\n", visible:true}
	tell newMessage
		make new to recipient at end of to recipients with properties {address:"%s"}
		make new attachment with properties {file name:(POSIX file "%s")} at after the last paragraph of content
	end tell
	activate
end tell`, asEscape(subject), asEscape(kindleEmail), asEscape(filePath))

	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("open Mail.app: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
