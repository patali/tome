package pdfgen

import (
	"embed"
	"encoding/base64"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
)

// Fonts are embedded and inlined as data: URIs rather than fetched from Google.
// Chrome renders the PDF from a file:// page, so a network font would mean the
// server reaching out mid-render — slow, dependent on egress, and a third-party
// disclosure of what users read. Inlining also removes any question of relative
// paths resolving from the temp directory.
//
// These are a copy of extension/fonts (Go can't embed across module
// boundaries). Both families are SIL OFL 1.1 — see fonts/OFL-*.txt.
//
//go:embed fonts/*.woff2
var fontFS embed.FS

// Subset coverage, copied from the Google CSS that produced these files (the
// values are identical for both families). These are load-bearing, not
// decoration: two faces sharing a family/style/weight with no unicode-range
// are the *same* face, so the later one silently replaces the earlier, and
// text outside its subset renders with no glyphs.
const (
	rangeLatin = "U+0000-00FF, U+0131, U+0152-0153, U+02BB-02BC, U+02C6, U+02DA, U+02DC, " +
		"U+0304, U+0308, U+0329, U+2000-206F, U+20AC, U+2122, U+2191, U+2193, U+2212, " +
		"U+2215, U+FEFF, U+FFFD"
	rangeLatinExt = "U+0100-02BA, U+02BD-02C5, U+02C7-02CC, U+02CE-02D7, U+02DD-02FF, " +
		"U+0304, U+0308, U+0329, U+1D00-1DBF, U+1E00-1E9F, U+1EF2-1EFF, U+2020, " +
		"U+20A0-20AB, U+20AD-20C0, U+2113, U+2C60-2C7F, U+A720-A7FF"
)

// Matches extension/fonts.css: latin + latin-ext of each family/style. The
// files are variable fonts, so one covers every weight the stylesheet asks for.
var fontFaces = []struct {
	family, style, file, unicodeRange string
}{
	{"Literata", "normal", "Literata-normal-latin.woff2", rangeLatin},
	{"Literata", "normal", "Literata-normal-latin-ext.woff2", rangeLatinExt},
	{"Literata", "italic", "Literata-italic-latin.woff2", rangeLatin},
	{"Literata", "italic", "Literata-italic-latin-ext.woff2", rangeLatinExt},
	{"JetBrains Mono", "normal", "JetBrainsMono-normal-latin.woff2", rangeLatin},
	{"JetBrains Mono", "normal", "JetBrainsMono-normal-latin-ext.woff2", rangeLatinExt},
}

var (
	fontCSSOnce sync.Once
	fontCSSVal  string
)

// fontCSS returns @font-face rules with the woff2 payloads inlined. Built once:
// base64 of ~285 KB of fonts is wasted work on every render otherwise.
func fontCSS() string {
	fontCSSOnce.Do(func() {
		var b strings.Builder
		for _, f := range fontFaces {
			data, err := fontFS.ReadFile(path.Join("fonts", f.file))
			if err != nil {
				continue // a missing face degrades to the fallback stack, not a failed render
			}
			// font-display:swap, not block: block hides text until the face
			// resolves, and Chrome prints on its own schedule — a blocked face
			// yields a laid-out PDF with no glyphs at all. Swap can never do
			// that. The payload is inline, so there's no real swap window.
			fmt.Fprintf(&b, "@font-face{font-family:'%s';font-style:%s;"+
				"font-weight:100 900;font-display:swap;unicode-range:%s;"+
				"src:url(data:font/woff2;base64,%s) format('woff2');}\n",
				f.family, f.style, f.unicodeRange, base64.StdEncoding.EncodeToString(data))
		}
		fontCSSVal = b.String()
	})
	return fontCSSVal
}

// embeddedFontNames lists what actually made it into the binary — used by the
// tests to catch a rename that would silently drop a face.
func embeddedFontNames() []string {
	entries, err := fontFS.ReadDir("fonts")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}
