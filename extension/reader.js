/*
 * reader.js — fills the reader page from the article stashed by background.js,
 * and drives the device / print toolbar. Runs as an extension page script
 * ('self'), so no inline handlers (blocked by MV3 CSP).
 */

var TOME_DEVICES = {
  scribe:     { size: "157mm 210mm", margin: "10mm 10mm 10mm 10mm" }, // 10.2" 1860x2480 @300ppi
  scribe3:    { size: "168mm 224mm", margin: "12mm 12mm 12mm 12mm" },
  paperwhite: { size: "105mm 140mm", margin: "8mm 8mm 8mm 8mm" }
};

function setPressed(selector, attr, value) {
  var btns = document.querySelectorAll(selector);
  for (var i = 0; i < btns.length; i++) {
    btns[i].setAttribute("aria-pressed", btns[i].dataset[attr] === value ? "true" : "false");
  }
}

function setDevice(name) {
  var d = TOME_DEVICES[name] || TOME_DEVICES.scribe;
  document.documentElement.dataset.device = name;
  var st = document.getElementById("tome-page-style");
  if (st) st.textContent = "@page { size: " + d.size + "; margin: " + d.margin + "; }";
  setPressed("#tome-toolbar button[data-device]", "device", name);
}

// Grayscale is the default (see reader.css); this just flips the data
// attribute the stylesheet keys off, so preview and PDF agree.
function setColor(mode) {
  var m = mode === "color" ? "color" : "bw";
  document.documentElement.dataset.color = m;
  setPressed("#tome-toolbar button[data-color]", "color", m);
}

function currentDevice() { return document.documentElement.dataset.device || "scribe"; }
function currentColor() { return document.documentElement.dataset.color || "bw"; }

function esc(s) {
  return String(s == null ? "" : s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function buildMeta(a) {
  var parts = [];
  if (a.byline) parts.push(esc(a.byline));
  if (a.publishedTime) {
    var w = new Date(a.publishedTime);
    if (!isNaN(w)) parts.push("Published " + w.toLocaleDateString(undefined,
      { year: "numeric", month: "short", day: "numeric" }));
  }
  if (a.url) {
    parts.push('Source: <a href="' + esc(a.url) + '">' +
      esc(a.url.replace(/^https?:\/\//, "")) + "</a>");
  }
  return parts.join("&nbsp;·&nbsp;");
}

// What this tab is showing — used to label the send job and to disable the
// send button when there's nothing to send.
var shown = null;

function render(article) {
  var status = document.getElementById("tome-status");
  if (!article || article.error) {
    status.textContent = "tome: " + ((article && article.error) ||
      "Nothing was extracted. Open an X article, scroll through it once, then click the extension button.");
    var sb = document.getElementById("tome-send");
    if (sb) sb.disabled = true;
    return;
  }
  shown = article;
  document.getElementById("tome-title").textContent = article.title || "Untitled";
  document.getElementById("tome-meta").innerHTML = buildMeta(article);
  document.getElementById("tome-content").innerHTML = article.content; // Readability strips <script>
  document.title = article.title || "tome";
  status.textContent = "";
}

// --- toolbar wiring ---
document.getElementById("tome-toolbar").addEventListener("click", function (e) {
  var dev = e.target.closest("button[data-device]");
  if (dev) { setDevice(dev.dataset.device); return; }
  var col = e.target.closest("button[data-color]");
  if (col) setColor(col.dataset.color);
});
document.getElementById("tome-print").addEventListener("click", function () { window.print(); });

/* --- Send to Kindle ------------------------------------------------------
 *
 * Goes through the same background job queue as the popup, so the send shows
 * up in the popup and in the settings history, and survives this tab being
 * closed. The article is already stashed; we only add the device and colour
 * the user picked here, so what ships matches what they're looking at.
 */

var sendBtn = document.getElementById("tome-send");
var sendStatus = document.getElementById("tome-send-status");
var sendJobId = "";

function showSendStatus(text, cls) {
  sendStatus.textContent = text;
  sendStatus.className = cls || "";
  sendStatus.hidden = !text;
}

if (sendBtn) {
  sendBtn.addEventListener("click", function () {
    sendBtn.disabled = true;
    showSendStatus("Sending…", "");
    chrome.runtime.sendMessage(
      {
        type: "convert", mode: "kindle", source: "stash",
        device: currentDevice(), color: currentColor(),
        title: (shown && shown.title) || "", url: (shown && shown.url) || ""
      },
      function (r) {
        if (chrome.runtime.lastError) {
          sendBtn.disabled = false;
          showSendStatus(chrome.runtime.lastError.message, "err");
          return;
        }
        if (!r || r.error) {
          sendBtn.disabled = false;
          showSendStatus((r && r.error) || "Couldn't start the send.", "err");
          return;
        }
        sendJobId = r.id;
      });
  });

  // The job record is the source of truth for how it ended — same as the popup.
  chrome.storage.onChanged.addListener(function (changes, area) {
    if (area !== "local" || !changes.jobs || !sendJobId) return;
    var job = (changes.jobs.newValue || []).find(function (j) { return j.id === sendJobId; });
    if (!job || job.state === "running") return;
    sendBtn.disabled = false;
    showSendStatus(job.message, job.state === "done" ? "ok" : "err");
  });
}

// --- load the stashed article ---
setDevice("scribe");
setColor("bw");
if (typeof chrome !== "undefined" && chrome.storage && chrome.storage.session) {
  chrome.storage.session.get("article", function (data) {
    render(data && data.article);
  });
} else {
  render({ error: "This page must be opened by the extension." });
}
