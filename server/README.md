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
cd server
container build -t tome .                    # or: docker build -t tome .
container volume create tome-data            # docker: volume is created implicitly
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

Two explicit send actions (the extension shows a button for each, toggleable
in its settings):

- **Send to Kindle** (`/send-to-kindle`) — Resend emails the file straight to
  the authed user's own Kindle address. Requires the admin to have configured
  Resend; 502 otherwise.
- **Send via email** (`/send-via-mail`) — opens **Mail.app on the server's Mac**
  with the file attached, addressed to the user's Kindle; you review and hit
  Send. Admin-only, macOS-only (it's a same-machine convenience).

## Admin CLI

`tome admin` is a thin client for the HTTP admin API — run it from anywhere
that can reach the server (env: `TOME_SERVER_URL`, `TOME_ADMIN_KEY`):

```
tome admin invites create [--email HINT] [--ttl 168h] [--send]
tome admin invites list | invites delete CODE
tome admin users list | users disable ID | users enable ID | users rotate-key ID
tome admin settings get | settings set [--resend-api-key K] [--resend-from ADDR]
```

Lost the admin key? `container exec -u tome tome tome init-admin --rotate-key`.

## Endpoints

Auth is `Authorization: Bearer tome_…`. Errors are always `{ "error": "…" }`.

| Method | Path | Auth | Notes |
|---|---|---|---|
| `GET` | `/status` | — | `{ ok, service, version, authRequired, defaultFormat, pdfAvailable }` |
| `POST` | `/auth/accept-invite` | — (rate-limited) | `{code, email, kindleEmail}` → `{apiKey, …}` — key shown once |
| `POST` | `/convert` | user | article JSON → rendered file (`?format=pdf\|epub`) |
| `POST` | `/send-to-kindle` | user | article JSON → Resend delivery to the user's Kindle; 502 if Resend unset |
| `POST` | `/send-via-mail` | admin on macOS | article JSON → opens local Mail.app with the file attached |
| `GET` / `PUT` | `/me` | user | own profile / update own `kindleEmail` |
| * | `/admin/invites[…]`, `/admin/users[…]`, `/admin/settings` | admin | see CLI above; the Resend API key is write-only (never echoed) |

Article JSON: `{ title, byline, publishedTime, content (HTML), url, device?, format?, css? }`.
- `device`: `scribe` (default) · `scribe3` · `paperwhite` — sets the PDF page size.
- `css`: the reader stylesheet; the extension ships `extension/reader.css` (the
  single source of truth) in every request. The server's embedded fallback is a
  compact approximation — don't tune typography there.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `TOME_PORT` | `8080` | listen port |
| `TOME_DATA_DIR` | `./data` (`/data` in the image) | SQLite database location |
| `TOME_CHROME` | auto-detected | Chrome-family binary for PDF rendering |
| `TOME_CHROME_FLAGS` | — (`--no-sandbox --disable-dev-shm-usage` in the image) | extra Chrome flags |
| `TOME_RESEND_BASE_URL` | `https://api.resend.com` | override for testing |
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
    ├── epubgen/         # Article -> EPUB fallback
    └── kindle/          # macOS Mail.app hand-off (admin fallback)
```

## Known limitations

- **PDF is fixed-layout** (device page size), not reflowable — great for Scribe,
  less ideal for the 6.8" Paperwhite; use `format=epub` there if you prefer reflow.
- **Grayscale is a CSS `filter`** in the render, not a true image conversion.
- Headless Chrome renders the PDF but sometimes doesn't self-exit on macOS; the
  server polls for the finished file and then kills the process group.
