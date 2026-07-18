# Tome — Local Server

Receives an extracted X article (from the browser extension) and renders it into a
Kindle-friendly document — **PDF by default** (device-accurate, e-ink-optimized) or
**EPUB** — returned as a download or emailed to your Kindle via Send-to-Kindle.

> **PDF** is rendered by driving **headless Chrome** over the extracted HTML
> (`--print-to-pdf` at the device page size). A real browser fetches images and
> honors the reader CSS, so images embed, tables render, and HTML entities work.
> Chrome renders the *local* extracted HTML — **it never visits x.com**, so no X
> login is involved; only public `pbs.twimg.com` images and fonts load.
>
> **EPUB** (pure-Go `go-shiori/go-epub`) is the fallback when no Chrome-family
> browser is installed. Set `TOME_CHROME` to point at a specific binary.

## Run

```bash
cd server
go run ./cmd/tome/     # listens on http://localhost:8080
```

`/convert` works immediately. `/send-to-kindle` needs the SMTP env vars below.

## Delivery methods

`/send-to-kindle` picks one automatically (see it in `GET /status` → `method`):

- **`smtp`** — all SMTP env vars set → emails the EPUB directly.
- **`mail-app`** — macOS only, SMTP not set → opens **Mail.app** with the EPUB
  attached and the Kindle address filled in; you review and hit **Send**. First use
  may trigger a macOS prompt to allow controlling Mail. The Kindle address defaults
  to `spatali.scribe@kindle.com` (override with `TOME_KINDLE_EMAIL`).
- **`none`** — not on macOS and no SMTP → `/convert` still returns the EPUB.

Either way, the account that actually sends must be an Amazon **approved sender**.

## Configure Send-to-Kindle (SMTP, optional)

```bash
export TOME_KINDLE_EMAIL="your-kindle@kindle.com"
export TOME_SENDER_EMAIL="you@gmail.com"   # must be an Amazon "approved sender"
export TOME_SMTP_USERNAME="you@gmail.com"
export TOME_SMTP_PASSWORD="app-password"   # Gmail: an App Password, not your login
# optional: TOME_SMTP_HOST (smtp.gmail.com), TOME_SMTP_PORT (587), TOME_PORT (8080)
```

Add your sender address to **Amazon → Manage Your Content and Devices →
Preferences → Personal Document Settings → Approved Personal Document E-mail List**,
and find your `@kindle.com` address on the same page.

## Endpoints

| Method | Path | Body | Response |
|---|---|---|---|
| `GET` | `/status` | — | `{ ok, kindleConfigured, method, defaultFormat, pdfAvailable }` |
| `POST` | `/convert` | article JSON | rendered file (PDF or EPUB) |
| `POST` | `/send-to-kindle` | article JSON | `{ ok, method, sentTo, filename, bytes }` or `{ error }` |

Article JSON: `{ title, byline, publishedTime, content (HTML), url, device?, format?, css? }`.
- `device`: `scribe` (default) · `scribe3` · `paperwhite` — sets the PDF page size.
- `format`: `pdf` (default when Chrome is present) · `epub`. `/convert` also accepts
  `?format=pdf|epub` as an override.
- `css`: the reader stylesheet. The extension ships `extension/reader.css` (the
  **single source of truth** for e-ink typography) in every request, so the PDF
  renders with exactly the CSS the reader tab shows. When absent (bare curl), the
  server uses a compact embedded fallback — do not tune typography there.

## Try it without the extension

```bash
curl -X POST http://localhost:8080/convert \
  -H 'Content-Type: application/json' \
  -d '{"title":"Test","byline":"Me","content":"<p>Hello <a href=\"https://example.com\">world</a>.</p>"}' \
  -o test.epub
```

## Layout

```
server/
├── cmd/tome/main.go   # HTTP server + handlers, CORS for the extension
└── internal/
    ├── article/              # the request payload type + title-based filename
    ├── pdfgen/               # Article -> PDF via headless Chrome (default)
    ├── epubgen/              # Article -> EPUB fallback (HTML normalized to XHTML)
    └── kindle/               # SMTP / Mail.app delivery + env config
```

## Known limitations

- **PDF is fixed-layout** (device page size), not reflowable — great for Scribe,
  less ideal for the 6.8" Paperwhite; use `format=epub` there if you prefer reflow.
- **Grayscale is a CSS `filter`** in the render, not a true image conversion.
- **Auth-protected images**: `pbs.twimg.com` media is public, but if X ever serves an
  image that needs the session, the fix is to have the extension inline images as data
  URIs from the already-authenticated DOM before POSTing.
- Headless Chrome renders the PDF but sometimes doesn't self-exit on macOS; the server
  polls for the finished file and then kills the process group.
