/*
 * background.js — tome service worker (MV3)
 *
 * Driven by the popup. On request it injects the extractor modules into the
 * active tab (isolated world, so the page's CSP doesn't apply), runs whichever
 * extractor claims the page, then either:
 *   - "reader": opens the packaged reader page for Cmd+P -> Save as PDF, or
 *   - "kindle": POSTs the article to the local Go server, which renders a
 *               PDF/EPUB and delivers it via Send-to-Kindle.
 *
 * Extraction is pluggable: each file in extractors/ registers itself into
 * self.__TOME_EXTRACTORS (keyed by name, so re-injection is idempotent). To
 * support a new source (Medium, Substack, ...), add extractors/<source>.js
 * with { priority, matches(location), extract(document, location) } and list
 * it in EXTRACTOR_FILES below — nothing else changes.
 */

const DEFAULT_SERVER = "http://localhost:8080";

// The server address is configurable (popup -> Server settings) so the Go
// server can run anywhere: localhost, a container, or another machine.
async function serverUrl() {
  try {
    const { serverUrl } = await chrome.storage.sync.get({ serverUrl: DEFAULT_SERVER });
    return (serverUrl || DEFAULT_SERVER).replace(/\/+$/, "");
  } catch (e) {
    return DEFAULT_SERVER;
  }
}

// Injection order: libraries first, then extractors (any order — priority
// decides who runs first), dispatcher last via func.
const EXTRACTOR_FILES = [
  "lib/Readability.js",
  "extractors/x.js",
  "extractors/generic.js"
];

// Runs IN the page's isolated world, after EXTRACTOR_FILES are injected.
// Picks the highest-priority extractor that matches; an extractor returning
// null passes the page to the next one.
function runExtractors() {
  try {
    var registry = self.__TOME_EXTRACTORS || {};
    var list = Object.keys(registry).map(function (name) {
      var ex = registry[name];
      ex.name = name;
      return ex;
    }).sort(function (a, b) { return (b.priority || 0) - (a.priority || 0); });

    var lastError = "";
    for (var i = 0; i < list.length; i++) {
      var ex = list[i];
      try {
        if (!ex.matches(location)) continue;
        var result = ex.extract(document, location);
        if (result && result.error) { lastError = result.error; continue; }
        if (result && result.content) { result.extractor = ex.name; return result; }
      } catch (e) {
        lastError = ex.name + ": " + String((e && e.message) || e);
      }
    }
    return { error: lastError || "No extractor could handle this page." };
  } catch (e) {
    return { error: String((e && e.message) || e) };
  }
}

// Inject the extractor modules, then dispatch. Returns the article or {error}.
async function extractFromTab(tab) {
  if (!tab || !tab.id) return { error: "No active tab." };
  try {
    await chrome.scripting.executeScript({ target: { tabId: tab.id }, files: EXTRACTOR_FILES });
    const [{ result }] = await chrome.scripting.executeScript({ target: { tabId: tab.id }, func: runExtractors });
    return result || { error: "Extraction returned nothing." };
  } catch (e) {
    return { error: "Couldn't run on this page: " + String((e && e.message) || e) };
  }
}

async function activeTab() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  return tab;
}

async function openReader(article) {
  await chrome.storage.session.set({ article });
  await chrome.tabs.create({ url: chrome.runtime.getURL("reader.html") });
}

// The shared e-ink stylesheet (extension/reader.css) is the single source of
// truth; ship it with the payload so the server's PDF renders with exactly the
// CSS the reader tab shows. The server keeps a fallback for bare-curl clients.
async function readerCSS() {
  try {
    return await (await fetch(chrome.runtime.getURL("reader.css"))).text();
  } catch (e) {
    return ""; // server falls back to its embedded default
  }
}

async function sendToKindle(article) {
  article.css = await readerCSS();
  const resp = await fetch((await serverUrl()) + "/send-to-kindle", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(article)
  });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(data.error || ("server returned " + resp.status));
  return data; // { ok, sentTo, filename, bytes }
}

async function serverStatus() {
  const url = await serverUrl();
  try {
    const resp = await fetch(url + "/status", { method: "GET" });
    if (!resp.ok) return { up: false, url };
    const data = await resp.json();
    return { up: true, url, kindleConfigured: !!data.kindleConfigured, method: data.method, kindleEmail: data.kindleEmail };
  } catch (e) {
    return { up: false, url };
  }
}

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  (async () => {
    try {
      if (msg.type === "ping") {
        sendResponse(await serverStatus());
        return;
      }
      if (msg.type === "convert") {
        const article = await extractFromTab(await activeTab());
        if (article.error) { sendResponse({ error: article.error }); return; }
        console.log("[tome] extracted via:", article.extractor, "->", article.title);

        if (msg.mode === "reader") {
          await openReader(article);
          sendResponse({ ok: true, mode: "reader" });
        } else if (msg.mode === "kindle") {
          const result = await sendToKindle(article);
          sendResponse({ ok: true, mode: "kindle", sentTo: result.sentTo });
        } else {
          sendResponse({ error: "Unknown mode: " + msg.mode });
        }
        return;
      }
      sendResponse({ error: "Unknown message type." });
    } catch (e) {
      sendResponse({ error: String((e && e.message) || e) });
    }
  })();
  return true; // keep the message channel open for the async response
});
