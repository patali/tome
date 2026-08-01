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

/* --- Sanitizing extracted markup -----------------------------------------
 *
 * The article HTML comes from whatever page the user was on, and this is a
 * privileged extension page. MV3's default CSP already blocks inline handlers
 * and remote script, so this is defence in depth rather than the only thing
 * standing in the way — but the cost is one DOM pass, and it means a hostile
 * page can't rely on a CSP mistake later.
 *
 * Allow-list, not block-list: anything not named here is unwrapped (children
 * kept) or dropped, so a tag nobody thought about fails closed.
 */

var ALLOWED_TAGS = {
  P: 1, BR: 1, HR: 1, DIV: 1, SPAN: 1, SECTION: 1, ARTICLE: 1,
  H1: 1, H2: 1, H3: 1, H4: 1, H5: 1, H6: 1,
  UL: 1, OL: 1, LI: 1, DL: 1, DT: 1, DD: 1,
  BLOCKQUOTE: 1, PRE: 1, CODE: 1, KBD: 1, SAMP: 1,
  EM: 1, STRONG: 1, I: 1, B: 1, U: 1, S: 1, SUB: 1, SUP: 1, SMALL: 1, MARK: 1,
  A: 1, IMG: 1, FIGURE: 1, FIGCAPTION: 1, PICTURE: 1, SOURCE: 1,
  TABLE: 1, THEAD: 1, TBODY: 1, TFOOT: 1, TR: 1, TD: 1, TH: 1, CAPTION: 1,
  TIME: 1, ABBR: 1, CITE: 1, Q: 1
};

// Dropped entirely, contents included — these carry no reading value and are
// the ones with script-like or navigational behaviour.
var DROP_TAGS = {
  SCRIPT: 1, STYLE: 1, IFRAME: 1, OBJECT: 1, EMBED: 1, APPLET: 1, LINK: 1,
  META: 1, BASE: 1, FORM: 1, INPUT: 1, BUTTON: 1, SELECT: 1, TEXTAREA: 1,
  NOSCRIPT: 1, TEMPLATE: 1, SVG: 1, MATH: 1, CANVAS: 1, AUDIO: 1, VIDEO: 1
};

var ALLOWED_ATTRS = {
  A: ["href", "title"],
  IMG: ["src", "alt", "title", "width", "height"],
  SOURCE: ["srcset", "type"],
  TD: ["colspan", "rowspan"],
  TH: ["colspan", "rowspan", "scope"],
  TIME: ["datetime"],
  ABBR: ["title"],
  Q: ["cite"]
};

// Only these schemes may appear in a src/href. Notably excludes javascript:
// and data: (a data: URL in an <a> can carry markup that runs on click).
function safeURL(value, allowData) {
  var v = String(value || "").trim();
  // Strip whitespace and control characters before testing the scheme —
  // browsers parse "java\tscript:alert(1)" as javascript:.
  var probe = v.replace(/[\u0000-\u0020]/g, "").toLowerCase();
  if (probe.indexOf("javascript:") === 0 || probe.indexOf("vbscript:") === 0) return "";
  if (probe.indexOf("data:") === 0) {
    // Images may legitimately be inline data; links may not.
    return allowData && /^data:image\/(png|jpe?g|gif|webp|avif|svg\+xml);/.test(probe) ? v : "";
  }
  return v;
}

function sanitizeInto(root) {
  var walker = root.querySelectorAll("*");
  for (var i = walker.length - 1; i >= 0; i--) {
    var el = walker[i];
    var tag = el.tagName;

    if (DROP_TAGS[tag]) { el.remove(); continue; }
    if (!ALLOWED_TAGS[tag]) {
      // Unknown but harmless-looking: keep the text, lose the element.
      el.replaceWith.apply(el, Array.prototype.slice.call(el.childNodes));
      continue;
    }

    var allowed = ALLOWED_ATTRS[tag] || [];
    var attrs = Array.prototype.slice.call(el.attributes);
    for (var j = 0; j < attrs.length; j++) {
      var name = attrs[j].name;
      var lower = name.toLowerCase();
      if (allowed.indexOf(lower) === -1) { el.removeAttribute(name); continue; }
      if (lower === "href" || lower === "src" || lower === "cite") {
        var cleaned = safeURL(attrs[j].value, lower === "src");
        if (cleaned) el.setAttribute(name, cleaned); else el.removeAttribute(name);
      }
    }
    // Links leave the reader; make them safe to click and un-exploitable.
    if (tag === "A" && el.getAttribute("href")) {
      el.setAttribute("target", "_blank");
      el.setAttribute("rel", "noopener noreferrer");
    }
  }
  return root;
}

// Parses in an inert document, so nothing loads or runs before it's cleaned.
function sanitizedFragment(html) {
  var doc = new DOMParser().parseFromString(String(html || ""), "text/html");
  return sanitizeInto(doc.body);
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
    // esc() stops attribute-breakout but not a javascript: scheme, and this
    // link is rendered into a privileged page — so check the scheme too.
    var href = safeURL(a.url, false);
    var label = esc(a.url.replace(/^https?:\/\//, ""));
    parts.push("Source: " + (href ? '<a href="' + esc(href) + '">' + label + "</a>" : label));
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
      "Nothing was extracted. Open an article, scroll through it once so images load, then click the extension button.");
    var sb = document.getElementById("tome-send");
    if (sb) sb.disabled = true;
    return;
  }
  shown = article;
  document.getElementById("tome-title").textContent = article.title || "Untitled";
  document.getElementById("tome-meta").innerHTML = buildMeta(article);
  // Extracted markup is page-controlled and this is a privileged page, so it
  // goes through the allow-list rather than straight into innerHTML.
  var content = document.getElementById("tome-content");
  content.textContent = "";
  var clean = sanitizedFragment(article.content);
  while (clean.firstChild) content.appendChild(document.adoptNode(clean.firstChild));
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
