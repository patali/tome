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

var settingsEl = document.getElementById("settings");
var signedOutEl = document.getElementById("acct-signedout");
var signedInEl = document.getElementById("acct-signedin");
var acctInfoEl = document.getElementById("acct-info");

// Reflect server + account state; "Send to Kindle" needs both.
function refreshStatus() {
  send({ type: "ping" }).then(function (s) {
    signedOutEl.hidden = !(s.up && !s.signedIn);
    signedInEl.hidden = !(s.up && s.signedIn);

    if (!s.up) {
      dotEl.className = "dot down";
      serverText.textContent = "server down · " + (s.url || "").replace(/^https?:\/\//, "");
      serverText.className = "muted";
      kindleBtn.disabled = true;
      kindleBtn.title = "Start the Tome server (or fix the URL in Server settings)";
      return;
    }
    if (!s.signedIn) {
      dotEl.className = "dot";
      serverText.textContent = s.badKey ? "server up · stored key rejected" : "server up · sign-in required";
      serverText.className = "";
      kindleBtn.disabled = true;
      kindleBtn.title = "Redeem an invite or paste your API key in Server settings";
      settingsEl.open = true;
      return;
    }
    dotEl.className = "dot up";
    var via = s.deliveryMethod === "resend" ? "sends via email"
            : s.deliveryMethod === "mail-app" ? "opens Mail.app"
            : "delivery not configured";
    serverText.textContent = s.email + " · " + via;
    serverText.className = "";
    acctInfoEl.textContent = s.email + " → " + (s.kindleEmail || "");
    kindleBtn.disabled = s.deliveryMethod === "none";
    kindleBtn.textContent = s.deliveryMethod === "mail-app" ? "Send to Kindle (via Mail)" : "Send to Kindle";
    kindleBtn.title = kindleBtn.disabled
      ? "Ask the admin to configure Resend delivery"
      : "→ " + (s.kindleEmail || "");
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

function note(text, cls) {
  noteEl.textContent = text;
  noteEl.className = cls || "muted";
}

document.getElementById("redeem").addEventListener("click", async function () {
  var code = document.getElementById("invite-code").value.trim();
  var email = document.getElementById("acct-email").value.trim();
  var kindle = document.getElementById("acct-kindle").value.trim();
  if (!code || !email || !kindle) { note("Fill in code, email, and Kindle address.", "err"); return; }
  note("Redeeming…");
  var r = await send({ type: "acceptInvite", code: code, email: email, kindleEmail: kindle });
  if (r.error) { note(r.error, "err"); return; }
  note(r.approvedSender
    ? "Welcome! Add " + r.approvedSender + " to your Amazon approved senders."
    : "Welcome, " + r.email + "!", "ok");
  refreshStatus();
});

document.getElementById("save-key").addEventListener("click", async function () {
  var key = document.getElementById("api-key-input").value.trim();
  if (!key) { note("Paste an API key first.", "err"); return; }
  note("Checking key…");
  var r = await send({ type: "setApiKey", apiKey: key });
  if (r.error) { note(r.error, "err"); return; }
  document.getElementById("api-key-input").value = "";
  note("Signed in as " + r.email + ".", "ok");
  refreshStatus();
});

document.getElementById("signout").addEventListener("click", async function (e) {
  e.preventDefault();
  await send({ type: "signOut" });
  note("Signed out.");
  refreshStatus();
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
  // Match patterns must not contain a port (they match every port on the
  // host), so request the portless origin. A missing grant isn't fatal —
  // the Tome server answers with permissive CORS — so always save.
  var pattern = u.protocol + "//" + u.hostname + "/*";
  chrome.permissions.request({ origins: [pattern] }, function (granted) {
    void chrome.runtime.lastError; // swallow "invalid pattern" style errors
    chrome.storage.sync.set({ serverUrl: normalized }, function () {
      urlInput.value = normalized;
      noteEl.textContent = granted ? "Saved." : "Saved (no host permission — using server CORS).";
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
