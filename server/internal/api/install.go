package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// publicBaseURL (TOME_BASE_URL) overrides the request-derived value — needed
// when a proxy rewrites the Host header, since we can't recover the original.
var publicBaseURL = strings.TrimRight(os.Getenv("TOME_BASE_URL"), "/")

// baseURL reconstructs the URL clients used to reach us, for self-referencing
// links (install page, invite emails).
//
// Behind a TLS-terminating proxy (Cloudflare, nginx, Tailscale Funnel) the
// connection we see is plain HTTP, so r.TLS is nil even though the user is on
// HTTPS; X-Forwarded-Proto carries the real scheme. It's client-spoofable when
// the server is also reachable directly, which here only affects the scheme in
// displayed links — set TOME_BASE_URL to pin it.
func baseURL(r *http.Request) string {
	if publicBaseURL != "" {
		return publicBaseURL
	}
	scheme := "http"
	if fwd := firstValue(r.Header.Get("X-Forwarded-Proto")); fwd == "https" || fwd == "http" {
		scheme = fwd
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// firstValue takes the leftmost entry of a comma-separated header — chained
// proxies append, so "https, http" means the client arrived over HTTPS.
func firstValue(h string) string {
	if i := strings.Index(h, ","); i >= 0 {
		h = h[:i]
	}
	return strings.ToLower(strings.TrimSpace(h))
}

// extensionVersion reports the version of the extension this server hands out,
// so an installed copy can tell whether it has fallen behind. Returns "" if it
// can't be determined — callers treat that as "no opinion" rather than an
// error, since a missing bundle already shows up at /extension.zip.
//
// Read per call rather than cached: /status is low-traffic, and a stale
// version here would be reported to users as fact.
func (s *Server) extensionVersion() string {
	if s.ExtensionPath == "" {
		return ""
	}
	fi, err := os.Stat(s.ExtensionPath)
	if err != nil {
		return ""
	}
	var raw []byte
	if fi.IsDir() {
		raw, err = os.ReadFile(filepath.Join(s.ExtensionPath, "manifest.json"))
	} else {
		raw, err = manifestFromZip(s.ExtensionPath)
	}
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return m.Version
}

// manifestFromZip pulls manifest.json out of the packaged extension. Both
// producers (the Dockerfile's zip and zipDir here) nest it one level down
// under tome-extension/.
func manifestFromZip(path string) ([]byte, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.Base(f.Name) != "manifest.json" || strings.Count(f.Name, "/") > 1 {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 1<<20))
	}
	return nil, fmt.Errorf("manifest.json not found in %s", path)
}

// handleExtensionZip serves the browser extension for manual installation.
// s.ExtensionPath is either a prebuilt zip (the container image bundles one)
// or the extension source directory (zipped on the fly — the `go run` case).
func (s *Server) handleExtensionZip(w http.ResponseWriter, r *http.Request) {
	if s.ExtensionPath == "" {
		errJSON(w, http.StatusNotFound, "extension bundle not configured (TOME_EXTENSION_PATH)")
		return
	}
	fi, err := os.Stat(s.ExtensionPath)
	if err != nil {
		errJSON(w, http.StatusNotFound, "extension bundle not found at "+s.ExtensionPath)
		return
	}

	var data []byte
	if fi.IsDir() {
		data, err = zipDir(s.ExtensionPath)
	} else {
		data, err = os.ReadFile(s.ExtensionPath)
	}
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="tome-extension.zip"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// zipDir packs dir into a zip whose entries live under "tome-extension/".
func zipDir(dir string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if name == ".DS_Store" || strings.HasPrefix(name, ".") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		entry, err := zw.Create("tome-extension/" + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		_, err = io.Copy(entry, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// handlePrivacy serves PRIVACY.md. Served as text so there is exactly one copy
// of the policy in the repo — a hosted URL is required for a store listing, and
// a second HTML rendering would be a second thing to keep true.
func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	if s.PrivacyPath == "" {
		errJSON(w, http.StatusNotFound, "privacy policy not bundled (TOME_PRIVACY_PATH)")
		return
	}
	data, err := os.ReadFile(s.PrivacyPath)
	if err != nil {
		errJSON(w, http.StatusNotFound, "privacy policy not found at "+s.PrivacyPath)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleInstallPage(w http.ResponseWriter, r *http.Request) {
	set, _ := s.Store.GetSettings()
	data := struct {
		Base       string
		Sender     string
		SenderHTML template.HTML
	}{Base: baseURL(r), Sender: set.ResendFrom, SenderHTML: senderHTML(set.ResendFrom)}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = installTmpl.Execute(w, data)
}

// senderHTML renders the approved-sender address, fenced off from Cloudflare's
// email obfuscation. Cloudflare otherwise rewrites it to "[email protected]"
// and restores it with JavaScript — but this is the one address a reader must
// copy into Amazon by hand, and a silent no-delivery is the cost of getting a
// scrambled one. The email_off markers must arrive as data: html/template
// elides comments written in template source.
func senderHTML(addr string) template.HTML {
	if addr == "" {
		return ""
	}
	return template.HTML("<!--email_off--><code>" +
		template.HTMLEscapeString(addr) + "</code><!--/email_off-->")
}

var installTmpl = template.Must(template.New("install").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Install Tome</title>
<style>
  :root { color-scheme: light dark; }
  body { max-width: 620px; margin: 0 auto; padding: 40px 20px 80px;
    font: 16px/1.6 ui-sans-serif, system-ui, sans-serif; color: #111; background: #fff; }
  @media (prefers-color-scheme: dark) { body { color: #eee; background: #1c1c1e; } }
  h1 { font-size: 26px; } h2 { font-size: 18px; margin-top: 36px; }
  ol li { margin-bottom: 10px; }
  code, kbd { font-family: ui-monospace, monospace; font-size: 0.9em;
    background: rgba(128,128,128,0.15); padding: 2px 6px; border-radius: 5px; }
  a.btn { display: inline-block; padding: 12px 22px; margin: 12px 0; font-weight: 700;
    border-radius: 10px; background: #111; color: #fff; text-decoration: none; }
  @media (prefers-color-scheme: dark) { a.btn { background: #eee; color: #111; } }
  .note { padding: 12px 16px; border-left: 3px solid #888; background: rgba(128,128,128,0.08);
    border-radius: 6px; }
  .warn { border-left-color: #d08700; background: rgba(208,135,0,0.10); }
  .lede { color: #555; }
  @media (prefers-color-scheme: dark) { .lede { color: #aaa; } }
</style>
</head>
<body>
<h1>📖 Install Tome</h1>
<p>Tome turns the article you're reading into a beautifully typeset document on
your Kindle. You'll need an <b>invite code</b> from the person running this
server.</p>

<a class="btn" href="{{.Base}}/extension.zip">⬇ Download the extension</a>

<h2>1 · Install the extension</h2>
<ol>
  <li>Unzip the download — you get a <code>tome-extension</code> folder. Move it
      somewhere permanent (not Downloads — the browser loads it from this
      location from now on).</li>
  <li>Open <code>chrome://extensions</code> (Arc: <code>arc://extensions</code>,
      Edge: <code>edge://extensions</code>).</li>
  <li>Turn on <b>Developer mode</b> (toggle in the top-right corner).</li>
  <li>Click <b>Load unpacked</b> and select the <code>tome-extension</code> folder.</li>
  <li>Pin Tome to your toolbar (puzzle-piece icon → 📌).</li>
</ol>

<h2>2 · Connect your account</h2>
<ol>
  <li>Click the Tome toolbar button, then the <b>⚙ gear</b> to open settings.</li>
  <li>Set the server URL to <code>{{.Base}}</code> and press <b>Save</b>.</li>
  <li>Enter your <b>invite code</b>, your email, and your
      <code>@kindle.com</code> address, then press <b>Redeem invite</b>.
      Step 3 explains where to find that address.</li>
</ol>

<h2>3 · Set up your Kindle email</h2>
<p class="lede">Amazon gives every Kindle its own e-mail address, and only
accepts documents from senders you've approved. Both halves are set on the same
Amazon page — this is the step people miss, and its failure mode is silence.</p>

<p><b>Find your Kindle address.</b> Go to
<a href="https://www.amazon.com/hz/mycd/myx">amazon.com → Manage Your Content
and Devices</a> → <b>Preferences</b> → <b>Personal Document Settings</b>.
Under <b>Send-to-Kindle E-Mail Settings</b> each device is listed with an
address ending in <code>@kindle.com</code>. Copy the one for the device you
actually read on.</p>

<p><b>Approve the sender.</b> On that same page, under <b>Approved Personal
Document E-Mail List</b>, click <b>Add a new approved e-mail address</b> and add
{{if .Sender}}{{.SenderHTML}}{{else}}the address this server sends from
(ask the person who runs it — the admin hasn't configured email delivery
yet){{end}}.</p>

<div class="note warn"><b>Don't skip the approval step.</b> If the sender isn't
on your approved list, Amazon accepts the message and throws it away. Tome
reports the send as successful, and nothing ever arrives on the device.</div>

<p><b>Tell Tome where to send.</b> Your Kindle address is stored on your account
here, not on your device. You set it when you redeem your invite, and you can
change it any time in the extension's <b>⚙ settings → Account → Kindle
address</b> — type the new one and press <b>Save</b>. Do this whenever you
switch Kindles, since the address is per-device.</p>

<h2>4 · Use it</h2>
<p>Open an article, scroll it once so images load, click the Tome button →
<b>Send to Kindle</b>. That's it — it lands on your Kindle in a minute or two.
<b>Open preview</b> gives you a print-ready view (<kbd>⌘P</kbd> → Save as PDF)
if you'd rather keep the file.</p>

<h2>Updating</h2>
<p>To update, download the zip again, replace the folder's contents, and press
the ↻ reload icon on the Tome card in <code>chrome://extensions</code>. Your
sign-in is kept. Tome tells you when this server has a newer version than the
copy you installed.</p>

<h2>Your data</h2>
<p>Articles you send are rendered and delivered, not kept. This server stores
your email, your Kindle address, and a hash of your API key — nothing else.
Full details: <a href="{{.Base}}/privacy">privacy policy</a>.</p>
</body>
</html>
`))
