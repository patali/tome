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

// Reflect server availability; disable "Send to Kindle" when it's down.
function refreshStatus() {
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
      serverText.textContent = "server down · " + (s.url || "").replace(/^https?:\/\//, "");
      serverText.className = "muted";
      kindleBtn.disabled = true;
      kindleBtn.title = "Start the Tome server (or fix the URL in Server settings)";
    }
  });
}
refreshStatus();

/* Server settings: the URL is stored in chrome.storage.sync; non-localhost
   origins need a runtime host permission, requested on Save (a user gesture). */
var urlInput = document.getElementById("server-url");
var saveBtn = document.getElementById("save-server");
var noteEl = document.getElementById("settings-note");

chrome.storage.sync.get({ serverUrl: "http://localhost:8080" }, function (v) {
  urlInput.value = v.serverUrl;
});

saveBtn.addEventListener("click", function () {
  var raw = urlInput.value.trim() || "http://localhost:8080";
  var u;
  try {
    u = new URL(raw);
    if (u.protocol !== "http:" && u.protocol !== "https:") throw new Error();
  } catch (e) {
    noteEl.textContent = "Enter a valid http(s) URL.";
    noteEl.className = "err";
    return;
  }
  var normalized = u.origin + u.pathname.replace(/\/+$/, "");
  chrome.permissions.request({ origins: [u.origin + "/*"] }, function (granted) {
    if (!granted) {
      noteEl.textContent = "Permission for " + u.origin + " was declined.";
      noteEl.className = "err";
      return;
    }
    chrome.storage.sync.set({ serverUrl: normalized }, function () {
      urlInput.value = normalized;
      noteEl.textContent = "Saved.";
      noteEl.className = "ok";
      refreshStatus();
    });
  });
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
  setStatus("Converting and sending…", "muted");
  kindleBtn.disabled = true;
  var r = await send({ type: "convert", mode: "kindle" });
  kindleBtn.disabled = false;
  if (r.error) { setStatus(r.error, "err"); return; }
  setStatus("Sent to " + (r.sentTo || "Kindle") + " ✓", "ok");
});
