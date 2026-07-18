package api

import (
	"archive/zip"
	"bytes"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// baseURL reconstructs the URL clients used to reach us, for self-referencing
// links (install page, invite emails).
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
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

func (s *Server) handleInstallPage(w http.ResponseWriter, r *http.Request) {
	set, _ := s.Store.GetSettings()
	data := struct {
		Base   string
		Sender string
	}{baseURL(r), set.ResendFrom}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = installTmpl.Execute(w, data)
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
      (Find your Kindle address at amazon.com → Manage Your Content and Devices
      → Preferences → Personal Document Settings.)</li>
</ol>

{{if .Sender}}<div class="note"><b>Required:</b> on that same Amazon
Personal&nbsp;Document&nbsp;Settings page, add <code>{{.Sender}}</code> to your
<b>Approved Personal Document E-mail List</b> — otherwise Amazon silently
rejects the deliveries.</div>{{end}}

<h2>3 · Use it</h2>
<p>Open an article on X, scroll it once so images load, click the Tome button →
<b>Send to Kindle</b>. That's it — it lands on your Kindle in a minute or two.
<b>Open preview</b> gives you a print-ready view (<kbd>⌘P</kbd> → Save as PDF)
if you'd rather keep the file.</p>

<h2>Optional: “Send via email” (macOS)</h2>
<p>If you prefer reviewing in Mail.app before sending: in the unzipped folder,
run <code>native-host/install.sh</code> in Terminal, restart the browser, and
enable the button in Tome's settings. It opens a Mail compose window with the
document attached.</p>

<h2>Updating</h2>
<p>To update, download the zip again, replace the folder's contents, and press
the ↻ reload icon on the Tome card in <code>chrome://extensions</code>. Your
sign-in is kept.</p>
</body>
</html>
`))
