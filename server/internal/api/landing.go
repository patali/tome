package api

import (
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/patali/tome/server/internal/pdfgen"
)

// inviteURL (TOME_INVITE_URL) is where the landing page's "Get an invite"
// button points — a mailto:, a DM link, a form, whatever the operator prefers.
// Unset, the button is replaced by a line of text: this server is public
// software, and a dead button on someone else's instance is worse than none.
var inviteURL = strings.TrimSpace(os.Getenv("TOME_INVITE_URL"))

// landingFont is the one face served over HTTP. The landing page is set in the
// same typeface the documents are, so the page doubles as a specimen of what
// you get — the pitch is typography, and describing typography is a poor
// substitute for showing it.
const landingFont = "Literata-normal-latin.woff2"

// handleFont serves an embedded font. Only the landing face is exposed: this
// is a specimen, not a font CDN.
func (s *Server) handleFont(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != landingFont {
		http.NotFound(w, r)
		return
	}
	b, ok := pdfgen.FontFile(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/woff2")
	// Content is immutable for a given binary, and the filename changes when
	// the family does.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
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

// The font is referenced root-relative rather than through {{.Base}} on
// purpose: it is a subresource, and a Base pointing at another origin would
// make it a cross-origin font request and get it blocked for want of CORS.
// User-visible links keep using Base, which is what proxies need rewritten.
var landingTmpl = template.Must(template.New("landing").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Tome — read it properly</title>
<meta name="description" content="Turn the article you're reading into a beautifully typeset document on your Kindle.">
<style>
  @font-face {
    font-family: 'Literata';
    src: url('/fonts/` + landingFont + `') format('woff2');
    font-weight: 400; font-style: normal; font-display: swap;
  }

  :root {
    color-scheme: light dark;
    --ink: #14110e;
    --paper: #faf8f5;
    --muted: #6b625a;
    --rule: rgba(20,17,14,0.12);
    --accent: #7a4b2a;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --ink: #ece7e0;
      --paper: #16151a;
      --muted: #9a938b;
      --rule: rgba(236,231,224,0.14);
      --accent: #d9a679;
    }
  }

  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 0;
    background: var(--paper); color: var(--ink);
    font: 17px/1.65 Literata, Georgia, 'Times New Roman', serif;
    -webkit-font-smoothing: antialiased;
  }
  .wrap { max-width: 660px; margin: 0 auto; padding: 0 22px; }

  header { padding: 72px 0 0; }
  .mark { font-size: 15px; letter-spacing: 0.14em; text-transform: uppercase;
    color: var(--muted); }
  h1 { font-size: clamp(34px, 7vw, 52px); line-height: 1.12; margin: 18px 0 0;
    font-weight: 400; letter-spacing: -0.015em; }
  .lede { font-size: clamp(18px, 2.6vw, 21px); color: var(--muted);
    margin: 20px 0 0; }

  .cta { margin: 38px 0 0; display: flex; flex-wrap: wrap; gap: 12px;
    align-items: center; }
  .btn { display: inline-block; padding: 14px 26px; border-radius: 999px;
    background: var(--ink); color: var(--paper); text-decoration: none;
    font-size: 16px; letter-spacing: 0.01em; }
  .btn:hover { opacity: 0.86; }
  .btn-quiet { background: transparent; color: var(--ink);
    border: 1px solid var(--rule); }
  .cta-note { font-size: 15px; color: var(--muted); }

  /* A page, roughly Scribe-proportioned (157x210mm), as its own specimen. */
  .specimen { margin: 62px 0 0; border: 1px solid var(--rule); border-radius: 3px;
    background: var(--paper); padding: 42px 40px 46px;
    box-shadow: 0 1px 0 var(--rule), 0 18px 40px -32px rgba(0,0,0,0.5); }
  .specimen .t { font-size: 25px; line-height: 1.22; margin: 0 0 6px; }
  .specimen .by { font-size: 14px; color: var(--muted); margin: 0 0 22px;
    letter-spacing: 0.03em; }
  .specimen p { margin: 0 0 14px; text-align: justify; hyphens: auto; }
  .specimen p:first-of-type::first-letter { float: left; font-size: 3.1em;
    line-height: 0.84; padding: 0.06em 0.09em 0 0; }
  .caption { font-size: 14px; color: var(--muted); margin: 14px 0 0;
    text-align: center; }

  h2 { font-size: 15px; letter-spacing: 0.14em; text-transform: uppercase;
    color: var(--muted); font-weight: 400; margin: 68px 0 22px; }
  .points { margin: 0; padding: 0; list-style: none; }
  .points li { margin: 0 0 26px; padding: 0 0 0 22px; border-left: 2px solid var(--rule); }
  .points b { font-weight: 400; display: block; font-size: 19px; }
  .points span { color: var(--muted); font-size: 16px; }

  table.sizes { width: 100%; border-collapse: collapse; font-size: 15px; }
  table.sizes td { padding: 9px 0; border-bottom: 1px solid var(--rule); }
  table.sizes td + td { text-align: right; color: var(--muted);
    font-variant-numeric: tabular-nums; }
  table.sizes tr:last-child td { border-bottom: 0; }

  .invite .row { display: flex; flex-wrap: wrap; gap: 10px; }
  .invite input[type=email] { flex: 1 1 240px; min-width: 0; padding: 13px 16px;
    border: 1px solid var(--rule); border-radius: 999px; background: transparent;
    color: var(--ink); font: inherit; font-size: 16px; }
  .invite input[type=email]:focus { outline: 2px solid var(--accent); outline-offset: 1px; }
  .invite button { border: 0; cursor: pointer; font: inherit; font-size: 16px; }
  .invite button[disabled] { opacity: 0.5; cursor: default; }
  /* Off-screen rather than display:none — some form-fillers skip hidden
     fields, and a honeypot only works if a bot believes it is real. */
  .hp { position: absolute; left: -9999px; width: 1px; height: 1px; overflow: hidden; }
  .invite .msg { font-size: 15px; color: var(--muted); margin: 12px 0 0; min-height: 1.4em; }
  .invite .msg.bad { color: #b3261e; }
  @media (prefers-color-scheme: dark) { .invite .msg.bad { color: #f2b8b5; } }
  .cf-turnstile { margin: 14px 0 0; }

  footer { margin: 76px 0 0; padding: 26px 0 60px; border-top: 1px solid var(--rule);
    font-size: 15px; color: var(--muted); display: flex; flex-wrap: wrap; gap: 18px; }
  footer a { color: var(--muted); }
  a { color: var(--accent); }
</style>
</head>
<body>
<div class="wrap">

  <header>
    <div class="mark">Tome</div>
    <h1>Read it properly.</h1>
    <p class="lede">Long articles deserve better than a phone screen at 11pm.
    Tome turns whatever you're reading into a typeset document and puts it on
    your Kindle — one click, no tab left open for three weeks.</p>

    <div class="cta">
      {{if .InviteForm}}<a class="btn" href="#invite">Get an invite</a>
      {{else if .InviteURL}}<a class="btn" href="{{.InviteURL}}">Get an invite</a>{{end}}
      <a class="btn btn-quiet" href="{{.Base}}/install">Set up the extension</a>
      {{if and (not .InviteForm) (not .InviteURL)}}<span class="cta-note">Invite-only — ask whoever runs this server.</span>{{end}}
    </div>
  </header>

  <div class="specimen">
    <div class="t">On the Pleasures of Reading Slowly</div>
    <div class="by">14 min · sent to your Kindle</div>
    <p>Everything on this page is set in Literata, the same face your documents
    are rendered in. What you're reading now is what lands on the device —
    real margins, real hyphenation, a measure that doesn't run to the edge of
    the glass.</p>
    <p>Images come through. Code blocks stay monospaced. Footnotes stay
    footnotes. The page is sized to your device in millimetres, so nothing is
    scaled down to fit after the fact.</p>
  </div>
  <p class="caption">Not a screenshot — this page is the specimen.</p>

  <h2>What it does</h2>
  <ul class="points">
    <li><b>One click, from the article</b>
      <span>A browser extension reads the page you're on, so paywalled-to-scrapers
      and login-only articles work — you're already signed in.</span></li>
    <li><b>Typeset, not converted</b>
      <span>Rendered through a real browser at your device's exact page size,
      with fonts embedded. Not a reflowed HTML dump.</span></li>
    <li><b>Straight to the device</b>
      <span>Delivered to your Kindle address. Or take the PDF yourself — the
      preview is print-ready.</span></li>
    <li><b>Nothing kept</b>
      <span>Articles are rendered and delivered, never stored. The server holds
      your email, your Kindle address, and a hash of your key.</span></li>
  </ul>

  <h2>Sized for</h2>
  <table class="sizes">
    <tr><td>Kindle Scribe</td><td>157 × 210 mm</td></tr>
    <tr><td>Kindle Scribe 3</td><td>168 × 224 mm</td></tr>
    <tr><td>Kindle Paperwhite</td><td>105 × 140 mm</td></tr>
  </table>

  {{if .InviteForm}}
  <h2 id="invite">Get an invite</h2>
  <p class="lede" style="font-size:17px">Tome runs on someone's own server —
  this one is invite-only. Leave your address and you'll get a code back.</p>

  <form id="invite-form" class="invite" novalidate>
    <div class="row">
      <input id="invite-email" type="email" name="email" required
             autocomplete="email" placeholder="you@example.com" aria-label="Your email address">
      <button class="btn" type="submit" id="invite-submit">Request</button>
    </div>
    <!-- Hidden from people, irresistible to naive bots. Not display:none —
         some form-fillers skip that; off-screen with aria-hidden reads as a
         real field to them and as nothing to a screen reader. -->
    <div class="hp" aria-hidden="true">
      <label>Website<input type="text" name="website" id="invite-hp" tabindex="-1" autocomplete="off"></label>
    </div>
    {{if .TurnstileSite}}<div class="cf-turnstile" data-sitekey="{{.TurnstileSite}}"
         data-action="turnstile-spin-v2" data-theme="auto"></div>{{end}}
    <p class="msg" id="invite-msg" role="status" aria-live="polite"></p>
  </form>
  {{end}}

  <footer>
    <a href="https://github.com/patali/tome">Source</a>
    <a href="{{.Base}}/privacy">Privacy</a>
    <a href="{{.Base}}/install">Install guide</a>
    <span>Self-hostable</span>
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

    // Same-origin on purpose: a misconfigured Base would make this a
    // cross-origin POST and land it in CORS preflight for no reason.
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
