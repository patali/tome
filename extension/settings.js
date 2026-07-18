/* settings.js — the Tome settings page: server URL, account, button toggles. */

var dotEl = document.getElementById("dot");
var serverText = document.getElementById("server-text");
var noteEl = document.getElementById("note");

function send(msg) {
  return new Promise(function (resolve) {
    chrome.runtime.sendMessage(msg, function (resp) {
      if (chrome.runtime.lastError) { resolve({ error: chrome.runtime.lastError.message }); return; }
      resolve(resp || {});
    });
  });
}

function note(text, cls) {
  noteEl.textContent = text;
  noteEl.className = cls || "muted";
}

function refreshStatus() {
  send({ type: "ping" }).then(function (s) {
    document.getElementById("acct-offline").hidden = s.up;
    document.getElementById("acct-signedin").hidden = !(s.up && s.signedIn);
    document.getElementById("acct-signedout").hidden = !(s.up && !s.signedIn);

    if (!s.up) {
      dotEl.className = "dot down";
      serverText.textContent = "server down · " + (s.url || "").replace(/^https?:\/\//, "");
      return;
    }
    if (!s.signedIn) {
      dotEl.className = "dot";
      serverText.textContent = s.badKey ? "server up · stored key rejected" : "server up · not signed in";
      return;
    }
    dotEl.className = "dot up";
    serverText.textContent = "connected as " + s.email;
    var caps = [];
    if (s.resendConfigured) caps.push("direct delivery");
    if (s.mailHelper) caps.push("Mail.app helper");
    document.getElementById("acct-info").textContent =
      s.email + (caps.length ? "  ·  " + caps.join(" + ") : "  ·  no delivery configured");
    var kindleInput = document.getElementById("kindle-edit");
    if (document.activeElement !== kindleInput) kindleInput.value = s.kindleEmail || "";
  });
}
refreshStatus();

/* --- Server URL ------------------------------------------------------- */

var urlInput = document.getElementById("server-url");
chrome.storage.sync.get({ serverUrl: "http://localhost:8080" }, function (v) {
  urlInput.value = v.serverUrl;
});

document.getElementById("save-server").addEventListener("click", function () {
  var raw = urlInput.value.trim() || "http://localhost:8080";
  var u;
  try {
    u = new URL(raw);
    if (u.protocol !== "http:" && u.protocol !== "https:") throw new Error();
  } catch (e) {
    note("Enter a valid http(s) URL.", "err");
    return;
  }
  var normalized = u.origin + u.pathname.replace(/\/+$/, "");
  // Match patterns must not contain a port (they match every port on the
  // host), so request the portless origin. A missing grant isn't fatal —
  // the Tome server answers with permissive CORS — so always save.
  var pattern = u.protocol + "//" + u.hostname + "/*";
  chrome.permissions.request({ origins: [pattern] }, function (granted) {
    void chrome.runtime.lastError;
    chrome.storage.sync.set({ serverUrl: normalized }, function () {
      urlInput.value = normalized;
      note(granted ? "Server saved." : "Server saved (no host permission — using server CORS).", "ok");
      refreshStatus();
    });
  });
});

/* --- Account ----------------------------------------------------------- */

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

document.getElementById("save-kindle").addEventListener("click", async function () {
  var kindle = document.getElementById("kindle-edit").value.trim();
  if (!kindle) { note("Enter your @kindle.com address.", "err"); return; }
  note("Saving…");
  var r = await send({ type: "updateKindle", kindleEmail: kindle });
  if (r.error) { note(r.error, "err"); return; }
  note("Kindle address updated to " + r.kindleEmail + ".", "ok");
  refreshStatus();
});

document.getElementById("signout").addEventListener("click", async function () {
  await send({ type: "signOut" });
  note("Signed out.");
  refreshStatus();
});

/* --- Popup button toggles ---------------------------------------------- */

var TOGGLES = { preview: "btn-preview", kindle: "btn-kindle", mail: "btn-mail" };
var DEFAULT_BUTTONS = { preview: true, kindle: true, mail: true };

chrome.storage.sync.get({ buttons: DEFAULT_BUTTONS }, function (v) {
  Object.keys(TOGGLES).forEach(function (k) {
    var box = document.getElementById(TOGGLES[k]);
    box.checked = v.buttons[k] !== false;
    box.addEventListener("change", function () {
      chrome.storage.sync.get({ buttons: DEFAULT_BUTTONS }, function (cur) {
        cur.buttons[k] = box.checked;
        chrome.storage.sync.set({ buttons: cur.buttons }, function () {
          note("Popup buttons updated.", "ok");
        });
      });
    });
  });
});
