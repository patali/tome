# Tome — Browser Extension (Arc / Chrome, MV3)

Click the toolbar button on an article → a clean, e-ink-optimized reader tab
opens (`Cmd+P` → **Save as PDF**), or send it straight to your Kindle via the
[local server](../server/README.md).

## Why an extension (and not a bookmarklet)

The extension injects its extractors in an **isolated content-script world**,
which is not subject to the page's Content-Security-Policy. So there's nothing
to inline or work around — it just runs. (An earlier bookmarklet approach died
on exactly that CSP wall, and Arc has no bookmarks bar anyway.)

> **Invitees:** the easiest path is your server's install page —
> `http://your-server/install` — which serves this extension as a zip with
> step-by-step instructions. The steps below are the from-source equivalent.
> The manifest pins a public key, so every install shares one extension ID.

## Install in Arc (unpacked)

1. Open a new tab and go to **`arc://extensions`**
   (Chrome users: `chrome://extensions`).
2. Turn on **Developer mode** (top-right toggle).
3. Click **Load unpacked** and select this **`extension/`** folder.
4. The **Tome** extension appears. Pin it so the button is visible:
   click the puzzle-piece / extensions icon in the toolbar and pin Tome
   (in Arc, the extensions live to the right of the address bar).

## Use

1. Open an X article and **scroll through it once** so images and text load.
2. Click the **Tome** toolbar button — the popup shows up to two actions
   (each can be hidden from the settings page, gear icon **⚙**):
   - **Open preview** → clean reader tab; pick your device in the top-right
     toolbar, then `Cmd+P` → **Save as PDF**. Works without the server.
   - **Send to Kindle** → the server renders a PDF and Resend emails it
     straight to your Kindle address.

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
and button toggles sync via `chrome.storage.sync`. Non-localhost server origins
trigger a one-time browser permission prompt (`optional_host_permissions`).

## Files

| File | Role |
|---|---|
| `manifest.json` | MV3 manifest. Permissions: `activeTab`, `scripting`, `storage` (page access only for the tab whose button you click); `host_permissions` for `http://localhost/*` (patterns match every port) so the popup can reach the local server. |
| `popup.html` / `popup.js` | Toolbar popup: the action buttons (Open preview / Send to Kindle — each toggleable), live status, and the queue of running/just-finished jobs. Starts jobs; never waits on them. |
| `settings.html` / `settings.js` | The settings page (gear icon): server URL, account (invite redemption / API key / sign-out), popup-button toggles, and the **Recent conversions** history. |
| `background.js` | Service worker. Injects the extractor modules into the page, dispatches to the highest-priority one that matches, then opens the reader or POSTs to the local server. Owns the job records in `chrome.storage.local` that outlive the popup. |
| `extractors/x.js` | **X (Twitter) article extractor** — reads X's stable `data-testid` landmarks directly (title, byline, date, cover image, sanitized body). |
| `extractors/generic.js` | Readability fallback for every other page (blogs, news, ...). |
| `reader.html` | The e-ink reader page. No inline scripts (MV3 CSP). |
| `reader.css` | **The single source of truth for e-ink typography.** Used by the reader page and shipped in the Send-to-Kindle payload so the server's PDF renders identically. Tune type here and only here. |
| `reader.js` | Fills the page from the stashed article; device + print toolbar. |
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
