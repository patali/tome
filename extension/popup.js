/* popup.js — drives the two actions and shows local-server status. */

var readerBtn = document.getElementById("reader");
var kindleBtn = document.getElementById("kindle");
var statusEl = document.getElementById("status");
var dotEl = document.getElementById("dot");
var serverText = document.getElementById("server-text");

function setStatus(text, cls) {
  statusEl.textContent = text;
  statusEl.className = cls || "";
}

function send(msg) {
  return new Promise(function (resolve) {
    chrome.runtime.sendMessage(msg, function (resp) {
      if (chrome.runtime.lastError) { resolve({ error: chrome.runtime.lastError.message }); return; }
      resolve(resp || {});
    });
  });
}

// Reflect local-server availability; disable "Send to Kindle" when it's down.
send({ type: "ping" }).then(function (s) {
  if (s.up) {
    dotEl.className = "dot up";
    var via = s.method === "smtp" ? "sends via SMTP"
            : s.method === "mail-app" ? "opens Mail.app"
            : "Kindle not configured";
    serverText.textContent = "server up · " + via;
    serverText.className = "";
    kindleBtn.disabled = !s.kindleConfigured;
    if (s.kindleConfigured) {
      kindleBtn.textContent = s.method === "mail-app" ? "Send to Kindle (via Mail)" : "Send to Kindle";
      kindleBtn.title = "→ " + (s.kindleEmail || "");
    } else {
      kindleBtn.title = "Set TOME_* env vars and restart the server";
    }
  } else {
    dotEl.className = "dot down";
    serverText.textContent = "server down — start it in server/";
    serverText.className = "muted";
    kindleBtn.disabled = true;
    kindleBtn.title = "Run the local Go server to enable this";
  }
});

readerBtn.addEventListener("click", async function () {
  setStatus("Extracting…", "muted");
  readerBtn.disabled = true;
  var r = await send({ type: "convert", mode: "reader" });
  readerBtn.disabled = false;
  if (r.error) { setStatus(r.error, "err"); return; }
  setStatus("Reader opened.", "ok");
  window.close();
});

kindleBtn.addEventListener("click", async function () {
  setStatus("Building EPUB and sending…", "muted");
  kindleBtn.disabled = true;
  var r = await send({ type: "convert", mode: "kindle" });
  kindleBtn.disabled = false;
  if (r.error) { setStatus(r.error, "err"); return; }
  setStatus("Sent to " + (r.sentTo || "Kindle") + " ✓", "ok");
});
