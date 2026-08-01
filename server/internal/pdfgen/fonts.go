package pdfgen

import (
	"embed"
	"encoding/base64"
	"path"
	"regexp"
	"strings"
	"sync"
)

// Fonts are embedded and inlined as data: URIs rather than fetched from Google.
// Chrome renders the PDF from a file:// page, so a network font would mean the
// server reaching out mid-render — slow, dependent on egress, and a third-party
// disclosure of what users read.
//
// fonts.css is a verbatim copy of extension/fonts.css and is the single source
// of truth for the @font-face rules. Deriving from it rather than restating the
// faces in Go keeps the PDF's typography identical to the preview's, and avoids
// getting weights wrong for the families that ship as static per-weight files
// rather than variable ones.
//
// Every family is SIL OFL 1.1 — see fonts/OFL-*.txt.
//
//go:embed fonts/fonts.css fonts/*.woff2
var fontFS embed.FS

// BodyFonts are the body faces a client may choose, keyed by the value sent as
// the article's "font" field and mirrored onto <html data-font>. The CSS family
// name must match the @font-face rules in fonts.css.
var BodyFonts = map[string]string{
	"literata":     "Literata",
	"sourceserif":  "Source Serif 4",
	"merriweather": "Merriweather",
	"baskerville":  "Libre Baskerville",
	"inter":        "Inter",
	"atkinson":     "Atkinson Hyperlegible",
}

// DefaultBodyFont is what an unset or unrecognised choice falls back to.
const DefaultBodyFont = "literata"

// monoFamily is always inlined: code blocks use it whatever the body face is.
const monoFamily = "JetBrains Mono"

var (
	faceBlockRe = regexp.MustCompile(`(?s)@font-face\s*\{.*?\}`)
	familyRe    = regexp.MustCompile(`font-family:\s*'([^']+)'`)
	fontURLRe   = regexp.MustCompile(`url\(fonts/([A-Za-z0-9._-]+\.woff2)\)`)
)

var (
	fontCSSMu    sync.Mutex
	fontCSSCache = map[string]string{}
)

// fontCSS returns @font-face rules for one body family plus the mono face, with
// the woff2 payloads inlined. Only the requested family is included: the bundle
// is about a megabyte across seven families, and inlining all of it into every
// render would be wasted base64 for fonts the page never names.
func fontCSS(choice string) string {
	family, ok := BodyFonts[choice]
	if !ok {
		family = BodyFonts[DefaultBodyFont]
	}

	fontCSSMu.Lock()
	defer fontCSSMu.Unlock()
	if css, hit := fontCSSCache[family]; hit {
		return css
	}

	raw, err := fontFS.ReadFile("fonts/fonts.css")
	if err != nil {
		return "" // fall back to the stylesheet's system stack
	}
	var b strings.Builder
	for _, block := range faceBlockRe.FindAllString(string(raw), -1) {
		m := familyRe.FindStringSubmatch(block)
		if m == nil || (m[1] != family && m[1] != monoFamily) {
			continue
		}
		inlined := fontURLRe.ReplaceAllStringFunc(block, func(match string) string {
			name := fontURLRe.FindStringSubmatch(match)[1]
			data, err := fontFS.ReadFile(path.Join("fonts", name))
			if err != nil {
				return match // leaves a dead relative URL; the face just doesn't load
			}
			return "url(data:font/woff2;base64," + base64.StdEncoding.EncodeToString(data) + ")"
		})
		// block, not swap, would hide text until the face resolves — and Chrome
		// prints on its own schedule, which yields a laid-out PDF with no glyphs.
		inlined = strings.ReplaceAll(inlined, "font-display: block", "font-display: swap")
		b.WriteString(inlined)
		b.WriteString("\n")
	}
	fontCSSCache[family] = b.String()
	return b.String()
}

// embeddedFontNames lists the woff2 files compiled in — used by tests to catch
// a rename that would silently drop a face.
func embeddedFontNames() []string {
	entries, err := fontFS.ReadDir("fonts")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".woff2") {
			out = append(out, e.Name())
		}
	}
	return out
}
