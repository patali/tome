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
    document.getElementById("acct-info").textContent =
      s.email + (s.resendConfigured ? "  ·  direct delivery" : "  ·  no delivery configured");
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

chrome.storage.sync.get({ buttonStyle: "text" }, function (v) {
  var radios = { text: document.getElementById("style-text"), icons: document.getElementById("style-icons") };
  (radios[v.buttonStyle] || radios.text).checked = true;
  Object.keys(radios).forEach(function (k) {
    radios[k].addEventListener("change", function () {
      if (!radios[k].checked) return;
      chrome.storage.sync.set({ buttonStyle: k }, function () {
        note("Popup style updated.", "ok");
      });
    });
  });
});

var TOGGLES = { preview: "btn-preview", kindle: "btn-kindle" };
var DEFAULT_BUTTONS = { preview: true, kindle: true };

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

/* --- Reading font -------------------------------------------------------- */

/* Every option is a bundled face, so the sample renders in the real typeface
   rather than describing it. The chosen key travels with each conversion, and
   the server mirrors it onto <html data-font>, so the PDF matches the preview. */

var FONTS = [
  { key: "literata", name: "Literata", stack: "'Literata', Georgia, serif",
    why: "Default. Built for e-readers — even colour, generous spacing." },
  { key: "sourceserif", name: "Source Serif 4", stack: "'Source Serif 4', Georgia, serif",
    why: "Narrower than Literata, so more words per page." },
  { key: "merriweather", name: "Merriweather", stack: "'Merriweather', Georgia, serif",
    why: "Big x-height, sturdy strokes — the most forgiving on low-contrast e-ink." },
  { key: "baskerville", name: "Libre Baskerville", stack: "'Libre Baskerville', Georgia, serif",
    why: "Traditional book feel; wider, so fewer words per page." },
  { key: "inter", name: "Inter", stack: "'Inter', ui-sans-serif, system-ui, sans-serif",
    why: "Sans. Neutral and even, if you'd rather not read a serif." },
  { key: "atkinson", name: "Atkinson Hyperlegible", stack: "'Atkinson Hyperlegible', ui-sans-serif, sans-serif",
    why: "Sans, drawn by the Braille Institute to make similar letters distinct." }
];
var DEFAULT_FONT = "literata";
var SAMPLE = "The quick brown fox jumps over the lazy dog.";

function renderFontChoices(selected) {
  var host = document.getElementById("font-choices");
  host.textContent = "";
  FONTS.forEach(function (f) {
    var label = document.createElement("label");
    label.className = "font";

    var radio = document.createElement("input");
    radio.type = "radio";
    radio.name = "reading-font";
    radio.value = f.key;
    radio.checked = f.key === selected;
    radio.addEventListener("change", function () {
      if (!radio.checked) return;
      chrome.storage.sync.set({ readingFont: f.key }, function () {
        note("Reading font set to " + f.name + ".", "ok");
      });
    });

    var body = document.createElement("div");
    var name = document.createElement("span");
    name.className = "fname";
    name.textContent = f.name;
    var why = document.createElement("div");
    why.className = "fwhy";
    why.textContent = f.why;
    var sample = document.createElement("span");
    sample.className = "fsample";
    sample.style.fontFamily = f.stack;
    sample.textContent = SAMPLE;

    body.appendChild(name);
    body.appendChild(why);
    body.appendChild(sample);
    label.appendChild(radio);
    label.appendChild(body);
    host.appendChild(label);
  });
}

chrome.storage.sync.get({ readingFont: DEFAULT_FONT }, function (v) {
  renderFontChoices(v.readingFont || DEFAULT_FONT);
});

/* --- Version ------------------------------------------------------------ */

/* Unpacked installs never auto-update, so the only honest thing to do is show
   both numbers and point at the instructions. */

var versionLine = document.getElementById("version-line");
var updateGuide = document.getElementById("update-guide");

function renderVersion(st) {
  updateGuide.hidden = !st.updateAvailable;
  if (st.updateAvailable) {
    versionLine.className = "";
    versionLine.textContent =
      "Installed " + st.installed + " · version " + st.latestVersion + " is available from your server.";
    return;
  }
  versionLine.className = "muted";
  versionLine.textContent = "Installed " + st.installed +
    (st.latestVersion ? " · up to date with your server" : " · server hasn't reported a version");
}

send({ type: "updateState" }).then(renderVersion);

document.getElementById("check-update").addEventListener("click", async function () {
  versionLine.textContent = "Checking…";
  versionLine.className = "muted";
  renderVersion(await send({ type: "checkUpdate" }));
});

updateGuide.addEventListener("click", function (e) {
  e.preventDefault();
  chrome.storage.sync.get({ serverUrl: "" }, function (v) {
    if (v.serverUrl) window.open(v.serverUrl + "/install", "_blank");
  });
});

/* --- Recent conversions ------------------------------------------------- */

/* The popup drops a job once it has shown the outcome once, so this is the
   only place a past conversion can be looked up again. */

var historyEl = document.getElementById("history");
var clearBtn = document.getElementById("clear-history");
var STATE_ICON = { running: "⏳", done: "✓", error: "✕" };

function relTime(ms) {
  if (!ms) return "";
  var secs = Math.round((Date.now() - ms) / 1000);
  if (secs < 60) return "just now";
  var mins = Math.round(secs / 60);
  if (mins < 60) return mins + "m ago";
  var hrs = Math.round(mins / 60);
  if (hrs < 24) return hrs + "h ago";
  return Math.round(hrs / 24) + "d ago";
}

function renderHistory(jobs) {
  historyEl.textContent = "";
  clearBtn.hidden = !jobs.length;
  if (!jobs.length) {
    historyEl.className = "muted";
    historyEl.textContent = "Nothing converted yet.";
    return;
  }
  historyEl.className = "";
  jobs.forEach(function (j) {
    var row = document.createElement("div");
    row.className = "hrow " + j.state;

    var state = document.createElement("span");
    state.className = "hstate";
    state.textContent = STATE_ICON[j.state] || "•";
    state.title = j.state;

    var body = document.createElement("div");
    body.className = "hbody";
    var title = document.createElement(j.url ? "a" : "span");
    title.className = "htitle";
    title.textContent = j.title || "This page";
    if (j.url) { title.href = j.url; title.target = "_blank"; title.rel = "noreferrer"; }
    var msg = document.createElement("span");
    msg.className = "hmsg";
    msg.textContent = j.label ? j.label + " · " + (j.message || "") : (j.message || "");
    body.appendChild(title);
    body.appendChild(msg);

    var when = document.createElement("span");
    when.className = "hwhen";
    when.textContent = relTime(j.endedAt || j.startedAt);

    row.appendChild(state);
    row.appendChild(body);
    row.appendChild(when);
    historyEl.appendChild(row);
  });
}

function refreshHistory() {
  send({ type: "getJobs" }).then(function (r) { renderHistory(r.jobs || []); });
}
refreshHistory();

chrome.storage.onChanged.addListener(function (changes, area) {
  if (area === "local" && changes.jobs) renderHistory(changes.jobs.newValue || []);
});

clearBtn.addEventListener("click", async function () {
  await send({ type: "clearJobs" });
  note("History cleared.", "ok");
});
