# Tome — Web Articles to Kindle-Optimized PDFs

> Project plan + running status. Tome converts articles you're reading in the
> browser (X/Twitter today; Medium, Substack, blogs later) into e-ink-optimized
> PDFs and sends them to your Kindle.

## Problem

X (Twitter) articles are long-form content locked behind authentication. There's no API for articles, and the pages return nothing to unauthenticated scrapers (`robots.txt` blocks automated access). Existing tools (x-thread.org, Thread2Print, Tweets2PDF) handle threads but not the newer X article format well, and none produce e-ink-optimized output.

The goal: a tool that takes an X article I'm already reading in my browser and produces a beautifully typeset PDF (or EPUB) optimized for reading on Kindle Scribe (10.2", 300 PPI) or Kindle Paperwhite (6.8", 300 PPI).

## Key Constraint

**Articles don't render without being logged in.** This means headless-browser-fetching-a-URL approaches won't work without fragile cookie/session stealing. The correct architecture is: **extract from the live DOM in the browser where I'm already authenticated**, then typeset locally.

## Architecture

```
┌─────────────────────────────────┐
│  Browser (authenticated on X)   │
│                                 │
│  Extension runs extractors      │
│  (x.js / Readability) → gets:   │
│  - title, author, date          │
│  - body HTML (clean)            │
│  - images (as data URIs/URLs)   │
└──────────┬──────────────────────┘
           │ POST JSON
           ▼
┌─────────────────────────────────┐
│  Local Go Server (:8080)        │
│                                 │
│  1. Receive clean HTML/markdown │
│  2. Download & process images   │
│     (grayscale, high contrast)  │
│  3. Convert to Typst markup     │
│  4. Compile with Typst engine   │
│  5. Return PDF / send to Kindle │
└──────────┬──────────────────────┘
           │
           ▼
     ┌─────────────┐
     │  Output:     │
     │  • PDF file  │
     │  • EPUB file │
     │  • Send to   │
     │    Kindle    │
     └─────────────┘
```

## Current Status (updated 2026-07-15)

We deliberately went **out of the original sequence** — the browser piece and the
server got pulled forward together once the bookmarklet proved awkward in Arc, and
Send-to-Kindle (originally a later Go-server phase) got built early. What exists now:

**Done**
- **Phase 0 — bookmarklet** (retired, directory removed): validated that
  Readability extracts X articles cleanly from the live DOM before the extension
  superseded it.
- **Browser extension** (`extension/`, MV3): the daily driver, replaces the
  bookmarklet (Arc has no bookmarks bar; the extension also sidesteps CSP via the
  isolated content-script world). Toolbar popup → **Open reader tab** (Cmd+P → PDF)
  or **Send to Kindle**. `activeTab`-scoped, no broad host access.
- **Local Go server** (`server/`): `POST /convert` (returns EPUB), `POST
  /send-to-kindle`, `GET /status`. Verified: builds, serves, emits **XML-well-formed
  EPUB** (void elements self-closed, links/tables/code preserved).
- **Send-to-Kindle delivery, two paths** (auto-selected): **SMTP** when configured,
  else a macOS **Mail.app fallback** (`osascript` opens a pre-composed message with
  the attachment — no SMTP creds needed; you hit Send). Default Kindle address
  `spatali.scribe@kindle.com`. Verified the Mail.app hand-off end-to-end.
- **PDF rendering via headless Chrome** (`server/internal/pdfgen/`, **default format**):
  renders the extracted HTML with `--print-to-pdf` at device-accurate page sizes
  (verified 157×210mm for Scribe). A real browser fetches images and honors the
  reader CSS, so **images embed, tables render, and HTML entities work** — the things
  EPUB got wrong. Chrome renders the *local* extracted HTML; **it never visits x.com**,
  so no X authentication is involved (only public `pbs.twimg.com` images + fonts load).
  Tuned dense: 9.5pt body, 10mm margins (~360 words/page on Scribe).
- **Single stylesheet**: `extension/reader.css` is the one source of truth for e-ink
  typography. The reader page links it, the extension ships it in the
  `/send-to-kindle` payload (so the PDF matches the reader exactly). The server
  keeps only a compact fallback for bare-curl requests.
- **Fixed a wrong Scribe page size** (was 119×159mm from a bad 1404×1872 spec; the real
  10.2" Scribe is 1860×2480 @300 PPI = **157×210mm**). At the true size the PDF renders
  1:1 on the device instead of being magnified ~1.3×, so ~90% more content fits per page.
- **EPUB entity bug fixed**: `&nbsp;`/`&mdash;`/etc. now decode to real characters
  (the parse context needed its `DataAtom`), so EPUB XHTML is valid. EPUB remains the
  fallback when Chrome isn't present.

**Findings that resolved the open questions**
- Readability handles X's React DOM fine once extracted from the live (authenticated)
  page; `document.cloneNode(true)` needs a `<base href>` injected so relative links
  absolutize.
- X in-article links **are** real `<a href>` with absolute URLs — Readability keeps
  them. Rendering choice: **underline only** (no inline URL / endnotes), per preference.
- Images come through as normal `<img>` (CDN URLs); no data-URI capture needed so far.

**Deviations from the original plan (intentional)**
- **PDF via headless Chrome, not Typst.** `typst` isn't installed, and rendering the
  already-good reader HTML in a real browser fixes images/tables/entities for free at
  device-accurate page sizes. Typst remains a possible pure-toolchain alternative.
- **Server config is env vars, not TOML** (see [Config](#config-file)) — fewer deps,
  password stays out of files.

**Resolved: dedicated X extractor (Readability alone was not enough)**
Analysis of a saved rendered article DOM showed Readability failing X articles in
four ways: title came out "X" (the real title is a `<div
data-testid="twitter-article-title">`, not a heading, and `og:title` is literally
"X"); the cover photo sits *before* the body container so it was excluded; **all
section `<h2>`s were silently dropped** (short heading-only siblings fail
Readability's boilerplate heuristics); and the byline was mushed. The fix
(`extension/extractors/x.js`): extract directly from X's
stable `data-testid` landmarks — `twitterArticleRichTextView` for the body
(sanitized: chrome/styles/classes stripped, leaf divs → `<p>`, image URLs upgraded
to `name=large`), `twitter-article-title`, `UserAvatar-Container-<handle>` +
profile link for `Name (@handle)`, `time[datetime]` for the date, and the first
`tweetPhoto` preceding the body as the cover image. Readability remains the
fallback for non-X pages. Verified against the saved DOM: title, byline, date,
cover + 3 body images, all 7 h2s, 4 code blocks, the table, and zero leftover
styles/classes/testids.

**Modular extractor architecture** (future sources: Medium, Substack, blogs)
Extraction is pluggable: each file in `extension/extractors/` registers
`{ priority, matches(location), extract(document, location) }` into a shared
registry in the page's isolated world; `background.js` injects the files and
dispatches to the highest-priority match (extractors can return `null` to pass).
Current modules: `extractors/x.js` (priority 100) and `extractors/generic.js`
(Readability fallback, priority 0). A new source is one new file + one entry in
`EXTRACTOR_FILES` — the reader, server, rendering, and delivery are all
source-agnostic. See `extension/README.md` → "Adding a new source".

**Next (re-sequenced)**
1. Image robustness: if a `pbs.twimg.com` image ever needs auth, have the extension
   inline images as **data URIs** from the already-authenticated DOM before POSTing.
2. Server-side image processing (grayscale, +contrast, sharpen) if Chrome's CSS
   `filter` isn't enough for e-ink.
3. Extension settings (device, output format, server URL); batch queue.
4. MCP wrapper; threads / Substack sources.

## Build Phases

### Phase 0 — Bookmarklet (done + retired; code removed after the extension shipped)

A bookmarklet that:
1. Injects Mozilla's Readability.js into the current X article page
2. Extracts title, byline, body content
3. Opens a new tab with the clean content rendered using an e-ink-optimized CSS stylesheet
4. User does `Cmd+P → Save as PDF` manually

**Purpose:** Validate that Readability.js can cleanly extract X article content, test typography choices on actual Kindle hardware before investing in the full pipeline.

**Tech:** Pure JS bookmarklet, Readability.js (from CDN or inlined), CSS.

**Files:**
- `bookmarklet.js` — the bookmarklet source
- `reader.html` — the clean reading template with e-ink CSS
- `README.md` — instructions

### Phase 1 — Browser Extension + Local Go Server (the daily driver)

#### Browser Extension (Chrome Manifest V3)

- Adds a 📖 icon/button when on `x.com/*/article/*` URLs
- On click: runs Readability.js content extraction in the page context
- Extracts: `{ title, author, date, content_html, images[], source_url }`
- POSTs to `http://localhost:8080/convert`
- Shows progress/status notification
- Optional: settings popup for choosing output format (PDF/EPUB), target device (Scribe/Paperwhite), and Kindle email address

**Files:**
```
extension/
├── manifest.json
├── content.js          # Readability extraction logic
├── background.js       # Service worker, handles POST to local server
├── popup.html          # Settings UI
├── popup.js
├── icons/
└── lib/
    └── Readability.js  # Mozilla's Readability (vendored)
```

#### Local Go Server

Receives extracted article content, produces Kindle-optimized output.

**Endpoints:**
- `POST /convert` — accepts article JSON, returns PDF
- `GET /status` — health check
- `POST /send-to-kindle` — convert + email to Kindle address

**Pipeline:**
1. Parse incoming HTML
2. Download images, convert to grayscale, optimize for e-ink
3. Convert HTML → Typst markup (or intermediate markdown → Typst)
4. Apply device-specific Typst template
5. Compile Typst → PDF
6. Optionally generate EPUB (for Paperwhite)
7. Return file or send to Kindle via email (SMTP)

**Files:**
```
server/
├── cmd/
│   └── tome/
│       └── main.go
├── internal/
│   ├── handler/        # HTTP handlers
│   ├── extractor/      # HTML parsing & cleanup
│   ├── images/         # Image download, grayscale, resize
│   ├── typst/          # HTML/MD → Typst conversion
│   ├── compiler/       # Typst compilation to PDF
│   ├── epub/           # EPUB generation
│   └── kindle/         # Send-to-Kindle email delivery
├── templates/
│   ├── article-scribe.typ    # Typst template for Kindle Scribe
│   ├── article-paperwhite.typ # Typst template for Paperwhite
│   └── fonts/                # Bundled fonts
├── go.mod
└── go.sum
```

**Key Go dependencies:**
- `github.com/go-shiori/go-epub` — EPUB generation **(in use)**
- `github.com/wneessen/go-mail` — SMTP for Send-to-Kindle **(in use)**
- `golang.org/x/net/html` — HTML parsing / XHTML normalization **(in use)**
- shell out to the `typst` CLI for PDF — *(planned; typst not yet installed)*
- `github.com/disintegration/imaging` — image processing (grayscale, resize) *(planned)*
- `github.com/yuin/goldmark` — markdown processing, if going HTML → MD → Typst *(planned)*

### Phase 2 — EPUB Output + MCP Server

- Add EPUB generation for Paperwhite (reflowable text is better on 6.8")
- Wrap the server as an MCP server so Claude Code can call it:
  `"convert this X article to Kindle PDF"` → tool call → done
- Batch queue: save multiple articles during the day, generate a single batch PDF or EPUB collection for evening reading

### Phase 3 — Polish & Extras

- Support for X threads (not just articles)
- Support for Substack, blog posts, and other long-form sources
- Table of contents generation for multi-article batches
- Reading time estimate on the extension popup
- Dark mode template variant (for Kindle with dark mode)

## E-Ink Typography Spec

This is the most important part for making it look "awesome." E-ink has different rendering characteristics than LCD/OLED.

### Page Dimensions (use actual device sizes, NOT A4/Letter)

| Device | Screen | Resolution | PDF Page Size |
|---|---|---|---|
| Kindle Scribe | 10.2" | 1860 × 2480 px @ 300 PPI | 157 × 210 mm |
| Kindle Scribe 3 | 11" | ~1980 × 2640 px @ 300 PPI | 168 × 224 mm |
| Kindle Paperwhite | 6.8" | 1236 × 1648 px @ 300 PPI | 105 × 140 mm |

### Fonts

| Role | Primary Choice | Fallback |
|---|---|---|
| Body text | Literata (Google Fonts, designed for screens) | Source Serif 4, Crimson Pro |
| Headings | Same family, heavier weight | — |
| Code blocks | JetBrains Mono | Iosevka, Fira Code |
| Metadata (author, date) | Body font, italic or lighter weight | — |

### Spacing & Layout

- **Body font size:** 11pt (Scribe), 10pt (Paperwhite)
- **Line height:** 1.5–1.6× body size
- **Paragraph spacing:** 0.5–0.8× body size (space between, no indent) OR first-line indent 1.5em with no space between — pick one style
- **Margins:** 15–20mm on Scribe (wider right margin for annotation), 10–12mm on Paperwhite
- **Max characters per line:** aim for 60–75 (the sweet spot for reading comfort)

### Color & Contrast

- Body text: pure `#000000` on `#FFFFFF` — e-ink renders mid-grays poorly
- Code block background: `#F0F0F0` (renders as very light gray on e-ink)
- Horizontal rules: `#CCCCCC`, thin (0.5pt)
- No color anywhere — everything grayscale
- Images: convert to grayscale, bump contrast +20%, apply mild sharpening

### Code Blocks (important for technical articles like ClaudeDevs)

- Monospace font at ~9pt
- Light gray background with 8–10px padding
- No syntax highlighting via color — use **bold** for keywords, *italic* for comments
- Line numbers optional, lighter weight if included
- Soft-wrap long lines (do NOT use horizontal scroll — e-ink can't scroll)
- If a code block exceeds ~30 lines, consider reducing font to 8pt

### Headers

- **H1 (title):** 18pt bold, generous top margin, followed by author/date metadata line
- **H2:** 14pt bold, small-caps variant looks excellent on e-ink
- **H3:** 12pt bold
- Use weight/size contrast only — no color differentiation

### Images

- Convert to grayscale
- Increase contrast (~20%)
- Apply mild sharpening for e-ink clarity
- Max width = text column width
- Add thin border (0.5pt, `#CCCCCC`) if image has white background (otherwise it bleeds into page)
- Captions in italic, slightly smaller than body text

### Article Metadata Block

At the top of each PDF, below the title:
```
Author · Published Date · Source: x.com/username/article/...
```
Small, italic, light gray. Functional but not dominant.

### Footer

Page number centered at bottom, small. Optionally include shortened source URL.

## Typst Template Skeleton

```typst
// article-scribe.typ — Kindle Scribe template

#set page(
  width: 157mm,
  height: 210mm,
  margin: (top: 10mm, bottom: 10mm, left: 10mm, right: 10mm),
  numbering: "1",
  number-align: center,
)

#set text(
  font: "Literata",
  size: 11pt,
  weight: "regular",
)

#set par(
  leading: 0.8em,    // line height
  spacing: 0.6em,    // between paragraphs
  justify: true,
)

#show heading.where(level: 1): set text(size: 18pt, weight: "bold")
#show heading.where(level: 2): set text(size: 14pt, weight: "bold")
#show heading.where(level: 3): set text(size: 12pt, weight: "bold")

#show raw.where(block: true): block.with(
  fill: luma(245),
  inset: 10pt,
  radius: 2pt,
  width: 100%,
)
#show raw: set text(font: "JetBrains Mono", size: 9pt)

// --- Article content injected below ---
```

## Readability.js Extraction Notes

Mozilla's Readability.js (https://github.com/mozilla/readability) is the engine behind Firefox Reader View. It's battle-tested on messy web pages.

**What it extracts:**
- `title` — article title
- `byline` — author name
- `content` — cleaned HTML string (main article body, images preserved)
- `excerpt` — first paragraph / description
- `siteName` — site name
- `length` — character count
- `lang` — detected language

**Usage in extension content script:**
```javascript
const documentClone = document.cloneNode(true);
const article = new Readability(documentClone).parse();
// article.title, article.byline, article.content (HTML string)
```

**Potential issues with X articles:**
- X may lazy-load images — ensure they're loaded before extraction
- Embedded tweets within articles may need special handling
- Code blocks might not be semantic `<pre><code>` — may need post-processing
- Test whether Readability.js correctly identifies the article boundary vs. reply threads

## Send-to-Kindle Integration

**Implemented** in `server/internal/kindle/` (SMTP via `go-mail`) and
`server/internal/epubgen/` (EPUB via `go-shiori/go-epub`). The extension's
**Send to Kindle** button POSTs the extracted article to `POST /send-to-kindle`;
the server builds the EPUB and emails it. `POST /convert` returns the EPUB as a
download for testing without SMTP. Config comes from `TOME_*` env vars
(see [Config](#config-file)); the sender must be an Amazon approved sender.

Amazon's Send to Kindle works via email:
- Each Kindle has a unique `@kindle.com` email address (find in Amazon account settings)
- Email the PDF/EPUB as an attachment
- Add `convert` in subject line for Amazon to attempt format optimization
- Sender email must be in the approved senders list
- Max attachment size: 50MB (PDF should be well under this)

Alternatively, use the Send to Kindle desktop app or web uploader for manual transfers.

## Config File

The server currently reads **environment variables** (fewer deps; the SMTP
password never lands in a file). A TOML config is a possible future convenience.

```bash
# Send-to-Kindle (all required for the /send-to-kindle endpoint)
export TOME_KINDLE_EMAIL="your-kindle@kindle.com"
export TOME_SENDER_EMAIL="you@gmail.com"   # must be an Amazon approved sender
export TOME_SMTP_USERNAME="you@gmail.com"
export TOME_SMTP_PASSWORD="app-password"   # e.g. a Gmail app password

# Optional (defaults shown)
export TOME_SMTP_HOST="smtp.gmail.com"
export TOME_SMTP_PORT="587"
export TOME_PORT="8080"                    # server listen port
```

Run the server:

```bash
cd server && go run ./cmd/tome/
# /convert (EPUB download) works without SMTP; /send-to-kindle needs the vars above
```

## Development Setup

```bash
# Prerequisites
go install # Go 1.22+
cargo install typst-cli # or download binary
# Fonts
mkdir -p ~/.local/share/fonts
# Download Literata and JetBrains Mono from Google Fonts

# Run server
cd server
go run ./cmd/tome/

# Load extension
# Chrome → chrome://extensions → Developer mode → Load unpacked → select extension/
```

## Open Questions

- [ ] Does Readability.js handle X articles cleanly, or does X's React DOM confuse it? (Test in Phase 0)
- [ ] Are X article images served from a CDN that allows cross-origin downloads, or do they need to be extracted as data URIs from the DOM?
- [ ] For the Scribe specifically: should the right margin be extra wide (25mm+) to leave room for pen annotations?
- [ ] Should EPUB be the default for Paperwhite instead of PDF? (Probably yes — reflowable text wins on 6.8")
- [ ] Consider `rod` (Go) as a backup extraction method for non-browser workflows — it can reuse Chrome's logged-in profile directory
- [ ] Typst compilation: shell out to `typst` CLI vs embed as library? CLI is simpler and always up to date; library avoids the dependency but requires CGo or Rust FFI

## References

- [Mozilla Readability.js](https://github.com/mozilla/readability)
- [Typst documentation](https://typst.app/docs/)
- [Chrome Extensions Manifest V3](https://developer.chrome.com/docs/extensions/mv3/)
- [Send to Kindle](https://www.amazon.com/sendtokindle)
- [Literata font](https://fonts.google.com/specimen/Literata)
- [JetBrains Mono](https://www.jetbrains.com/lp/mono/)
- [go-epub](https://github.com/bmaupin/go-epub)
- [rod - Go headless browser](https://github.com/go-rod/rod)
