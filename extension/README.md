# Tome — Browser Extension (Arc / Chrome, MV3)

Click the toolbar button on an article → a clean, e-ink-optimized reader tab
opens (`Cmd+P` → **Save as PDF**), or send it straight to your Kindle via the
[local server](../server/README.md).

## Why an extension (and not a bookmarklet)

The extension injects its extractors in an **isolated content-script world**,
which is not subject to the page's Content-Security-Policy. So there's nothing
to inline or work around — it just runs. (An earlier bookmarklet approach died
on exactly that CSP wall, and Arc has no bookmarks bar anyway.)

## Install

**From the Chrome Web Store** —
[Tome](https://chromewebstore.google.com/detail/tome/mfnoejpbojcndlepcbkidppdinbbohmi).
Arc, Edge and Brave all install from that listing. It updates itself, and it
talks to whichever server you point it at, so one listing serves every
deployment.

> **Invitees:** you also need an invite code and a server URL. Your server's
> install page — `http://your-server/install` — walks through both, plus the
> Amazon approved-sender step that everyone forgets. It also offers this
> extension as a zip, for anyone who'd rather not use the store.

**From source (development)** — open **`chrome://extensions`** (Arc:
`arc://extensions`), turn on **Developer mode**, click **Load unpacked** and
select this **`extension/`** folder. Pin Tome from the puzzle-piece icon (in
Arc, extensions live to the right of the address bar).

The manifest pins a public key, so every unpacked install — from this folder or
from the server's zip — shares one extension ID and keeps its settings across
reloads. `pack-store.sh` strips that key for the store upload, because the store
rejects a pinned one and assigns an identity itself. The upshot: **the store
build is a different extension from an unpacked one**, with its own
`chrome.storage`. Running both gives you two Tome buttons and two sign-ins;
switching from one to the other means signing in again.

## Use

1. Open an article and **scroll through it once** so images and text load.
2. Click the **Tome** toolbar button — the popup shows up to two actions
   (each can be hidden from the settings page, gear icon **⚙**):
   - **Open preview** → clean reader tab. A panel docked to the right picks the
     device, flips images between **B&W** and **Colour**, switches the reading
     font, and offers `Cmd+P` → **Save as PDF** or **Send to Kindle** — the send
     uses the same job queue as the popup and carries whichever device, colour
     and font you chose. Collapse the panel with the tab on its edge to read
     without it in the way; the state is remembered.
   - **Send to Kindle** → the server renders a PDF and Resend emails it
     straight to your Kindle address.

Images are grayscaled by default because e-ink can't show colour and
flattening early keeps contrast predictable. **Colour** is there for a colour
Kindle, or a PDF you'll read on a screen.

The **reading font** can be set in the preview panel or on the settings page —
they edit the same preference. Six bundled faces, all chosen for e-ink (sturdy
strokes, open counters, generous x-heights rather than delicate high-contrast
shapes that grey out on a reflective display): **Literata** (default),
**Source Serif 4**, **Merriweather**, **Libre Baskerville**, **Inter** and
**Atkinson Hyperlegible**. Code stays JetBrains Mono throughout. The choice
travels with each conversion, so the document matches the preview.

Conversions run in the background service worker, not in the popup. Clicking
the page dismisses the popup — that's unavoidable for a browser popup — but the
job keeps going, and reopening shows where it got to. Each finished job is
shown once and then clears; the full list stays under **Recent conversions**
on the settings page.

> In the print dialog: **Margins: Default** and enable **Background graphics**
> so code-block shading prints.

### Settings page (gear icon ⚙)

All configuration lives on the settings page (popup → **⚙**, or the
extension's Options): server URL, account, and which action buttons the popup
shows.

Sign-in (accounts are required — the server is invite-only):

1. Set the **server URL** (defaults to `http://localhost:8080`) → Save.
2. Either **redeem an invite** — enter the invite code you were sent, your
   email, and your `@kindle.com` address — or paste an existing **API key**.
3. The popup status flips to your email and the send buttons unlock. Don't
   forget to add the server's sender address (shown after redeeming) to your
   Amazon approved-senders list.

The API key is stored in `chrome.storage.local` (never synced); the server URL
and button toggles sync via `chrome.storage.sync`. No host permission is asked
for at any point — a Tome server answers with permissive CORS, which is all an
MV3 fetch needs.

## Versioning and updates

The version lives in **`manifest.json`** and is the single source of truth —
the server reads it out of the bundle it serves and reports it at `/status`.

Chrome's format is 1–4 dot-separated integers (`0.3.0`), with no `-beta`
suffixes; we read it as semver otherwise. Bump it in the same commit as the
change, then ship both channels:

- **Store** — `./pack-store.sh` builds `tome-extension-store.zip` (key stripped,
  `manifest.json` at the archive root); upload it to the Chrome Web Store.
- **Server** — rebuild and push the image; the server hands out whatever
  `extension/` was baked in, so an unbumped manifest silently tells every manual
  install they're up to date.

Review lag means the two drift: `/status` can legitimately report a version the
store hasn't published yet. That's expected, and nobody needs to act on it.

**Store installs** update themselves. The extension detects this by its own
`chrome.runtime.id` (the listing's ID is fixed; an unpacked install's comes from
the pinned key) or an injected `update_url`, and stays quiet — no badge, no
popup notice. Settings → **Version** still shows the installed version and what
the server ships, since being behind is worth being able to look up.

> If the store listing is ever republished under a new ID, update
> `STORE_EXTENSION_ID` in `background.js` to match.

**Manual installs** are never updated by the browser, so the extension compares
its own version against `/status` on popup open and every 6 hours; when the
server is ahead it shows a badge dot plus one dismissible line in the popup
(dismissal lasts until a newer version appears). Those users update by
downloading the zip again, replacing the folder's contents, and hitting reload
on the extension card. Sign-in survives.

## Files

| File | Role |
|---|---|
| `manifest.json` | MV3 manifest. Permissions: `activeTab`, `scripting`, `storage` (page access only for the tab whose button you click); `host_permissions` for `http://localhost/*` (patterns match every port) so the popup can reach the local server. |
| `popup.html` / `popup.js` | Toolbar popup: the action buttons (Open preview / Send to Kindle — each toggleable), live status, and the queue of running/just-finished jobs. Starts jobs; never waits on them. |
| `settings.html` / `settings.js` | The settings page (gear icon): server URL, account (invite redemption / API key / sign-out), popup-button toggles, and the **Recent conversions** history. |
| `background.js` | Service worker. Injects the extractor modules into the page, dispatches to the highest-priority one that matches, then opens the reader or POSTs to the local server. Owns the job records in `chrome.storage.local` that outlive the popup. |
| `extractors/x.js` | Source-specific extractor for X, where the generic pass falls short — reads stable `data-testid` landmarks directly (title, byline, date, cover image, sanitized body). |
| `extractors/generic.js` | Readability fallback for every other page (blogs, news, ...). |
| `reader.html` | The e-ink reader page. No inline scripts (MV3 CSP). |
| `reader.css` | **The single source of truth for e-ink typography.** Used by the reader page and shipped in the Send-to-Kindle payload so the server's PDF renders identically. Tune type here and only here. |
| `reader.js` | Fills the page from the stashed article; the collapsible right-hand panel (device / images / font / send). Sanitizes the extracted markup on the way in — it is page-controlled and this is a privileged page. |
| `fonts.css` / `fonts/` | The bundled webfonts and their `@font-face` rules — six body faces plus JetBrains Mono, all SIL OFL 1.1 with notices included. Nothing is fetched from a font CDN. See `fonts/README.md`. |
| `lib/Readability.js` | Mozilla Readability v0.5.0, vendored unmodified. |
| `icons/` | Tome icon (16/48/128 px, transparent background) — toolbar button, popup header, reader favicon. Source art in `docs/assets/tome-icon.png`. |

## Adding a new source (Medium, Substack, ...)

Extraction is pluggable. Each file in `extractors/` registers itself into a shared
registry in the page's isolated world:

```js
// extractors/medium.js
(function () {
  var registry = (self.__TOME_EXTRACTORS = self.__TOME_EXTRACTORS || {});
  registry["medium"] = {
    priority: 100,                       // > 0 runs before the generic fallback
    matches: function (location) { return /(^|\.)medium\.com$/.test(location.hostname); },
    extract: function (document, location) {
      // return { title, byline, publishedTime, content, url } — or null to
      // pass the page to the next extractor, or { error } to report failure.
    }
  };
})();
```

Then add `"extractors/medium.js"` to `EXTRACTOR_FILES` in `background.js`. Nothing
else changes — the reader, server, PDF/EPUB rendering, and Send-to-Kindle are all
source-agnostic.

## Notes & limitations

- **Fonts load from Google Fonts** the first time; they fall back to Georgia /
  system monospace offline.
- **Grayscale/contrast on images is a CSS preview** (`filter:`), not baked into
  the PDF — true conversion is a Phase 1 server job.
- **No page numbers** — Chrome's print engine ignores CSS `@page` margin boxes.
- After editing any file, return to `arc://extensions` and click the **reload**
  (↻) icon on the Tome card.

## Debugging

- Right-click the toolbar button → **Inspect service worker** for `background.js`
  logs.
- If extraction is wrong, the reader tab shows the error in a yellow banner. To
  debug the extracted DOM offline, save the article page as HTML and I can run
  Readability against it headlessly.
