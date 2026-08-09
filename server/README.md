# Tome — Server

Receives an extracted article (from the browser extension) and renders it into a
Kindle-friendly document — **PDF by default** (device-accurate, e-ink-optimized) or
**EPUB** — returned as a download or emailed to the user's Kindle.

The server is **multi-user and invite-only**: every request (except `/status`
and invite redemption) needs a per-user API key, each user has their own Kindle
address, and the admin manages invites, users, and email settings through a
JSON API / the `tome admin` CLI. There is no anonymous mode.

> **PDF** is rendered by driving **headless Chrome** over the extracted HTML
> (`--print-to-pdf` at the device page size). Chrome renders the *local*
> extracted HTML — **it never visits the source site**, so no login is involved.
> **EPUB** (pure-Go) is the fallback when no Chrome-family browser is installed
> (set `TOME_CHROME` to point at a specific binary).

## Quick start (self-host container)

Works with Docker or [Apple's `container`](https://github.com/apple/container)
(`brew install container`, then `container system start` once):

```bash
# from the REPO ROOT (the image bundles the extension for self-distribution)
container build -t tome -f server/Dockerfile .    # or: docker build -t tome -f server/Dockerfile .
container volume create tome-data                 # docker: volume is created implicitly
container run --detach --name tome -p 8080:8080 -v tome-data:/data tome
```

Bootstrap your admin account (prints your API key **once** — save it):

```bash
container exec -u tome tome tome init-admin \
  --email you@example.com --kindle you@kindle.com
export TOME_ADMIN_KEY=tome_…                 # from the output
```

Configure email delivery ([Resend](https://resend.com) — needs an API key and a
verified sending domain):

```bash
go run ./cmd/tome admin settings set \
  --resend-api-key re_xxxxxxxx --resend-from tome@yourdomain.com
# (or build the binary once: go build -o tome ./cmd/tome)
```

Invite a friend:

```bash
tome admin invites create --email friend@example.com --send   # emails them the code via Resend
tome admin invites create                                     # …or just print a code to share
```

They install the extension, set the server URL in the popup, and redeem the
code with their email + `@kindle.com` address — that's the whole signup.

**The server distributes the extension itself**: point invitees at
`https://your-server/` (the same page is at `/install`) — a step-by-step
install and Kindle-setup guide with a download
button for `GET /extension.zip` (bundled into the image at build time; when run
from source, the `extension/` dir is zipped on the fly). Invite emails link to
it automatically. Chrome blocks off-store one-click installs, so this is the
"Load unpacked" flow — the manifest pins a key, giving every install the same
extension ID.

> **Everyone** (you included) must add the `--resend-from` address to their
> Amazon **Approved Personal Document E-mail List** (amazon.com → Content &
> Devices → Preferences → Personal Document Settings), or Amazon silently
> rejects the deliveries.

## Run from source (no container)

```bash
cd server
go run ./cmd/tome            # serve on :8080, data in ./data (TOME_DATA_DIR)
go run ./cmd/tome init-admin --email you@example.com --kindle you@kindle.com
```

Accounts are still required — paste the printed admin key into the extension.

## Delivery methods

- **Send to Kindle** (`/send-to-kindle`) — Resend emails the file straight to
  the authed user's own Kindle address. Requires the admin to have configured
  Resend; 502 otherwise.
- **Open preview** — the extension's own reader tab, no server round trip.

`/convert` still returns the rendered file to any caller that wants to handle
delivery itself. A "Send via email" action that handed the file to the user's
local Mail.app used to sit on top of it; it needed a native-messaging helper
installed per machine, so it was removed from the extension.

## Admin CLI

`tome admin` is a thin client for the HTTP admin API — run it from anywhere
that can reach the server (env: `TOME_SERVER_URL`, `TOME_ADMIN_KEY`):

```
tome admin invites create [--email HINT] [--ttl 168h] [--send]
tome admin invites list | invites delete CODE
tome admin users list | users disable ID | users enable ID | users rotate-key ID
tome admin settings get | settings set [--resend-api-key K] [--resend-from ADDR]
                                      [--posthog-api-key K] [--posthog-host URL]
```

Lost the admin key? `container exec -u tome tome tome init-admin --rotate-key`.

## Analytics (optional, off by default)

The server always keeps its own conversion records — that is what
`tome admin conversions` reads, and it needs nothing configured. Those answer
*is my server healthy*.

If you also want to know *what to build next* — which devices people target,
which body faces get used, how often a render fails — the server can mirror
those same records to a [PostHog](https://posthog.com) project:

```bash
tome admin settings set --posthog-api-key phc_xxxxxxxx
tome admin settings set --posthog-host https://eu.i.posthog.com   # optional
tome admin settings set --posthog-api-key ""                      # turn it off
```

It is **your** PostHog project. Nothing reports to the authors of Tome, and the
server states at boot whether analytics is on and where it points.

Events are `conversion`, `invite_redeemed`, and `server_started`. A conversion
carries the kind, format, success, duration, a size *band*, and the chosen
device / font / colour — plus a failure *category* when it fails.

What is deliberately not sent, and cannot be: the article, its title, its URL,
its domain, any email address, or any reader's IP. Capture is server-side, so
PostHog only ever sees the server's address; GeoIP is disabled and person
profiles are switched off, so no per-person record is built. The client
[refuses any property value](internal/posthog/posthog.go) that looks like a URL
or an email address, so this holds even if a future call site gets careless.

Users are an opaque account number, meaningless outside this server's database.

Turning this on changes what a reader's data does, so
[`PRIVACY.md`](../PRIVACY.md) describes it — under "If the operator turned on
analytics". If you enable it, that section is now describing you.

## Endpoints

Auth is `Authorization: Bearer tome_…`. Errors are always `{ "error": "…" }`.

| Method | Path | Auth | Notes |
|---|---|---|---|
| `GET` | `/status` | — | `{ ok, service, version, authRequired, defaultFormat, pdfAvailable, extensionVersion }` — `extensionVersion` is read from the bundled extension's manifest, so installs can tell they've fallen behind |
| `GET` | `/` , `/install` | — | HTML install + Kindle-setup guide for invitees |
| `GET` | `/extension.zip` | — | the browser extension, for manual install |
| `POST` | `/auth/accept-invite` | — (rate-limited) | `{code, email, kindleEmail}` → `{apiKey, …}` — key shown once |
| `POST` | `/convert` | user | article JSON → rendered file (`?format=pdf\|epub`) |
| `POST` | `/send-to-kindle` | user | article JSON → Resend delivery to the user's Kindle; 502 if Resend unset |
| `GET` / `PUT` | `/me` | user | own profile / update own `kindleEmail` |
| * | `/admin/invites[…]`, `/admin/users[…]`, `/admin/settings` | admin | see CLI above; the Resend API key is write-only (never echoed) |

Article JSON: `{ title, byline, publishedTime, content (HTML), url, device?, format?, color?, font?, css? }`.
- `device`: `scribe` (default) · `scribe3` · `paperwhite` — sets the PDF page size.
- `font`: body face — `literata` (default) · `sourceserif` · `merriweather` ·
  `baskerville` · `inter` · `atkinson`. All are bundled and inlined into the
  render; an unknown value falls back to the default.
- `color`: `bw` (default) · `color` — grayscales images or leaves them alone.
  These are applied as `data-device` / `data-color` / `data-font` on `<html>`,
  which is what the shipped stylesheet keys its rules off.
- `css`: the reader stylesheet; the extension ships `extension/reader.css` (the
  single source of truth) in every request. The server's embedded fallback is a
  compact approximation — don't tune typography there.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `TOME_PORT` | `8080` | listen port |
| `TOME_DATA_DIR` | `./data` (`/data` in the image) | SQLite database location |
| `TOME_BASE_URL` | derived from the request | public URL in self-links (setup page, invite emails); set it when a proxy rewrites `Host` |
| `TOME_CHROME` | auto-detected | Chrome-family binary for PDF rendering |
| `TOME_CHROME_FLAGS` | — (`--no-sandbox --disable-dev-shm-usage` in the image) | extra Chrome flags |
| `TOME_RESEND_BASE_URL` | `https://api.resend.com` | override for testing |
| `TOME_POSTHOG_BASE_URL` | stored setting, else `https://us.i.posthog.com` | override for testing |
| `TOME_EXTENSION_PATH` | `/opt/tome/extension.zip` in the image, else `../extension` | what `/extension.zip` serves (zip file or source dir) |
| `TOME_SERVER_URL`, `TOME_ADMIN_KEY` | — | defaults for the `tome admin` CLI |

Resend credentials are **not** env vars — the admin sets them at runtime
(`tome admin settings set`) and they live in the database.

## Container notes

- The image bundles Chromium; the entrypoint starts as root only to `chown` the
  `/data` volume (runtimes like Apple's `container` mount named volumes
  root-owned), then drops to the unprivileged `tome` user. Use **named volumes**;
  a bind mount needs a host-side chown.
- **Apple `container` on macOS 26**: host↔container traffic rides vmnet, which
  macOS gates behind the **Local Network** privacy permission. If
  `curl localhost:8080/status` hangs while
  `container exec tome wget -qO- localhost:8080/status` works, grant Local
  Network access to your terminal app (System Settings → Privacy & Security →
  Local Network) and restart the runtime. VPNs (Tailscale, WARP) can also
  interfere with vmnet routing.
- Exposing the server to friends over the internet is on you: put it behind
  HTTPS (reverse proxy, Tailscale Funnel, Cloudflare Tunnel, …) — API keys ride
  the Authorization header and deserve TLS.

## Layout

```
server/
├── cmd/tome/            # serve | init-admin | admin (CLI over the admin API)
└── internal/
    ├── api/             # HTTP surface: public, authed, admin routes; CORS
    ├── auth/            # API keys, Bearer middleware, invite rate limiter
    ├── store/           # SQLite (pure Go): users, invites, settings
    ├── resend/          # minimal Resend API client (delivery + invite emails)
    ├── article/         # the request payload type + title-based filename
    ├── pdfgen/          # Article -> PDF via headless Chrome (default)
    └── epubgen/         # Article -> EPUB fallback
```

## Known limitations

- **PDF is fixed-layout** (device page size), not reflowable — great for Scribe,
  less ideal for the 6.8" Paperwhite; use `format=epub` there if you prefer reflow.
- **Grayscale is a CSS `filter`** in the render, not a true image conversion.
- Headless Chrome renders the PDF but sometimes doesn't self-exit on macOS; the
  server polls for the finished file and then kills the process group.
