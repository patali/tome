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
 * Work is tracked as jobs in storage rather than returned through the message
 * channel. A popup closes the moment the user clicks the page behind it, which
 * kills that channel mid-conversion; the job record is what survives, so the
 * popup can show progress again on reopen.
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

// The article the reader page is currently showing. Session storage is
// cleared when the browser closes, so a stale reader tab reopened later has
// nothing to send — say so rather than silently sending the wrong thing.
async function stashedArticle() {
  const { article } = await chrome.storage.session.get({ article: null });
  if (!article || !article.content) {
    return { error: "This preview is no longer loaded — reopen it from the Tome button." };
  }
  return article;
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

// readingFont is the body face chosen in settings. It rides along with every
// conversion so the document matches what the preview showed.
async function readingFont() {
  try {
    const { readingFont } = await chrome.storage.sync.get({ readingFont: "literata" });
    return readingFont || "literata";
  } catch (e) {
    return "literata";
  }
}

// deliver POSTs the article to /send-to-kindle (Resend -> the user's Kindle).
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

/* --- Update check -------------------------------------------------------
 *
 * Only manual ("Load unpacked") installs need this. The browser never updates
 * one, so the server reports the version of the extension it hands out at
 * /status; if that is newer than ours, we raise a badge and the popup explains
 * how to update.
 *
 * A store install updates itself, so there is nothing to tell its user and no
 * action they could take — the badge and the popup notice stay off. Settings →
 * Version still shows both numbers, because being ahead or behind the server is
 * worth being able to look up.
 *
 * Deliberately quiet: a badge dot and one line in the popup, no notifications
 * and no interruption of whatever the user was doing.
 */

const UPDATE_ALARM = "tome-update-check";
const UPDATE_PERIOD_MIN = 360; // 6h — a manual-install update is never urgent

// The published listing. An unpacked install derives its ID from the pinned
// manifest key instead, so the two never collide.
const STORE_EXTENSION_ID = "mfnoejpbojcndlepcbkidppdinbbohmi";

// Which install this is. chrome.management.getSelf() would answer directly, but
// only the "management" permission makes that API available for certain — and
// declaring it would put Tome back in the Chrome Web Store's in-depth review
// queue, the same cost that dropping the host permissions was avoiding. Both
// checks below need no permission and no API beyond runtime:
//
//   - our own listing has a known, fixed ID;
//   - any other store install (a fork's listing) carries the update_url Chrome
//     injects into a store manifest.
//
// Anything else is loaded from a folder — this repo's extension/, or the zip
// the server hands out — and nothing will ever update it.
function isUnpacked() {
  if (chrome.runtime.id === STORE_EXTENSION_ID) return false;
  return !chrome.runtime.getManifest().update_url;
}

function installedVersion() {
  try {
    return chrome.runtime.getManifest().version || "";
  } catch (e) {
    return "";
  }
}

// Compares dotted-integer versions (Chrome's manifest format: 1-4 numbers, no
// pre-release suffixes). Returns 1 if a > b, -1 if a < b, 0 if equal/unknown.
function compareVersions(a, b) {
  const pa = String(a || "").split(".");
  const pb = String(b || "").split(".");
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = parseInt(pa[i], 10) || 0;
    const y = parseInt(pb[i], 10) || 0;
    if (x > y) return 1;
    if (x < y) return -1;
  }
  return 0;
}

async function updateState() {
  const { latestVersion, dismissedVersion, lastCheckedAt } = await chrome.storage.local.get({
    latestVersion: "", dismissedVersion: "", lastCheckedAt: 0
  });
  const installed = installedVersion();
  const available = !!latestVersion && compareVersions(latestVersion, installed) > 0;
  const unpacked = isUnpacked();
  return {
    installed,
    latestVersion,
    lastCheckedAt,
    unpacked,
    updateAvailable: available,
    // Dismissal is per-version: a newer release surfaces again. Store installs
    // never surface it at all — Chrome is already handling it.
    showUpdate: available && unpacked && compareVersions(latestVersion, dismissedVersion) > 0
  };
}

async function refreshBadge() {
  const st = await updateState();
  try {
    await chrome.action.setBadgeText({ text: st.showUpdate ? "•" : "" });
    if (st.showUpdate) {
      await chrome.action.setBadgeBackgroundColor({ color: "#1a9e4b" });
      await chrome.action.setTitle({ title: "Tome — version " + st.latestVersion + " is available" });
    } else {
      await chrome.action.setTitle({ title: "Convert this article for Kindle" });
    }
  } catch (e) { /* action API unavailable (e.g. during teardown) */ }
}

// Asks the server which extension version it ships. Failure is silent: the
// server being unreachable is already reported by the status dot, and an
// update check is not worth a second error message.
async function checkForUpdate() {
  try {
    const resp = await fetch((await serverUrl()) + "/status");
    if (!resp.ok) return;
    const status = await resp.json();
    if (status && typeof status.extensionVersion === "string" && status.extensionVersion) {
      await chrome.storage.local.set({
        latestVersion: status.extensionVersion, lastCheckedAt: Date.now()
      });
    }
  } catch (e) { /* offline or misconfigured server */ }
  await refreshBadge();
}

// Only create the alarm if it isn't already scheduled. This module re-runs
// every time the service worker wakes, and create() on an existing name
// restarts its countdown — recreating unconditionally means a worker that
// wakes more often than every 6h resets the timer forever and it never fires.
chrome.alarms.get(UPDATE_ALARM).then((existing) => {
  if (!existing) chrome.alarms.create(UPDATE_ALARM, { periodInMinutes: UPDATE_PERIOD_MIN });
}).catch(() => {});

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === UPDATE_ALARM) checkForUpdate();
});
chrome.runtime.onInstalled.addListener(() => {
  // A fresh install of a newer build should clear a stale "update available".
  chrome.storage.local.remove("dismissedVersion").then(checkForUpdate, checkForUpdate);
});
chrome.runtime.onStartup.addListener(checkForUpdate);
refreshBadge();

/* --- Job queue ---------------------------------------------------------
 *
 * Jobs live in storage.local so they outlive both the popup and this service
 * worker. The popup renders from here; nothing it needs arrives by message
 * response, because that response is lost if the popup closes first.
 *
 * "seen" drives the popup only: a finished job is shown until the popup has
 * displayed it once, then drops out. Settings keeps the full list as history.
 */

const JOBS_KEY = "jobs";
const MAX_JOBS = 50;

// All mutations go through one promise chain. Read-modify-write on a single
// storage key would otherwise interleave between concurrent jobs and lose an
// update — two conversions running at once is the normal case here.
let jobsChain = Promise.resolve();

function withJobs(fn) {
  const next = jobsChain.then(async () => {
    const { [JOBS_KEY]: stored } = await chrome.storage.local.get({ [JOBS_KEY]: [] });
    const jobs = Array.isArray(stored) ? stored : [];
    const result = await fn(jobs);
    await chrome.storage.local.set({ [JOBS_KEY]: jobs.slice(0, MAX_JOBS) });
    return result;
  });
  jobsChain = next.then(() => {}, () => {});
  return next;
}

function newJobId() {
  return Date.now().toString(36) + "-" + Math.random().toString(36).slice(2, 8);
}

const MODE_LABEL = { reader: "Preview", kindle: "Send to Kindle" };

async function addJob(mode, tab, opts) {
  const o = opts || {};
  const job = {
    id: newJobId(),
    mode,
    label: MODE_LABEL[mode] || mode,
    // "stash" means the reader page already holds the extracted article, so
    // there is nothing to pull out of the active tab (which is the reader).
    source: o.source === "stash" ? "stash" : "tab",
    device: o.device || "",
    color: o.color || "",
    font: o.font || "",
    title: o.title || (tab && tab.title) || "This page",
    url: o.url || (tab && tab.url) || "",
    state: "running",
    message: mode === "reader" ? "Extracting…" : "Converting…",
    startedAt: Date.now(),
    endedAt: 0,
    seen: false
  };
  await withJobs((jobs) => { jobs.unshift(job); });
  return job;
}

async function patchJob(id, patch) {
  await withJobs((jobs) => {
    const job = jobs.find((j) => j.id === id);
    if (job) Object.assign(job, patch);
  });
}

// A service worker restart proves nothing is still driving those fetches, so
// anything left "running" in storage died with the previous worker. Without
// this they'd spin in the popup forever.
async function reconcileStaleJobs() {
  await withJobs((jobs) => {
    jobs.forEach((j) => {
      if (j.state !== "running") return;
      j.state = "error";
      j.message = "Interrupted — the browser stopped the extension mid-run. Try again.";
      j.endedAt = Date.now();
      j.seen = false;
    });
  });
}
reconcileStaleJobs();

// runJob owns the whole conversion. It deliberately returns nothing to the
// caller: progress and outcome are written to the job record instead.
async function runJob(job) {
  try {
    const article = job.source === "stash" ? await stashedArticle() : await extractFromTab(await activeTab());
    if (article.error) throw new Error(article.error);
    console.log("[tome] article:", article.extractor || job.source, "->", article.title);
    if (article.title) await patchJob(job.id, { title: article.title });
    // Preview-page choices override whatever the article was stashed with;
    // the body face falls back to the stored setting for popup-driven sends.
    if (job.device) article.device = job.device;
    if (job.color) article.color = job.color;
    article.font = job.font || (await readingFont());

    if (job.mode === "reader") {
      await openReader(article);
      await patchJob(job.id, {
        state: "done", message: "Preview opened.", endedAt: Date.now()
      });
      return;
    }
    if (job.mode === "kindle") {
      await patchJob(job.id, { message: "Rendering and sending…" });
      const result = await deliver(article, "/send-to-kindle");
      await patchJob(job.id, {
        state: "done",
        message: "Sent to " + (result.sentTo || "your Kindle"),
        sentTo: result.sentTo || "",
        endedAt: Date.now()
      });
      return;
    }
    throw new Error("Unknown mode: " + job.mode);
  } catch (e) {
    await patchJob(job.id, {
      state: "error", message: String((e && e.message) || e), endedAt: Date.now()
    });
  }
}

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  (async () => {
    try {
      if (msg.type === "ping") {
        sendResponse(await serverStatus());
        // Opening the popup is the cheapest moment to re-check; not awaited,
        // so it never delays the status the popup is waiting on.
        checkForUpdate();
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
        const tab = msg.source === "stash" ? null : await activeTab();
        const job = await addJob(msg.mode, tab, msg);
        // Not awaited: the popup gets the job id now and follows the rest in
        // storage, so closing it can't strand the conversion.
        runJob(job);
        sendResponse({ ok: true, id: job.id });
        return;
      }
      if (msg.type === "updateState") {
        sendResponse(await updateState());
        return;
      }
      if (msg.type === "checkUpdate") {
        await checkForUpdate();
        sendResponse(await updateState());
        return;
      }
      if (msg.type === "dismissUpdate") {
        const { latestVersion } = await chrome.storage.local.get({ latestVersion: "" });
        await chrome.storage.local.set({ dismissedVersion: latestVersion });
        await refreshBadge();
        sendResponse({ ok: true });
        return;
      }
      if (msg.type === "getJobs") {
        const { jobs } = await chrome.storage.local.get({ [JOBS_KEY]: [] });
        sendResponse({ jobs: Array.isArray(jobs) ? jobs : [] });
        return;
      }
      if (msg.type === "markSeen") {
        const ids = new Set(msg.ids || []);
        await withJobs((jobs) => {
          jobs.forEach((j) => { if (ids.has(j.id)) j.seen = true; });
        });
        sendResponse({ ok: true });
        return;
      }
      if (msg.type === "clearJobs") {
        await withJobs((jobs) => { jobs.length = 0; });
        sendResponse({ ok: true });
        return;
      }
      sendResponse({ error: "Unknown message type." });
    } catch (e) {
      sendResponse({ error: String((e && e.message) || e) });
    }
  })();
  return true; // keep the message channel open for the async response
});
