/*
 * tome-ios-glue.js — the iOS-only tail of the extraction bundle.
 *
 * build-extraction-js.sh concatenates, in order:
 *   extension/lib/Readability.js
 *   extension/extractors/x.js
 *   extension/extractors/generic.js
 *   runExtractors()            (lifted from extension/background.js by marker)
 *   this file
 *
 * Everything above is shared verbatim with the browser extension. This file is
 * the only iOS-specific code, and it exists because the two iOS hosts need
 * different entry points into the same dispatcher:
 *
 *   Share Extension  -> ExtensionPreprocessingJS, which iOS calls itself.
 *   In-app WebView   -> __tomeExtract(), which the app calls on demand.
 *
 * Nothing here runs on load. The WebView injects this at document-end and then
 * waits for the user to actually ask for the article.
 *
 * Deliberately ES5, like the extractors it ships with: this same text is handed
 * to Apple's JS preprocessing sandbox, and matching the extension's dialect
 * keeps one bundle valid in both places.
 */

/* Normalizes whatever the dispatcher returns into something the app can always
 * act on. A failed extraction still carries the URL and document title, so the
 * app can name the failure (or offer a retry) instead of showing "unknown". */
function __tomeResult() {
  var out;
  try {
    out = runExtractors() || { error: "Extraction returned nothing." };
  } catch (e) {
    out = { error: String((e && e.message) || e) };
  }
  if (!out.url) out.url = location.href;
  if (!out.title) out.title = (document.title || "").trim();
  return out;
}

/* Scrolls the page to force lazy-loaded images to resolve, then returns to the
 * top. The extension can't do this — extractors/generic.js promotes data-src
 * attributes and otherwise the popup tells you to "Scroll through it once, then
 * retry". Driving our own WebView means we can just do it.
 *
 * Stops early once the page stops growing at the bottom, so ordinary articles
 * cost a few hundred ms rather than the full budget; the deadline is what keeps
 * infinite-scroll feeds from running forever. */
function __tomeAutoScroll(done) {
  var DEADLINE_MS = 8000;
  var STEP_MS = 120;
  var started = Date.now();
  var lastHeight = -1;
  var y = 0;

  function pageHeight() {
    return Math.max(
      document.body ? document.body.scrollHeight : 0,
      document.documentElement ? document.documentElement.scrollHeight : 0
    );
  }

  function step() {
    var height = pageHeight();
    var atBottom = y + window.innerHeight >= height;
    // Settled: we're at the bottom and the page didn't grow in response.
    if ((atBottom && height === lastHeight) || Date.now() - started > DEADLINE_MS) {
      window.scrollTo(0, 0);
      // A beat for images that just entered the viewport to start decoding.
      setTimeout(done, 250);
      return;
    }
    lastHeight = height;
    y += Math.max(window.innerHeight * 0.85, 200);
    window.scrollTo(0, y);
    setTimeout(step, STEP_MS);
  }

  step();
}

/* WebView entry point. Results go back over the message handler rather than as
 * an evaluateJavaScript return value: an article is often hundreds of KB of
 * HTML, and the return path is the one that gets unreliable at that size. */
function __tomeExtract() {
  __tomeAutoScroll(function () {
    var out = __tomeResult();
    try {
      window.webkit.messageHandlers.tome.postMessage(out);
    } catch (e) {
      /* Not running inside the Tome WebView — nothing to hand back to. */
    }
  });
}

/* Share Extension entry point. iOS instantiates this global itself, runs it
 * inside the live Safari page, and hands the dict to ShareViewController.
 *
 * No auto-scroll on this path, on purpose: the share sheet's time budget is
 * short and undocumented, and a completionFunction that arrives too late means
 * the share fails silently. The user was already reading the page, so it is
 * typically scrolled anyway. */
function TomePreprocessor() {}
TomePreprocessor.prototype = {
  run: function (args) {
    args.completionFunction(__tomeResult());
  },
  finalize: function () {}
};

var ExtensionPreprocessingJS = new TomePreprocessor();
