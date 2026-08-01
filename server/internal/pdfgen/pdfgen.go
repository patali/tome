// Package pdfgen renders an Article to a device-accurate, e-ink-optimized PDF
// by driving headless Chrome. A real browser fetches remote images and honors
// the reader CSS, so the PDF avoids EPUB's remote-image and entity pitfalls.
package pdfgen

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/patali/tome/server/internal/article"
)

// chromeCandidates are the Chrome-family binaries we try, in order. Override
// with TOME_CHROME.
var chromeCandidates = []string{
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	"/Applications/Arc.app/Contents/MacOS/Arc",
	"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
}

// ChromePath returns the browser binary to use, or "" if none is found.
func ChromePath() string {
	if p := os.Getenv("TOME_CHROME"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, p := range chromeCandidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("chromium"); err == nil {
		return p
	}
	return ""
}

// Available reports whether PDF rendering is possible on this machine.
func Available() bool { return ChromePath() != "" }

type pageGeom struct{ size, margin string }

// Page sizes are the true physical screen dimensions (px / 300 PPI) so the PDF
// renders 1:1 on the device — no magnification, maximum content per page.
var devices = map[string]pageGeom{
	"scribe":     {"157mm 210mm", "10mm 10mm 10mm 10mm"}, // 10.2" 1860x2480
	"scribe3":    {"168mm 224mm", "12mm 12mm 12mm 12mm"}, // 11"   1980x2640
	"paperwhite": {"105mm 140mm", "8mm 8mm 8mm 8mm"},     // 6.8"  1236x1648
}

func metaLine(a article.Article) string {
	var parts []string
	if a.Byline != "" {
		parts = append(parts, html.EscapeString(a.Byline))
	}
	if a.PublishedTime != "" {
		if t, err := time.Parse(time.RFC3339, a.PublishedTime); err == nil {
			parts = append(parts, "Published "+t.Format("Jan 2, 2006"))
		}
	}
	if a.URL != "" {
		parts = append(parts, "Source: "+html.EscapeString(a.URL))
	}
	if len(parts) == 0 {
		return ""
	}
	return `<p class="article-meta">` + strings.Join(parts, " &#183; ") + `</p>`
}

func buildHTML(a article.Article) string {
	dev := devices[a.Device]
	if dev.size == "" {
		dev = devices["scribe"]
	}
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = "Untitled Article"
	}
	page := fmt.Sprintf("@page { size: %s; margin: %s; }", dev.size, dev.margin)

	// Prefer the stylesheet shipped in the payload (extension/reader.css — the
	// single source of truth) so the PDF matches the reader tab exactly; fall
	// back to the embedded copy for bare clients (curl, tests).
	css := strings.TrimSpace(a.CSS)
	if css == "" {
		css = fallbackCSS
	}
	// The stylesheet is injected into a <style> block; make sure it can't
	// terminate the block early.
	css = strings.ReplaceAll(css, "</style", "<\\/style")

	body := "<h1 class=\"article-title\">" + html.EscapeString(title) + "</h1>" +
		metaLine(a) +
		"<article class=\"article-body\">" + a.Content + "</article>"
	return strings.NewReplacer(
		"{{PAGE}}", page,
		"{{FONTS}}", fontCSS(a.Font),
		"{{CSS}}", css,
		"{{TITLE}}", html.EscapeString(title),
		"{{BODY}}", body,
		"{{ROOTATTRS}}", rootAttrs(a),
	).Replace(htmlTemplate)
}

// rootAttrs mirrors onto <html> the dataset attributes the reader page sets,
// because reader.css keys its device and color rules off them. Without this
// the shipped stylesheet's html[data-*] blocks never match and the PDF quietly
// ignores both choices.
func rootAttrs(a article.Article) string {
	device := a.Device
	if devices[device].size == "" {
		device = "scribe"
	}
	color := "bw"
	if a.Color == "color" {
		color = "color"
	}
	font := a.Font
	if _, ok := BodyFonts[font]; !ok {
		font = DefaultBodyFont
	}
	return fmt.Sprintf(` data-device=%q data-color=%q data-font=%q`, device, color, font)
}

// Build renders the article to PDF bytes via headless Chrome.
func Build(a article.Article) (data []byte, filename string, err error) {
	chrome := ChromePath()
	if chrome == "" {
		return nil, "", fmt.Errorf("no Chrome-family browser found (set TOME_CHROME)")
	}

	work, err := os.MkdirTemp("", "tome-pdf-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(work)

	inPath := filepath.Join(work, "reader.html")
	outPath := filepath.Join(work, "out.pdf")
	if err := os.WriteFile(inPath, []byte(buildHTML(a)), 0o644); err != nil {
		return nil, "", err
	}

	args := []string{
		"--headless=new", "--disable-gpu", "--no-pdf-header-footer",
		"--disable-background-networking", "--disable-component-update", "--no-first-run",
		"--user-data-dir=" + filepath.Join(work, "profile"),
	}
	// Extra flags for constrained environments, e.g. containers need
	// "--no-sandbox --disable-dev-shm-usage" (set by the Dockerfile).
	if extra := os.Getenv("TOME_CHROME_FLAGS"); extra != "" {
		args = append(args, strings.Fields(extra)...)
	}
	args = append(args, "--print-to-pdf="+outPath, "file://"+inPath)

	var stderr bytes.Buffer
	cmd := exec.Command(chrome, args...)
	cmd.Stderr = &stderr
	// Chrome needs a writable HOME to set up its crashpad database, and aborts
	// outright ("chrome_crashpad_handler: --database is required", SIGTRAP) if
	// it can't — no PDF, so the poll below just waits out the deadline. That
	// happens whenever HOME is read-only, e.g. a container run with a
	// read_only root filesystem, where su-exec has reset HOME to the service
	// user's home. --user-data-dir doesn't cover it and neither does
	// --crash-dumps-dir; only HOME itself. Point it at the work dir, which is
	// already temporary and cleaned up on return.
	cmd.Env = append(os.Environ(), "HOME="+work)
	// New process group so we can reliably kill Chrome and its children — some
	// builds render the PDF but never exit on macOS.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("start chrome: %w", err)
	}
	pgid := cmd.Process.Pid
	defer func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	}()

	// Poll for the PDF to appear and stop growing, up to a hard cap.
	deadline := time.Now().Add(30 * time.Second)
	var lastSize int64 = -1
	stable := false
	for time.Now().Before(deadline) {
		if fi, statErr := os.Stat(outPath); statErr == nil && fi.Size() > 0 {
			if fi.Size() == lastSize {
				stable = true
				break
			}
			lastSize = fi.Size()
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !stable {
		return nil, "", fmt.Errorf("chrome did not produce a PDF in time: %s", strings.TrimSpace(stderr.String()))
	}

	data, err = os.ReadFile(outPath)
	if err != nil {
		return nil, "", err
	}
	return data, a.FileName(".pdf"), nil
}

// htmlTemplate wraps the article markup; {{CSS}} is the reader stylesheet
// shipped in the payload (or fallbackCSS below).
const htmlTemplate = `<!DOCTYPE html>
<html lang="en"{{ROOTATTRS}}>
<head>
<meta charset="utf-8">
<title>{{TITLE}}</title>
<style>
{{FONTS}}
{{PAGE}}
{{CSS}}
</style>
</head>
<body>
{{BODY}}
</body>
</html>
`

// fallbackCSS is used ONLY when the request carries no stylesheet (bare curl,
// tests). The canonical stylesheet is extension/reader.css — keep this a
// compact approximation, not a second source of truth.
const fallbackCSS = `
:root { --ink:#000; --paper:#fff; --faint:#666; --rule:#ccc; --code-bg:#f0f0f0;
  --serif:"Literata","Source Serif 4",Georgia,serif; --mono:"JetBrains Mono",ui-monospace,monospace; }
* { box-sizing:border-box; }
body { margin:0; color:var(--ink); background:var(--paper); font-family:var(--serif);
  font-size:9.5pt; line-height:1.38; -webkit-font-smoothing:antialiased; }
.article-title { font-size:15pt; font-weight:700; line-height:1.12; margin:0 0 0.2em; }
.article-meta { font-size:0.78em; font-style:italic; color:var(--faint); margin:0 0 0.7em;
  padding-bottom:0.5em; border-bottom:0.5pt solid var(--rule); }
.article-body p { margin:0 0 0.35em; text-align:justify; hyphens:auto; }
.article-body h1 { font-size:13pt; } .article-body h2 { font-size:12pt; font-variant:small-caps; }
.article-body h3 { font-size:10.5pt; }
.article-body h1,.article-body h2,.article-body h3 { line-height:1.18; margin:0.85em 0 0.28em; page-break-after:avoid; font-weight:700; }
.article-body a { color:var(--ink); text-decoration:underline; }
.article-body blockquote { margin:0.6em 0; padding:0 0 0 0.8em; border-left:2pt solid var(--rule); font-style:italic; }
.article-body ul,.article-body ol { margin:0 0 0.35em; padding-left:1.25em; }
hr { border:none; border-top:0.5pt solid var(--rule); margin:0.9em 0; }
.article-body code { font-family:var(--mono); font-size:0.85em; background:var(--code-bg); padding:0.08em 0.3em; }
.article-body pre { font-family:var(--mono); font-size:7.5pt; line-height:1.35; background:var(--code-bg); padding:7px 9px;
  margin:0.6em 0; white-space:pre-wrap; word-break:break-word; overflow-wrap:anywhere; page-break-inside:avoid; }
.article-body pre code { background:none; padding:0; font-size:inherit; }
.article-body img { display:block; max-width:100%; height:auto; margin:0.6em auto; filter:grayscale(1) contrast(1.2);
  border:0.5pt solid var(--rule); page-break-inside:avoid; }
.article-body table { width:100%; border-collapse:collapse; margin:0.6em 0; font-size:0.8em; line-height:1.28; page-break-inside:avoid; }
.article-body th,.article-body td { border:0.5pt solid var(--rule); padding:3px 5px; text-align:left; vertical-align:top; overflow-wrap:anywhere; }
.article-body thead th { background:var(--code-bg); font-weight:700; }
`
