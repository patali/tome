/* popup.js — action buttons + the running-job queue. All configuration lives
   in the settings page (gear icon -> settings.html).

   The popup is disposable: clicking the page behind it closes it and tears
   down any pending message response. So actions only *start* a job here, and
   everything shown comes from the job records in storage. */

var readerBtn = document.getElementById("reader");
var kindleBtn = document.getElementById("kindle");
var statusEl = document.getElementById("status");
var queueEl = document.getElementById("queue");
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

// Which action buttons are shown, and how (icon+text vs icons only), are
// user settings (settings page).
chrome.storage.sync.get(
  { buttons: { preview: true, kindle: true }, buttonStyle: "text" },
  function (v) {
    readerBtn.hidden = v.buttons.preview === false;
    kindleBtn.hidden = v.buttons.kindle === false;
    document.body.classList.toggle("icons-only", v.buttonStyle === "icons");
  });

// Server + account state decide which of the visible buttons are usable.
// "Open preview" is fully client-side, so it never depends on the server.
send({ type: "ping" }).then(function (s) {
  if (!s.up) {
    dotEl.className = "dot down";
    serverText.textContent = "server down · " + (s.url || "").replace(/^https?:\/\//, "");
    serverText.className = "muted";
    hintEl.hidden = false;
    kindleBtn.disabled = true;
    return;
  }
  if (!s.signedIn) {
    dotEl.className = "dot";
    serverText.textContent = s.badKey ? "stored key rejected" : "sign-in required";
    serverText.className = "";
    hintEl.hidden = false;
    kindleBtn.disabled = true;
    return;
  }
  dotEl.className = "dot up";
  serverText.textContent = s.email;
  serverText.className = "";
  kindleBtn.disabled = !s.resendConfigured;
  kindleBtn.title = s.resendConfigured
    ? "Email the document to " + (s.kindleEmail || "your Kindle")
    : "Ask the admin to configure Resend delivery";
});

/* --- Update notice ------------------------------------------------------ */

/* The extension is installed unpacked, so nothing updates it automatically.
   Surface it once, quietly, with a way to say "not now". */

var updateEl = document.getElementById("update");
var updateText = document.getElementById("update-text");

function showUpdate(st) {
  if (!st.showUpdate) { updateEl.hidden = true; return; }
  updateText.textContent = "Version " + st.latestVersion + " is available (you have " + st.installed + ").";
  updateEl.hidden = false;
}

send({ type: "updateState" }).then(showUpdate);

document.getElementById("update-how").addEventListener("click", function (e) {
  e.preventDefault();
  chrome.storage.sync.get({ serverUrl: "" }, function (v) {
    if (v.serverUrl) chrome.tabs.create({ url: v.serverUrl + "/install" });
  });
});

document.getElementById("update-dismiss").addEventListener("click", async function () {
  await send({ type: "dismissUpdate" });
  updateEl.hidden = true;
});

/* --- Queue -------------------------------------------------------------- */

var ICON = { running: "⏳", done: "✓", error: "✕" };

// Ids this popup has already put on screen. Marking a job seen must not yank
// it out from under the user who is watching it finish, so anything shown once
// stays for the life of this popup; the set dies with the popup, which is what
// makes a seen job disappear on the *next* open rather than immediately.
var shownIds = [];

// Show anything still running, plus finished jobs no popup has displayed yet —
// that second group is the whole point: it's the result the user missed
// because the popup closed while the conversion was in flight.
function visibleJobs(jobs) {
  return jobs.filter(function (j) {
    return j.state === "running" || !j.seen || shownIds.indexOf(j.id) !== -1;
  });
}

function renderQueue(jobs) {
  var list = visibleJobs(jobs);
  list.forEach(function (j) {
    if (shownIds.indexOf(j.id) === -1) shownIds.push(j.id);
  });
  queueEl.textContent = "";
  list.forEach(function (j) {
    var row = document.createElement("div");
    row.className = "job " + j.state;

    var ico = document.createElement("span");
    ico.className = "jico";
    ico.textContent = ICON[j.state] || "•";

    var body = document.createElement("div");
    body.className = "jbody";
    var title = document.createElement("span");
    title.className = "jtitle";
    title.textContent = j.title || "This page";
    title.title = j.title || "";
    var msg = document.createElement("span");
    msg.className = "jmsg";
    msg.textContent = j.message || "";
    body.appendChild(title);
    body.appendChild(msg);

    row.appendChild(ico);
    row.appendChild(body);
    queueEl.appendChild(row);
  });

  // Mark the finished ones seen so they clear on the next open. Running jobs
  // stay unseen — they haven't shown their outcome yet. Skipping the already
  // seen matters: marking writes storage, which re-enters this function, and
  // an unconditional mark would never stop re-marking.
  var settled = list.filter(function (j) { return j.state !== "running" && !j.seen; })
    .map(function (j) { return j.id; });
  if (settled.length) send({ type: "markSeen", ids: settled });
}

send({ type: "getJobs" }).then(function (r) { renderQueue(r.jobs || []); });

// Live-update while the popup happens to be open.
chrome.storage.onChanged.addListener(function (changes, area) {
  if (area !== "local" || !changes.jobs) return;
  renderQueue(changes.jobs.newValue || []);
});

/* --- Actions ------------------------------------------------------------ */

function runAction(btn, mode) {
  btn.addEventListener("click", async function () {
    setStatus("");
    var r = await send({ type: "convert", mode: mode });
    if (r.error) { setStatus(r.error, "err"); return; }
    // The reader opens its own tab, which closes the popup anyway.
    if (mode === "reader") window.close();
  });
}

runAction(readerBtn, "reader");
runAction(kindleBtn, "kindle");
