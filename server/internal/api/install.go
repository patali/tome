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

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
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

// privacyMD renders the policy. GFM tables are enabled because the policy's
// two inventories (what the extension stores, what each permission is for) are
// written as tables; raw HTML stays disabled, which costs nothing since the
// source is ours and markdown-only.
var privacyMD = goldmark.New(goldmark.WithExtensions(extension.Table))

// handlePrivacy renders PRIVACY.md.
//
// The policy is rendered rather than restated: PRIVACY.md remains the single
// copy in the repo, which is what makes the hosted page and the one GitHub
// shows provably the same document. Restating it in a template to get the
// styling would create a second thing to keep true, and a privacy policy that
// has quietly drifted from its canonical copy is worse than an ugly one.
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

	var body bytes.Buffer
	if err := privacyMD.Convert(data, &body); err != nil {
		// Fall back to the raw markdown rather than an error page: an
		// unstyled policy still discharges the obligation to publish one.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(data)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = privacyTmpl.Execute(w, struct {
		Base string
		Body template.HTML
	}{Base: baseURL(r), Body: template.HTML(body.String())})
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
<meta name="description" content="Get set up with Tome in four unhurried steps.">
<link rel="icon" type="image/png" href="/icon.png">
<link rel="apple-touch-icon" href="/icon.png">
<meta name="theme-color" content="#fbf6ec">
<style>
  @font-face {
    font-family: 'Literata';
    src: url('/fonts/Literata-normal-latin.woff2') format('woff2');
    font-weight: 400 900; font-style: normal; font-display: swap;
  }
  @font-face {
    font-family: 'Literata';
    src: url('/fonts/Literata-var-italic-latin.woff2') format('woff2');
    font-weight: 400 700; font-style: italic; font-display: swap;
  }

  :root {
    color-scheme: light;
    --paper:   #fbf6ec;
    --tile:    #f3ebda;
    --ink:     #211d17;
    --body:    #3a352b;
    --soft:    #4a453d;
    --eyebrow: #9a7a55;
    --accent:  #bb4a1f;
    --rule:    #ece3d2;
    --muted:   #6b6357;
    --numeral: #c9a24a;
  }

  * { box-sizing: border-box; }
  html { -webkit-text-size-adjust: 100%; }
  body {
    margin: 0; background: var(--paper); color: var(--ink);
    font: 400 17px/1.6 Literata, Georgia, 'Times New Roman', serif;
    -webkit-font-smoothing: antialiased;
  }
  .wrap { max-width: 820px; margin: 0 auto; padding: 0 40px; }
  @media (max-width: 560px) { .wrap { padding: 0 22px; } }
  a { color: var(--accent); text-decoration: none; }
  a:hover { color: #9a3c17; text-decoration: underline; }

  /* Same mark construction as the landing page, so header, tab and hero
     tile stay provably one object rather than three lookalikes. */
  .mark { position: relative; display: inline-flex; flex-shrink: 0;
    background: var(--ink); box-sizing: border-box;
    width: 30px; height: 38px; border-radius: 6px; padding: 3px; }
  .mark > .screen { position: relative; flex: 1; background: var(--tile);
    border-radius: 3px; display: flex; align-items: center;
    justify-content: center; overflow: hidden; }
  .mark .ribbon { position: absolute; top: -1px; right: 5px;
    width: 5px; height: 13px; background: var(--accent);
    clip-path: polygon(0 0, 100% 0, 100% 100%, 50% 74%, 0 100%); }
  .mark .t { font-family: Literata, serif; font-weight: 600; font-style: italic;
    color: var(--ink); line-height: 1; font-size: 22px; margin-top: 2px; }

  header.site { display: flex; align-items: center; justify-content: space-between;
    gap: 20px; padding: 28px 0; }
  .lockup { display: flex; align-items: center; gap: 12px; text-decoration: none; }
  .lockup:hover { text-decoration: none; }
  .lockup .word { font-weight: 500; font-style: italic; font-size: 24px; color: var(--ink); }
  nav.site { display: flex; gap: 26px; font-size: 15px; }
  nav.site a { color: var(--soft); }
  nav.site a:hover { color: var(--ink); text-decoration: none; }
  .rule { border-top: 1px solid var(--rule); }

  .hero { padding: 72px 0 56px; }
  .eyebrow { font-size: 13px; letter-spacing: 0.42em; text-transform: uppercase;
    color: var(--eyebrow); margin-bottom: 22px; }
  h1 { font-weight: 900; font-size: clamp(36px, 7vw, 60px); line-height: 1.04;
    letter-spacing: -0.018em; margin: 0; max-width: 680px; text-wrap: pretty; }
  .lede { font-size: 20px; line-height: 1.6; color: var(--soft); max-width: 600px;
    margin: 26px 0 0; text-wrap: pretty; }
  .download { display: inline-flex; align-items: center; gap: 10px;
    background: var(--accent); color: var(--paper); padding: 15px 28px;
    border-radius: 9px; font-size: 17px; margin-top: 34px; }
  .download:hover { background: #a54019; color: var(--paper); text-decoration: none; }

  /* ── steps ────────────────────────────────────────────────────────── */
  .step { display: flex; gap: 34px; padding: 52px 0; border-bottom: 1px solid var(--rule); }
  .step .num { flex-shrink: 0; width: 56px; text-align: right;
    font-weight: 300; font-size: 46px; line-height: 1; color: var(--numeral);
    font-style: italic; }
  .step .body { flex: 1; min-width: 0; }
  .step h2 { font-weight: 700; font-size: 28px; line-height: 1.15;
    letter-spacing: -0.01em; margin: 2px 0 0; }
  .step .prose { margin-top: 22px; font-size: 18px; line-height: 1.62; color: var(--body); }
  @media (max-width: 560px) {
    .step { gap: 18px; padding: 38px 0; }
    .step .num { width: 34px; font-size: 32px; }
    .step h2 { font-size: 23px; }
    .step .prose { font-size: 17px; }
  }

  /* Numbered sub-steps as inline counters rather than list markers, so the
     numeral matches the face the rest of the page is set in. */
  .prose ol { margin: 0; padding: 0; list-style: none; counter-reset: s;
    display: flex; flex-direction: column; gap: 16px; }
  .prose ol > li { position: relative; padding-left: 44px; counter-increment: s; }
  .prose ol > li::before {
    content: counter(s); position: absolute; left: 0; top: 1px;
    width: 26px; height: 26px; border-radius: 50%;
    background: #efe6d3; color: #9a3c17;
    font-family: Literata, serif; font-weight: 600; font-size: 14px; font-style: italic;
    display: flex; align-items: center; justify-content: center;
  }
  .prose p { margin: 0 0 16px; }
  .prose p:last-child { margin-bottom: 0; }
  .prose strong { font-weight: 600; color: var(--ink); }
  .prose code, .prose kbd {
    font-family: ui-monospace, 'SFMono-Regular', Menlo, monospace;
    font-size: 0.82em; background: #f0e7d5; color: #7a3414;
    padding: 2px 7px; border-radius: 5px; word-break: break-word;
  }
  .note { color: var(--muted); font-size: 16px; }

  .callout { margin-top: 48px; background: var(--tile);
    border-left: 4px solid var(--accent); border-radius: 4px; padding: 28px 32px; }
  .callout .label { font-size: 13px; letter-spacing: 0.22em; text-transform: uppercase;
    color: var(--accent); font-weight: 600; margin-bottom: 12px; }
  .callout p { margin: 0; font-size: 18px; line-height: 1.62; color: var(--body);
    text-wrap: pretty; }

  footer.site { border-top: 1px solid var(--rule); background: #f6f0e3; margin-top: 96px; }
  footer.site .inner { max-width: 820px; margin: 0 auto; padding: 44px 40px;
    display: flex; justify-content: space-between; align-items: flex-start;
    gap: 40px; flex-wrap: wrap; }
  @media (max-width: 560px) { footer.site .inner { padding: 36px 22px; } }
  footer.site .h { font-weight: 600; font-size: 16px; letter-spacing: 0.04em;
    margin-bottom: 12px; }
  footer.site p { margin: 0; font-size: 15px; line-height: 1.6; color: var(--muted);
    max-width: 440px; }
  footer.site .sig { font-size: 15px; color: #8a8175; font-style: italic; }
</style>
</head>
<body>

<div class="wrap">
  <header class="site">
    <a class="lockup" href="{{.Base}}/">
      <span class="mark"><span class="screen">
        <span class="ribbon"></span><span class="t">T</span>
      </span></span>
      <span class="word">Tome</span>
    </a>
    <nav class="site">
      <a href="{{.Base}}/#what">What it does</a>
      <a href="{{.Base}}/privacy">Privacy</a>
    </nav>
  </header>
</div>
<div class="rule"></div>

<div class="wrap">
  <div class="hero">
    <div class="eyebrow">Install Tome</div>
    <h1>Get set up in four unhurried steps.</h1>
    <p class="lede">Tome turns the article you're reading into a beautifully
    typeset document on your Kindle. You'll need an <strong>invite code</strong>
    from the person running this server.</p>
    <a class="download" href="{{.Base}}/extension.zip"><span>↓</span> Download the extension</a>
  </div>

  <div class="rule"></div>

  <main>
    <div class="step">
      <div class="num">1</div>
      <div class="body">
        <h2>Install the extension</h2>
        <div class="prose">
          <ol>
            <li>Unzip the download — you get a <code>tome-extension</code> folder.
                Move it somewhere permanent (not Downloads — the browser loads it
                from this location from now on).</li>
            <li>Open <code>chrome://extensions</code> (Arc: <code>arc://extensions</code>,
                Edge: <code>edge://extensions</code>).</li>
            <li>Turn on <strong>Developer mode</strong> (toggle in the top-right corner).</li>
            <li>Click <strong>Load unpacked</strong> and select the
                <code>tome-extension</code> folder.</li>
            <li>Pin Tome to your toolbar (puzzle-piece icon → 📌).</li>
          </ol>
        </div>
      </div>
    </div>

    <div class="step">
      <div class="num">2</div>
      <div class="body">
        <h2>Connect your account</h2>
        <div class="prose">
          <ol>
            <li>Click the Tome toolbar button, then the <strong>gear</strong> to open settings.</li>
            <li>Set the server URL to <code>{{.Base}}</code> and press <strong>Save</strong>.</li>
            <li>Enter your <strong>invite code</strong>, your email, and your
                <code>@kindle.com</code> address, then press <strong>Redeem invite</strong>.
                Step 3 explains where to find that address.</li>
          </ol>
        </div>
      </div>
    </div>

    <div class="step">
      <div class="num">3</div>
      <div class="body">
        <h2>Set up your Kindle email</h2>
        <div class="prose">
          <p>Amazon gives every Kindle its own e-mail address, and only accepts
          documents from senders you've approved. Both halves are set on the same
          Amazon page — this is the step people miss, and its failure mode is silence.</p>

          <p><strong>Find your Kindle address.</strong> Go to
          <a href="https://www.amazon.com/hz/mycd/myx">Manage Your Content and
          Devices</a> → <strong>Preferences</strong> →
          <strong>Personal Document Settings</strong>. Under
          <strong>Send-to-Kindle E-Mail Settings</strong> each device is listed with
          an address ending in <code>@kindle.com</code>. Copy the one for the device
          you actually read on.</p>

          <p><strong>Approve the sender.</strong> On that same page, under
          <strong>Approved Personal Document E-Mail List</strong>, click
          <strong>Add a new approved e-mail address</strong> and add
          {{if .Sender}}{{.SenderHTML}}{{else}}the address this server sends from
          (ask the person who runs it — the admin hasn't configured email delivery
          yet){{end}}.</p>

          <p><strong>Tell Tome where to send.</strong> Your Kindle address is stored
          on your account here, not on your device. You set it when you redeem your
          invite, and can change it any time in the extension's
          <strong>settings → Account → Kindle address</strong>. Do this whenever you
          switch Kindles, since the address is per-device.</p>
        </div>
      </div>
    </div>

    <div class="step">
      <div class="num">4</div>
      <div class="body">
        <h2>Use it</h2>
        <div class="prose">
          <p>Open an article, scroll it once so images load, click the Tome button →
          <strong>Send to Kindle</strong>. That's it — it lands on your Kindle in a
          minute or two. <strong>Open preview</strong> gives you a print-ready view
          (<kbd>⌘P</kbd> → Save as PDF) if you'd rather keep the file.</p>
          <p class="note"><strong>Updating:</strong> download the zip again, replace
          the folder's contents, and press the reload icon on the Tome card in
          <code>chrome://extensions</code>. Your sign-in is kept. Tome tells you when
          this server has a newer version than the copy you installed.</p>
        </div>
      </div>
    </div>

    <div class="callout">
      <div class="label">Don't skip the approval step</div>
      <p>If the sender isn't on your approved list, Amazon accepts the message and
      throws it away. Tome reports the send as successful, and nothing ever arrives
      on the device.</p>
    </div>
  </main>
</div>

<footer class="site">
  <div class="inner">
    <div>
      <div class="h">Your data</div>
      <p>Articles you send are rendered and delivered, not kept. This server stores
      your email, your Kindle address, and a hash of your API key — nothing else.
      Full details: <a href="{{.Base}}/privacy">privacy policy</a>.</p>
    </div>
    <div class="sig">Tome — read it properly.</div>
  </div>
</footer>

</body>
</html>
`))

var privacyTmpl = template.Must(template.New("privacy").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Privacy policy — Tome</title>
<link rel="icon" type="image/png" href="/icon.png">
<link rel="apple-touch-icon" href="/icon.png">
<meta name="theme-color" content="#fbf6ec">
<style>
  @font-face {
    font-family: 'Literata';
    src: url('/fonts/Literata-normal-latin.woff2') format('woff2');
    font-weight: 400 900; font-style: normal; font-display: swap;
  }
  @font-face {
    font-family: 'Literata';
    src: url('/fonts/Literata-var-italic-latin.woff2') format('woff2');
    font-weight: 400 700; font-style: italic; font-display: swap;
  }

  :root {
    color-scheme: light;
    --paper: #fbf6ec; --tile: #f3ebda; --ink: #2a2620; --body: #3a352b;
    --soft: #5a5348; --muted: #6b6357; --faint: #8a8175;
    --accent: #bb4a1f; --rule: #ece3d2; --hair: #efe7d6; --mono: #7a3414;
  }
  * { box-sizing: border-box; }
  html { -webkit-text-size-adjust: 100%; }
  body { margin: 0; background: var(--paper); color: var(--ink);
    font: 400 17px/1.7 Literata, Georgia, 'Times New Roman', serif;
    -webkit-font-smoothing: antialiased; }
  .wrap { max-width: 680px; margin: 0 auto; padding: 0 40px; }
  @media (max-width: 560px) { .wrap { padding: 0 22px; } }
  a { color: var(--accent); text-decoration: none; }
  a:hover { color: #9a3c17; text-decoration: underline; }

  .mark { position: relative; display: inline-flex; flex-shrink: 0;
    background: #211d17; box-sizing: border-box;
    width: 26px; height: 33px; border-radius: 5px; padding: 3px; }
  .mark > .screen { position: relative; flex: 1; background: var(--tile);
    border-radius: 2px; display: flex; align-items: center;
    justify-content: center; overflow: hidden; }
  .mark .ribbon { position: absolute; top: -1px; right: 4px; width: 4px; height: 11px;
    background: var(--accent);
    clip-path: polygon(0 0, 100% 0, 100% 100%, 50% 74%, 0 100%); }
  .mark .t { font-family: Literata, serif; font-weight: 600; font-style: italic;
    color: #211d17; line-height: 1; font-size: 19px; margin-top: 2px; }

  header.site { display: flex; align-items: center; justify-content: space-between;
    gap: 20px; padding: 24px 0; border-bottom: 1px solid var(--rule); }
  .lockup { display: flex; align-items: center; gap: 11px; }
  .lockup:hover { text-decoration: none; }
  .lockup .word { font-weight: 500; font-size: 21px; font-style: italic; color: var(--ink); }
  nav.site { display: flex; gap: 24px; font-size: 14px; }
  nav.site a { color: var(--muted); }
  nav.site a:hover { color: var(--ink); text-decoration: none; }

  main { padding: 56px 0 40px; }

  /* ── rendered markdown ────────────────────────────────────────────────
     Everything below styles goldmark's output. The policy itself lives in
     PRIVACY.md and is rendered, never restated here. */
  .md h1 { font-weight: 700; font-size: clamp(28px, 5vw, 34px); line-height: 1.15;
    letter-spacing: -0.01em; margin: 0; }
  /* The "Last updated" line is the one emphasis directly after the title. */
  .md h1 + p em { font-style: normal; font-size: 14px; color: var(--faint); }
  .md h1 + p { margin: 10px 0 0; }
  .md h2 { font-weight: 600; font-size: 20px; margin: 52px 0 6px;
    padding-top: 26px; border-top: 1px solid var(--rule); }
  .md p { font-size: 17px; line-height: 1.7; color: var(--body); margin: 18px 0 0; }
  .md strong { font-weight: 600; color: var(--ink); }
  .md code { font-family: ui-monospace, Menlo, monospace; font-size: 0.85em;
    color: var(--mono); background: #f0e7d5; padding: 1px 6px; border-radius: 4px; }
  .md ul { margin: 14px 0 0; padding-left: 22px; }
  .md li { font-size: 16px; line-height: 1.7; color: var(--body); margin: 0 0 8px; }

  /* Tables carry the two inventories. Rendered as rows rather than a grid so
     a long "why" column wraps under its label instead of squeezing it. */
  .md table { width: 100%; border-collapse: collapse; margin: 20px 0 0; }
  .md thead { display: none; }
  .md tr { display: block; padding: 16px 0; border-bottom: 1px solid var(--hair); }
  .md td { display: block; padding: 0; }
  .md td:first-child { font-weight: 600; font-size: 16px; color: var(--ink); }
  .md td:last-child { font-size: 15px; line-height: 1.6; color: var(--muted); margin-top: 5px; }

  /* The 3-column inventory puts its storage area on the same line as the
     name, right-aligned and monospaced, the way the spec reads it. */
  .md table:has(td:nth-child(3)) tr {
    display: grid; grid-template-columns: 1fr auto; column-gap: 16px; align-items: baseline; }
  .md table:has(td:nth-child(3)) td:nth-child(2) {
    font-family: ui-monospace, Menlo, monospace; font-size: 12px;
    color: #8a7a5f; white-space: nowrap; text-align: right; }
  .md table:has(td:nth-child(3)) td:nth-child(2) code { background: none; padding: 0; color: inherit; }
  .md table:has(td:nth-child(3)) td:nth-child(3) { grid-column: 1 / -1; }

  /* The 2-column one is a definition list: term left, reason right. */
  .md table:not(:has(td:nth-child(3))) tr {
    display: grid; grid-template-columns: 180px 1fr; gap: 20px; padding: 12px 0;
    align-items: baseline; }
  .md table:not(:has(td:nth-child(3))) td:first-child {
    font-family: ui-monospace, Menlo, monospace; font-size: 13px; color: var(--mono);
    font-weight: 400; }
  .md table:not(:has(td:nth-child(3))) td:first-child code { background: none; padding: 0; color: inherit; }
  .md table:not(:has(td:nth-child(3))) td:last-child {
    font-size: 15.5px; color: var(--body); margin-top: 0; }
  @media (max-width: 560px) {
    .md table:not(:has(td:nth-child(3))) tr { grid-template-columns: 1fr; gap: 4px; }
  }

  footer.site { border-top: 1px solid var(--rule); }
  footer.site .inner { max-width: 680px; margin: 0 auto; padding: 24px 40px;
    font-size: 14px; color: var(--faint); font-style: italic; }
  @media (max-width: 560px) { footer.site .inner { padding: 24px 22px; } }
</style>
</head>
<body>

<div class="wrap">
  <header class="site">
    <a class="lockup" href="{{.Base}}/">
      <span class="mark"><span class="screen">
        <span class="ribbon"></span><span class="t">T</span>
      </span></span>
      <span class="word">Tome</span>
    </a>
    <nav class="site">
      <a href="{{.Base}}/install">Install</a>
      <a href="{{.Base}}/#what">What it does</a>
    </nav>
  </header>

  <main class="md">{{.Body}}</main>
</div>

<footer class="site">
  <div class="inner">Tome — read it properly.</div>
</footer>

</body>
</html>
`))
