/*
 * extractors/generic.js — Readability fallback for any page no dedicated
 * extractor claims (blogs, news sites, ...). Future dedicated extractors
 * (e.g. extractors/medium.js) should register with priority > 0 so they
 * run before this one.
 *
 * Requires lib/Readability.js to be injected into the same isolated world.
 */
(function () {
  var registry = (self.__TOME_EXTRACTORS = self.__TOME_EXTRACTORS || {});

  registry["generic-readability"] = {
    priority: 0,

    matches: function () { return true; },

    extract: function (document, location) {
      if (typeof Readability !== "function") {
        return { error: "Readability failed to load into the page." };
      }
      // Promote lazy-loaded media so it survives extraction.
      var imgs = document.querySelectorAll("img[data-src], img[data-srcset]");
      for (var i = 0; i < imgs.length; i++) {
        var im = imgs[i];
        if (im.dataset.src && !im.src) im.src = im.dataset.src;
        if (im.dataset.srcset && !im.srcset) im.srcset = im.dataset.srcset;
      }
      var docClone = document.cloneNode(true);
      // A cloned document's baseURI is about:blank, so relative links would
      // resolve wrong. Inject a <base> so Readability absolutizes them.
      try {
        var base = docClone.createElement("base");
        base.setAttribute("href", location.href);
        var head = docClone.head || docClone.documentElement;
        if (head) head.insertBefore(base, head.firstChild);
      } catch (e) { /* best effort */ }

      var article = new Readability(docClone).parse();
      if (!article || !article.content) {
        return { error: "No article content found on this page. Scroll through it once, then retry." };
      }
      var title = (article.title || "").trim();
      if (!title || /^(x|twitter)$/i.test(title)) {
        var og = document.querySelector('meta[property="og:title"]');
        title = (og && og.getAttribute("content") || "").trim() || title || "Untitled Article";
      }
      return {
        title: title,
        byline: article.byline || "",
        publishedTime: article.publishedTime || "",
        content: article.content,
        url: location.href
      };
    }
  };
})();
