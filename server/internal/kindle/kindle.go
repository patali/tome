// Package kindle opens macOS Mail.app with a document attached, addressed to
// a Kindle. This is the admin-only fallback when Resend isn't configured;
// normal delivery is the Resend API (internal/resend).
package kindle

import (
	"fmt"
	"os/exec"
	"strings"
)

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
