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

function setDevice(name) {
  var d = TOME_DEVICES[name] || TOME_DEVICES.scribe;
  document.documentElement.dataset.device = name;
  var st = document.getElementById("tome-page-style");
  if (st) st.textContent = "@page { size: " + d.size + "; margin: " + d.margin + "; }";
  var btns = document.querySelectorAll("#tome-toolbar button[data-device]");
  for (var i = 0; i < btns.length; i++) {
    btns[i].setAttribute("aria-pressed", btns[i].dataset.device === name ? "true" : "false");
  }
}

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

function render(article) {
  var status = document.getElementById("tome-status");
  if (!article || article.error) {
    status.textContent = "tome: " + ((article && article.error) ||
      "Nothing was extracted. Open an X article, scroll through it once, then click the extension button.");
    return;
  }
  document.getElementById("tome-title").textContent = article.title || "Untitled";
  document.getElementById("tome-meta").innerHTML = buildMeta(article);
  document.getElementById("tome-content").innerHTML = article.content; // Readability strips <script>
  document.title = article.title || "tome";
  status.textContent = "";
}

// --- toolbar wiring ---
document.getElementById("tome-toolbar").addEventListener("click", function (e) {
  var b = e.target.closest("button[data-device]");
  if (b) setDevice(b.dataset.device);
});
document.getElementById("tome-print").addEventListener("click", function () { window.print(); });

// --- load the stashed article ---
setDevice("scribe");
if (typeof chrome !== "undefined" && chrome.storage && chrome.storage.session) {
  chrome.storage.session.get("article", function (data) {
    render(data && data.article);
  });
} else {
  render({ error: "This page must be opened by the extension." });
}
