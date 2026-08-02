package api

import (
	"embed"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/patali/tome/server/internal/pdfgen"
)

// inviteURL (TOME_INVITE_URL) is where the landing page's "Get an invite"
// button points when no notify address is configured — a mailto:, a DM link,
// whatever the operator prefers. With TOME_INVITE_NOTIFY set the on-page form
// takes precedence and this is unused.
var inviteURL = strings.TrimSpace(os.Getenv("TOME_INVITE_URL"))

//go:embed assets/icon-512.png
var assetFS embed.FS

// landingFonts are the faces served over HTTP for the landing page.
//
// The roman file is the same one pdfgen embeds — it turns out to be the
// variable Literata covering 400..900, and fonts.css simply declares it as 400
// because that is all a document needs. The page declares the full range over
// the identical bytes, so the 900 headline is a real weight rather than a
// synthesised one, at no extra download.
//
// The italic is the exception: pdfgen's is a static 400, and the wordmark and
// the T want 500..600. That one file is carried for the page alone; nothing in
// fonts.css references it, so PDF rendering is untouched.
var landingFonts = map[string]bool{
	"Literata-normal-latin.woff2":     true,
	"Literata-var-italic-latin.woff2": true,
}

// handleFont serves an embedded font face used by the landing page. The
// allowlist keeps this from becoming a general font CDN.
func (s *Server) handleFont(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !landingFonts[name] {
		http.NotFound(w, r)
		return
	}
	b, ok := pdfgen.FontFile(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	// Immutable for a given binary; the filename changes when the face does.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// handleIcon serves the app icon, used as the favicon and apple-touch-icon.
func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request) {
	b, err := assetFS.ReadFile("assets/icon-512.png")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func (s *Server) handleLandingPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = landingTmpl.Execute(w, struct {
		Base          string
		InviteURL     string
		InviteForm    bool
		TurnstileSite string
	}{
		Base:          baseURL(r),
		InviteURL:     inviteURL,
		InviteForm:    inviteFormEnabled(),
		TurnstileSite: turnstileSite,
	})
}

// Subresources (fonts, icon) are referenced root-relative rather than through
// {{.Base}}: a Base pointing at another origin would make the fonts a
// cross-origin request and get them blocked for want of CORS. User-visible
// links keep using Base, which is what a proxy needs rewritten.
var landingTmpl = template.Must(template.New("landing").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Tome — read it properly</title>
<meta name="description" content="Tome typesets whatever you're reading and drops it on your Kindle — one click.">
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

  /* The art direction is paper. Committing to light keeps the ink, the tile
     and the one accent in the relationship they were drawn in; a dark
     inversion would need its own design, not a filter. */
  :root {
    color-scheme: light;
    --paper:   #fbf6ec;
    --tile:    #f3ebda;
    --ink:     #211d17;
    --body:    #4a453d;
    --eyebrow: #9a7a55;
    --accent:  #bb4a1f;
    --rule:    #ece3d2;
    --line:    #cabd9f;
    --muted:   #6b6357;
  }

  * { box-sizing: border-box; }
  html { -webkit-text-size-adjust: 100%; }
  body {
    margin: 0;
    background: var(--paper); color: var(--ink);
    font: 400 17px/1.6 Literata, Georgia, 'Times New Roman', serif;
    -webkit-font-smoothing: antialiased;
  }
  .wrap { max-width: 1010px; margin: 0 auto; padding: 0 28px; }
  a { color: var(--accent); }

  /* ── the mark ─────────────────────────────────────────────────────────
     A Kindle seen head-on: ink body, e-ink screen, the ribbon as a
     bookmark tucked over the top edge. Same construction at every size,
     only the numbers change, so the tab, the header and the hero tile are
     provably the same object rather than three lookalikes. */
  .mark { position: relative; display: inline-flex; flex-shrink: 0;
    background: var(--ink); box-sizing: border-box; }
  .mark > .screen { position: relative; flex: 1; background: var(--tile);
    display: flex; align-items: center; justify-content: center; overflow: hidden; }
  .mark .ribbon { position: absolute; background: var(--accent);
    clip-path: polygon(0 0, 100% 0, 100% 100%, 50% 74%, 0 100%); }
  .mark .t { font-family: Literata, serif; font-weight: 600; font-style: italic;
    color: var(--ink); line-height: 1; }

  .mark-sm { width: 30px; height: 38px; border-radius: 6px; padding: 3px; }
  .mark-sm > .screen { border-radius: 3px; }
  .mark-sm .ribbon { top: -1px; right: 5px; width: 5px; height: 13px; }
  .mark-sm .t { font-size: 22px; margin-top: 2px; }

  /* ── header ───────────────────────────────────────────────────────── */
  header.site { display: flex; align-items: center; justify-content: space-between;
    gap: 20px; padding: 22px 0; border-bottom: 1px solid var(--rule); }
  .lockup { display: flex; align-items: center; gap: 12px; text-decoration: none; }
  .lockup .word { font-weight: 500; font-style: italic; font-size: 24px;
    color: var(--ink); }
  nav.site { display: flex; gap: 26px; font-size: 15px; color: var(--body); }
  nav.site a { color: var(--body); text-decoration: none; }
  nav.site a:hover { color: var(--ink); }
  nav.site a.accent { color: var(--accent); }
  @media (max-width: 620px) { nav.site .hide-sm { display: none; } }

  /* ── hero ─────────────────────────────────────────────────────────── */
  .hero { display: flex; align-items: center; gap: 52px; padding: 60px 0 68px; }
  .hero-copy { flex: 1; min-width: 0; }
  .eyebrow { font-size: 13px; letter-spacing: 0.42em; text-transform: uppercase;
    color: var(--eyebrow); margin-bottom: 20px; }
  h1 { font-weight: 900; font-size: clamp(34px, 6vw, 52px); line-height: 1.06;
    letter-spacing: -0.015em; margin: 0; }
  .sub { font-size: 18px; line-height: 1.6; color: var(--body);
    margin: 22px 0 0; max-width: 440px; }
  .cta { display: flex; flex-wrap: wrap; gap: 14px; margin-top: 30px; font-size: 16px; }
  .btn { display: inline-block; padding: 12px 24px; border-radius: 8px;
    text-decoration: none; background: var(--accent); color: var(--paper);
    border: 0; font: inherit; cursor: pointer; }
  .btn:hover { background: #a54019; }
  .btn-quiet { background: transparent; color: var(--ink);
    box-shadow: inset 0 0 0 1px var(--line); }
  .btn-quiet:hover { background: rgba(33,29,23,0.04); }
  .btn[disabled] { opacity: 0.5; cursor: default; }

  /* Hero tile — the icon at display size. It is the product: an article
     already on the device. */
  .tile { width: 256px; height: 256px; border-radius: 56px; background: var(--tile);
    position: relative; overflow: hidden; flex-shrink: 0;
    box-shadow: 0 24px 50px -24px rgba(33,29,23,.5); }
  .tile .mark { position: absolute; top: 39px; left: 50%; transform: translateX(-50%);
    width: 118px; height: 178px; border-radius: 13px; padding: 10px 10px 23px; }
  .tile .mark > .screen { border-radius: 4px; }
  .tile .ribbon { top: -1px; right: 18px; width: 17px; height: 52px; }
  .tile .t { font-size: 95px; margin-top: 11px; }

  @media (max-width: 860px) {
    .hero { flex-direction: column-reverse; align-items: flex-start; gap: 40px;
      padding: 44px 0 56px; }
    .sub { max-width: none; }
  }

  /* ── sections ─────────────────────────────────────────────────────── */
  h2 { font-size: 13px; letter-spacing: 0.28em; text-transform: uppercase;
    color: var(--eyebrow); font-weight: 500; margin: 0 0 26px; }
  section { padding: 46px 0; border-top: 1px solid var(--rule); }
  /* 320px min lands these 2-up rather than 3-up: there are four, and three
     columns orphans the last one on its own row. */
  .points { margin: 0; padding: 0; list-style: none; display: grid;
    grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 30px 52px; }
  .points b { font-weight: 500; display: block; font-size: 19px; margin-bottom: 6px; }
  .points span { color: var(--body); font-size: 16px; line-height: 1.55; }

  table.sizes { width: 100%; border-collapse: collapse; font-size: 16px; max-width: 460px; }
  table.sizes td { padding: 11px 0; border-bottom: 1px solid var(--rule); }
  table.sizes td + td { text-align: right; color: var(--muted);
    font-variant-numeric: tabular-nums; }
  table.sizes tr:last-child td { border-bottom: 0; }

  /* ── invite ───────────────────────────────────────────────────────── */
  .invite { max-width: 520px; }
  .invite .lede { color: var(--body); font-size: 17px; margin: 0 0 22px; }
  .invite .row { display: flex; flex-wrap: wrap; gap: 10px; }
  .invite input[type=email] { flex: 1 1 240px; min-width: 0; padding: 12px 16px;
    border: 1px solid var(--line); border-radius: 8px; background: #fffdf8;
    color: var(--ink); font: inherit; font-size: 16px; }
  .invite input[type=email]:focus { outline: 2px solid var(--accent); outline-offset: 1px; }
  /* Off-screen, not display:none — some form-fillers skip hidden fields, and
     a honeypot only works if a bot believes it is real. */
  .hp { position: absolute; left: -9999px; width: 1px; height: 1px; overflow: hidden; }
  .invite .msg { font-size: 15px; color: var(--muted); margin: 14px 0 0; min-height: 1.4em; }
  .invite .msg.bad { color: #a3301c; }
  .cf-turnstile { margin: 16px 0 0; }

  footer.site { border-top: 1px solid var(--rule); margin-top: 46px;
    padding: 26px 0 64px; font-size: 15px; color: var(--muted);
    display: flex; flex-wrap: wrap; gap: 20px; align-items: center; }
  footer.site a { color: var(--muted); text-decoration: none; }
  footer.site a:hover { color: var(--ink); }
</style>
</head>
<body>
<div class="wrap">

  <header class="site">
    <a class="lockup" href="{{.Base}}/">
      <span class="mark mark-sm"><span class="screen">
        <span class="ribbon"></span><span class="t">T</span>
      </span></span>
      <span class="word">Tome</span>
    </a>
    <nav class="site">
      <a class="hide-sm" href="#what">What it does</a>
      <a class="hide-sm" href="#sized">Sized for</a>
      {{if .InviteForm}}<a class="accent" href="#invite">Get an invite</a>
      {{else if .InviteURL}}<a class="accent" href="{{.InviteURL}}">Get an invite</a>{{end}}
    </nav>
  </header>

  <div class="hero">
    <div class="hero-copy">
      <div class="eyebrow">Read it properly</div>
      <h1>Long articles deserve better than a phone at 11pm.</h1>
      <p class="sub">Tome typesets whatever you're reading and drops it on your
      Kindle — one click.</p>
      <div class="cta">
        {{if .InviteForm}}<a class="btn" href="#invite">Get an invite</a>
        {{else if .InviteURL}}<a class="btn" href="{{.InviteURL}}">Get an invite</a>{{end}}
        <a class="btn btn-quiet" href="{{.Base}}/install">Add to Chrome</a>
      </div>
    </div>

    <div class="tile" role="img" aria-label="A Kindle showing a typeset page, bookmarked">
      <span class="mark"><span class="screen">
        <span class="ribbon"></span><span class="t">T</span>
      </span></span>
    </div>
  </div>

  <section id="what">
    <h2>What it does</h2>
    <ul class="points">
      <li><b>One click, from the article</b>
        <span>A browser extension reads the page you're on, so login-only
        articles work — you're already signed in.</span></li>
      <li><b>Typeset, not converted</b>
        <span>Rendered through a real browser at your device's exact page size,
        fonts embedded. Not a reflowed HTML dump.</span></li>
      <li><b>Straight to the device</b>
        <span>Delivered to your Kindle address — or any address you like, so a
        rule in your mail or something like Zapier can take it from there. Or
        keep the PDF; the preview is print-ready.</span></li>
      <li><b>Nothing kept</b>
        <span>Articles are rendered and delivered, never stored. The server
        holds your email, your Kindle address, and a hash of your key.</span></li>
    </ul>
  </section>

  <section id="sized">
    <h2>Sized for</h2>
    <table class="sizes">
      <tr><td>Kindle Scribe</td><td>157 × 210 mm</td></tr>
      <tr><td>Kindle Scribe 3</td><td>168 × 224 mm</td></tr>
      <tr><td>Kindle Paperwhite</td><td>105 × 140 mm</td></tr>
    </table>
  </section>

  {{if .InviteForm}}
  <section id="invite">
    <h2>Get an invite</h2>
    <div class="invite">
      <p class="lede">Tome runs on someone's own server — this one is
      invite-only. Leave your address and you'll get a code back.</p>
      <form id="invite-form" novalidate>
        <div class="row">
          <input id="invite-email" type="email" name="email" required
                 autocomplete="email" placeholder="you@example.com" aria-label="Your email address">
          <button class="btn" type="submit" id="invite-submit">Request</button>
        </div>
        <div class="hp" aria-hidden="true">
          <label>Website<input type="text" name="website" id="invite-hp" tabindex="-1" autocomplete="off"></label>
        </div>
        {{if .TurnstileSite}}<div class="cf-turnstile" data-sitekey="{{.TurnstileSite}}"
             data-action="turnstile-spin-v2" data-theme="light"></div>{{end}}
        <p class="msg" id="invite-msg" role="status" aria-live="polite"></p>
      </form>
    </div>
  </section>
  {{end}}

  <footer class="site">
    <a href="https://github.com/patali/tome">Source</a>
    <a href="{{.Base}}/privacy">Privacy</a>
    <a href="{{.Base}}/install">Install guide</a>
    <span>Self-hostable · MIT</span>
  </footer>

</div>

{{if .InviteForm}}
{{if .TurnstileSite}}<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>{{end}}
<script>
(function () {
  var form = document.getElementById('invite-form');
  var msg  = document.getElementById('invite-msg');
  var btn  = document.getElementById('invite-submit');
  if (!form) return;

  function say(text, bad) {
    msg.textContent = text;
    msg.className = bad ? 'msg bad' : 'msg';
  }

  form.addEventListener('submit', function (e) {
    e.preventDefault();
    var email = document.getElementById('invite-email').value.trim();
    if (!email) { say('Enter an email address first.', true); return; }

    // Turnstile writes its token into a hidden input it injects itself.
    var t = form.querySelector('[name="cf-turnstile-response"]');

    btn.disabled = true;
    say('Sending…');

    // Same-origin: a misconfigured Base would make this a cross-origin POST
    // and land it in CORS preflight for no reason.
    fetch('/invite-request', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        email: email,
        website: document.getElementById('invite-hp').value,
        turnstile: t ? t.value : ''
      })
    }).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (b) {
        return { ok: r.ok, body: b };
      });
    }).then(function (res) {
      if (res.ok) {
        form.querySelector('.row').style.display = 'none';
        say('Got it — you\'ll hear back at ' + email + '.');
        return;
      }
      btn.disabled = false;
      say(res.body.error || 'Something went wrong. Try again shortly.', true);
      // Tokens are single-use: a retry needs a fresh one or the edge rejects
      // it as timeout-or-duplicate.
      if (window.turnstile) { window.turnstile.reset(); }
    }).catch(function () {
      btn.disabled = false;
      say('Could not reach the server. Try again shortly.', true);
    });
  });
})();
</script>
{{end}}
</body>
</html>
`))
