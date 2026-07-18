/* popup.js — action buttons + status. All configuration lives in the
   settings page (gear icon -> settings.html). */

var readerBtn = document.getElementById("reader");
var kindleBtn = document.getElementById("kindle");
var mailBtn = document.getElementById("mail");
var statusEl = document.getElementById("status");
var dotEl = document.getElementById("dot");
var serverText = document.getElementById("server-text");
var hintEl = document.getElementById("setup-hint");

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

function openSettings() { chrome.runtime.openOptionsPage(); }
document.getElementById("gear").addEventListener("click", openSettings);
document.getElementById("open-settings").addEventListener("click", function (e) {
  e.preventDefault();
  openSettings();
});

// Which action buttons are shown is a user setting (settings page).
chrome.storage.sync.get({ buttons: { preview: true, kindle: true, mail: true } }, function (v) {
  readerBtn.hidden = v.buttons.preview === false;
  kindleBtn.hidden = v.buttons.kindle === false;
  mailBtn.hidden = v.buttons.mail === false;
});

// Server + account state decide which of the visible buttons are usable.
// "Open preview" is fully client-side, so it never depends on the server.
send({ type: "ping" }).then(function (s) {
  if (!s.up) {
    dotEl.className = "dot down";
    serverText.textContent = "server down · " + (s.url || "").replace(/^https?:\/\//, "");
    serverText.className = "muted";
    hintEl.hidden = false;
    kindleBtn.disabled = mailBtn.disabled = true;
    return;
  }
  if (!s.signedIn) {
    dotEl.className = "dot";
    serverText.textContent = s.badKey ? "stored key rejected" : "sign-in required";
    serverText.className = "";
    hintEl.hidden = false;
    kindleBtn.disabled = mailBtn.disabled = true;
    return;
  }
  dotEl.className = "dot up";
  serverText.textContent = s.email;
  serverText.className = "";
  kindleBtn.disabled = !s.resendConfigured;
  kindleBtn.title = s.resendConfigured
    ? "Email the document to " + (s.kindleEmail || "your Kindle")
    : "Ask the admin to configure Resend delivery";
  mailBtn.disabled = !s.mailHelper;
  mailBtn.title = s.mailHelper
    ? "Open Mail.app with the document attached (to " + (s.kindleEmail || "your Kindle") + ")"
    : "Install the mail helper: run extension/native-host/install.sh, then restart the browser";
});

function runAction(btn, mode, busyText, doneText) {
  btn.addEventListener("click", async function () {
    setStatus(busyText, "muted");
    btn.disabled = true;
    var r = await send({ type: "convert", mode: mode });
    btn.disabled = false;
    if (r.error) { setStatus(r.error, "err"); return; }
    setStatus(doneText(r), "ok");
    if (mode === "reader") window.close();
  });
}

runAction(readerBtn, "reader", "Extracting…", function () { return "Preview opened."; });
runAction(kindleBtn, "kindle", "Converting and sending…", function (r) { return "Sent to " + (r.sentTo || "Kindle") + " ✓"; });
runAction(mailBtn, "mail", "Converting…", function () { return "Opened in Mail — review and hit Send."; });
