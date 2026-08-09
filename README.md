<p align="center">
  <img src="docs/assets/tome-icon.png" alt="Tome" width="200">
</p>

<h1 align="center">Tome</h1>

Turn web articles into beautifully typeset, e-ink-optimized documents on your
Kindle. It works on ordinary article pages — blogs, news, long-form posts — and
because the extension reads the page you already have open, it handles articles
behind a login that scrapers can't reach.

**How it works:** a browser extension extracts the article from the live DOM of
the page you're already reading (so authentication is never an issue), then a
local Go server typesets it into a device-accurate PDF via headless Chrome and
delivers it to your Kindle by email.

```
Browser (you, logged in)          Local Go server (:8080)
┌───────────────────────┐         ┌──────────────────────────────┐
│ Tome extension        │  POST   │ /convert        → PDF / EPUB │
│  extractors/x.js      │ ──────▶ │ /send-to-kindle → Resend →   │
│  extractors/generic.js│  JSON   │                   your Kindle│
└───────────────────────┘         └──────────────────────────────┘
```

## Quick start

1. **Install the extension** — [Tome on the Chrome Web
   Store](https://chromewebstore.google.com/detail/tome/mfnoejpbojcndlepcbkidppdinbbohmi)
   (Arc, Edge and Brave install from the same listing). Hacking on it? Load
   [`extension/`](extension/) unpacked instead — `chrome://extensions` →
   Developer mode → *Load unpacked*.
2. **Run the server** — `cd server && go run ./cmd/tome`
   (needs Go and any Chrome-family browser installed), then bootstrap your
   admin account: `go run ./cmd/tome init-admin --email you@example.com
   --kindle you@kindle.com` (prints your API key once).
3. In the popup → **Server settings**, paste the API key.
4. Open an article, click the **Tome** toolbar button →
   **Send to Kindle** (or **Open preview** → `Cmd+P` → Save as PDF).

## Self-host for your friends

Tome is **multi-user and invite-only**: run one server, invite people, and each
person uses the extension with their own account and Kindle address. Email
delivery goes through [Resend](https://resend.com) (admin-configured); admin
work happens through the `tome admin` CLI — no web dashboard to babysit.

```bash
cd server
container build -t tome .      # or: docker build -t tome .
container volume create tome-data
container run --detach --name tome -p 8080:8080 -v tome-data:/data tome
container exec -u tome tome tome init-admin --email you@example.com --kindle you@kindle.com
tome admin settings set --resend-api-key re_… --resend-from tome@yourdomain.com
tome admin invites create --email friend@example.com --send
```

Friends redeem their invite code right in the extension popup. Full setup,
endpoint docs, and container caveats in [`server/README.md`](server/README.md).

## Layout

| Path | What it is |
|---|---|
| [`extension/`](extension/README.md) | Chrome/Arc MV3 extension: pluggable per-site extractors, e-ink reader tab, Send-to-Kindle popup. `reader.css` here is the single source of truth for typography. |
| [`server/`](server/README.md) | Local Go server: PDF (headless Chrome) / EPUB (fallback) rendering, SMTP or macOS Mail.app delivery. |
| [`docs/plan.md`](docs/plan.md) | Project plan and running status — design decisions, findings, what's next. |

## Highlights

- **Device-accurate pages** — Kindle Scribe 157×210 mm, Paperwhite 105×140 mm;
  the PDF renders 1:1 on the device, no magnification.
- **Dense e-ink typography** — Literata 9.5 pt by default, ~360 words/page on
  Scribe; tables, code blocks, and links (underline style) all preserved.
- **Six body faces to choose from** — Literata, Source Serif 4, Merriweather,
  Libre Baskerville, Inter, Atkinson Hyperlegible; all picked for e-ink and all
  bundled, so the preview and the PDF agree.
- **Pluggable extractors** — [Readability.js](#third-party-code) handles pages
  generally, and a source can add its own extractor when the generic pass falls
  short, reading that site's own markup for title, byline, date and body.
- **No third-party calls** — every face ships in the package, so previewing
  works offline and nothing tells an outside service what you're reading.

## Third-party code

- **[Readability.js](https://github.com/mozilla/readability)** — Mozilla's
  article extractor, descended from Arc90's original `readability.js`. It does
  the heavy lifting behind [`extractors/generic.js`](extension/extractors/generic.js):
  handed a page, it works out which part of the DOM is the article and returns
  clean HTML, which is what lets Tome work on an arbitrary blog or news site
  without anyone writing a rule for it. Tome's own per-site extractors run ahead
  of it and fall through to it whenever they don't claim a page.

  Vendored unmodified at v0.5.0 in
  [`extension/lib/Readability.js`](extension/lib/Readability.js). **Apache
  License 2.0**, © 2010 Arc90 Inc — the licence header is kept intact at the top
  of the file.

- **Bundled fonts** — seven families under the SIL Open Font License 1.1, each
  notice kept in full beside the files. See
  [`extension/fonts/README.md`](extension/fonts/README.md).

Tome itself is MIT licensed — see [LICENSE](LICENSE).
