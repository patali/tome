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

// The API key lives in storage.local (not sync): secrets shouldn't roam
// between profiles via the browser's sync service.
async function apiKey() {
  try {
    const { apiKey } = await chrome.storage.local.get({ apiKey: "" });
    return apiKey || "";
  } catch (e) {
    return "";
  }
}

async function authHeaders() {
  const key = await apiKey();
  return key ? { "Authorization": "Bearer " + key } : {};
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

// deliver POSTs the article to a send endpoint: "/send-to-kindle" (Resend ->
// the user's Kindle) or "/send-via-mail" (opens Mail.app on the server host).
async function deliver(article, path) {
  article.css = await readerCSS();
  const resp = await fetch((await serverUrl()) + path, {
    method: "POST",
    headers: Object.assign({ "Content-Type": "application/json" }, await authHeaders()),
    body: JSON.stringify(article)
  });
  if (resp.status === 401) throw new Error("Not signed in — open Tome settings (⚙).");
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(data.error || ("server returned " + resp.status));
  return data; // { ok, method, sentTo, filename, bytes }
}

// Two-step status: is the server up, and does our stored key identify us?
async function serverStatus() {
  const url = await serverUrl();
  let status;
  try {
    const resp = await fetch(url + "/status", { method: "GET" });
    if (!resp.ok) return { up: false, url };
    status = await resp.json();
  } catch (e) {
    return { up: false, url };
  }
  const out = { up: true, url, authRequired: !!status.authRequired, signedIn: false, badKey: false };
  if (!(await apiKey())) return out;
  try {
    const resp = await fetch(url + "/me", { headers: await authHeaders() });
    if (resp.status === 401) { out.badKey = true; return out; }
    if (!resp.ok) return out;
    const me = await resp.json();
    out.signedIn = true;
    out.email = me.email;
    out.kindleEmail = me.kindleEmail;
    out.resendConfigured = !!me.resendConfigured;
    out.mailApp = !!me.mailApp;
  } catch (e) { /* leave signedIn false */ }
  return out;
}

// Redeems an invite (or validates a pasted key) and stores the API key.
async function acceptInvite(code, email, kindleEmail) {
  const resp = await fetch((await serverUrl()) + "/auth/accept-invite", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ code, email, kindleEmail })
  });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(data.error || ("server returned " + resp.status));
  await chrome.storage.local.set({ apiKey: data.apiKey });
  return { ok: true, email: data.email, approvedSender: data.approvedSender };
}

async function updateKindleEmail(kindleEmail) {
  const resp = await fetch((await serverUrl()) + "/me", {
    method: "PUT",
    headers: Object.assign({ "Content-Type": "application/json" }, await authHeaders()),
    body: JSON.stringify({ kindleEmail })
  });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error(data.error || ("server returned " + resp.status));
  return { ok: true, kindleEmail: data.kindleEmail };
}

async function setApiKey(key) {
  const resp = await fetch((await serverUrl()) + "/me", {
    headers: { "Authorization": "Bearer " + key }
  });
  if (resp.status === 401) throw new Error("That API key was rejected by the server.");
  if (!resp.ok) throw new Error("server returned " + resp.status);
  const me = await resp.json();
  await chrome.storage.local.set({ apiKey: key });
  return { ok: true, email: me.email };
}

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  (async () => {
    try {
      if (msg.type === "ping") {
        sendResponse(await serverStatus());
        return;
      }
      if (msg.type === "acceptInvite") {
        sendResponse(await acceptInvite(msg.code, msg.email, msg.kindleEmail));
        return;
      }
      if (msg.type === "setApiKey") {
        sendResponse(await setApiKey(msg.apiKey));
        return;
      }
      if (msg.type === "updateKindle") {
        sendResponse(await updateKindleEmail(msg.kindleEmail));
        return;
      }
      if (msg.type === "signOut") {
        await chrome.storage.local.remove("apiKey");
        sendResponse({ ok: true });
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
          const result = await deliver(article, "/send-to-kindle");
          sendResponse({ ok: true, mode: "kindle", sentTo: result.sentTo });
        } else if (msg.mode === "mail") {
          const result = await deliver(article, "/send-via-mail");
          sendResponse({ ok: true, mode: "mail", sentTo: result.sentTo });
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
