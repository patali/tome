// Package article defines the payload the browser extension sends to the server.
package article

import (
	"regexp"
	"strings"
)

// Article is the cleaned content extracted by Readability in the browser.
type Article struct {
	Title         string `json:"title"`
	Byline        string `json:"byline"`
	PublishedTime string `json:"publishedTime"`
	Content       string `json:"content"` // sanitized HTML from the extractor
	URL           string `json:"url"`
	Device        string `json:"device"` // optional: "scribe" | "scribe3" | "paperwhite"
	Format        string `json:"format"` // optional: "pdf" | "epub"
	Color         string `json:"color"`  // optional: "bw" (default, e-ink) | "color"
	Font          string `json:"font"`   // optional: body face key — see pdfgen.BodyFonts
	CSS           string `json:"css"`    // optional: reader stylesheet from the extension (single source of truth)
}

var (
	unsafeRe = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]`) // illegal in filenames
	wsRe     = regexp.MustCompile(`\s+`)
)

// BaseName returns the article title as a filesystem-safe base name (no
// extension), preserving spaces and capitalization and stripping only
// characters that are illegal in filenames.
func (a Article) BaseName() string {
	name := unsafeRe.ReplaceAllString(a.Title, "")
	name = strings.TrimSpace(wsRe.ReplaceAllString(name, " "))
	if name == "" {
		name = "Article"
	}
	if r := []rune(name); len(r) > 120 { // keep it a sane length (rune-safe)
		name = strings.TrimSpace(string(r[:120]))
	}
	return name
}

// FileName returns the title-based filename with the given extension
// (e.g. ".pdf" or ".epub"); ext should include the leading dot.
func (a Article) FileName(ext string) string {
	return a.BaseName() + ext
}
