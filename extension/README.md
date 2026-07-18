# Tome — Browser Extension (Arc / Chrome, MV3)

Click the toolbar button on an article → a clean, e-ink-optimized reader tab
opens (`Cmd+P` → **Save as PDF**), or send it straight to your Kindle via the
[local server](../server/README.md).

## Why an extension (and not a bookmarklet)

The extension injects its extractors in an **isolated content-script world**,
which is not subject to the page's Content-Security-Policy. So there's nothing
to inline or work around — it just runs. (An earlier bookmarklet approach died
on exactly that CSP wall, and Arc has no bookmarks bar anyway.)

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
2. Click the **Tome** toolbar button — a small popup opens with two actions:
   - **Open reader tab** → clean reader page; pick your device in the top-right
     toolbar, then `Cmd+P` → **Save as PDF**.
   - **Send to Kindle** → builds a PDF (or EPUB fallback) on the local server and
     delivers it to your Kindle. Requires the [server](../server/README.md) running;
     the popup shows a live server-status dot and disables this button when it can't.

> In the print dialog: **Margins: Default** and enable **Background graphics**
> so code-block shading prints.

### Send to Kindle setup

1. Start the local server: `cd server && go run ./cmd/tome/`.
2. Set the `TOME_*` env vars (see `server/README.md`) and restart it.
3. The popup's status line should read **server up · Kindle configured**.

### Server settings (custom server URL)

The server address defaults to `http://localhost:8080`. To use a server running
elsewhere (a container, home server, NAS): popup → **Server settings** → enter
the URL → **Save**. Non-localhost origins trigger a one-time browser permission
prompt (`optional_host_permissions`); the setting syncs via `chrome.storage.sync`.

## Files

| File | Role |
|---|---|
| `manifest.json` | MV3 manifest. Permissions: `activeTab`, `scripting`, `storage` (page access only for the tab whose button you click); `host_permissions` for `http://localhost/*` (patterns match every port) so the popup can reach the local server. |
| `popup.html` / `popup.js` | Toolbar popup: **Open reader tab** / **Send to Kindle**, plus a live server-status indicator. |
| `background.js` | Service worker. Injects the extractor modules into the page, dispatches to the highest-priority one that matches, then opens the reader or POSTs to the local server. |
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
