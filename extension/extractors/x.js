/*
 * extractors/x.js — X (Twitter) article extractor.
 *
 * Injected into the page's isolated world alongside the other extractor files;
 * registers itself in the shared registry that background.js dispatches over.
 *
 * Why not Readability here: X's DOM defeats it four ways — the title is a
 * <div data-testid="twitter-article-title"> (og:title is literally "X"), the
 * cover photo sits before the body container so it gets excluded, short section
 * headings are rejected as boilerplate (every <h2> vanished), and the byline
 * comes out mushed. X's data-testid landmarks are stable, so extract directly.
 */
(function () {
  var registry = (self.__TOME_EXTRACTORS = self.__TOME_EXTRACTORS || {});

  function trimText(el) {
    return el ? (el.textContent || "").replace(/\s+/g, " ").trim() : "";
  }

  registry["x-article"] = {
    // Higher priority runs first; generic fallback is 0.
    priority: 100,

    matches: function (location) {
      return /(^|\.)x\.com$|(^|\.)twitter\.com$/.test(location.hostname);
    },

    // Returns an article object, or null to let the next extractor try.
    extract: function (document, location) {
      var body = document.querySelector('[data-testid="twitterArticleRichTextView"]');
      if (!body) return null; // not an article page (timeline, tweet, ...)
      var readView = document.querySelector('[data-testid="twitterArticleReadView"]') || document;

      var title = trimText(document.querySelector('[data-testid="twitter-article-title"]'));

      // Author: handle is encoded in the avatar container's testid; the display
      // name is the profile link whose text isn't the @handle.
      var handle = "";
      var av = readView.querySelector('[data-testid^="UserAvatar-Container-"]') ||
               document.querySelector('[data-testid^="UserAvatar-Container-"]');
      if (av) handle = av.getAttribute("data-testid").slice("UserAvatar-Container-".length);
      var name = "";
      if (handle) {
        var links = document.querySelectorAll('a[href$="/' + handle + '"]');
        for (var i = 0; i < links.length; i++) {
          var t = trimText(links[i]);
          if (t && t.charAt(0) !== "@") { name = t; break; }
        }
      }
      var byline = name && handle ? name + " (@" + handle + ")" : (handle ? "@" + handle : "");

      var timeEl = readView.querySelector("time[datetime]") || document.querySelector("time[datetime]");
      var publishedTime = timeEl ? timeEl.getAttribute("datetime") : "";

      // Cover image: the first article photo that precedes the body container.
      var cover = "";
      var photos = document.querySelectorAll('[data-testid="tweetPhoto"] img');
      for (var j = 0; j < photos.length; j++) {
        if (photos[j].compareDocumentPosition(body) & Node.DOCUMENT_POSITION_FOLLOWING) {
          cover = photos[j].currentSrc || photos[j].src || "";
          break;
        }
      }

      // Sanitize a clone of the body: drop chrome, strip presentation attributes
      // (X inlines theme colors — white-on-black would print white-on-white),
      // keep only semantic structure.
      var clone = body.cloneNode(true);
      var junk = clone.querySelectorAll('script,style,link,svg,button,[role="button"]');
      for (var k = junk.length - 1; k >= 0; k--) junk[k].parentNode.removeChild(junk[k]);

      var KEEP = { a: ["href"], img: ["src", "alt"], td: ["colspan", "rowspan"], th: ["colspan", "rowspan"] };
      var all = clone.querySelectorAll("*");
      for (var m = 0; m < all.length; m++) {
        var el = all[m];
        var keep = KEEP[el.tagName.toLowerCase()] || [];
        for (var n = el.attributes.length - 1; n >= 0; n--) {
          var attr = el.attributes[n].name;
          if (keep.indexOf(attr) === -1) el.removeAttribute(attr);
        }
      }

      // Absolutize + upgrade image URLs (pbs serves size via ?name=; ask for large).
      var imgs = clone.querySelectorAll("img[src]");
      for (var q = 0; q < imgs.length; q++) {
        var abs = imgs[q].src; // property resolves relative -> absolute
        imgs[q].setAttribute("src", abs.replace(/([?&]name=)[a-z0-9]+/i, "$1large"));
      }

      // Text paragraphs are leaf <div>s; rename to <p> so paragraph CSS applies.
      var divs = clone.querySelectorAll("div");
      for (var r = divs.length - 1; r >= 0; r--) {
        var d = divs[r];
        if (!d.querySelector("div,p,h1,h2,h3,h4,ul,ol,table,pre,blockquote,img,figure") &&
            (d.textContent || "").trim()) {
          var p = document.createElement("p");
          while (d.firstChild) p.appendChild(d.firstChild);
          d.parentNode.replaceChild(p, d);
        }
      }

      var content = clone.innerHTML;
      if (!content || !trimText(clone)) return null;
      if (cover) {
        content = '<img src="' + cover.replace(/([?&]name=)[a-z0-9]+/i, "$1large").replace(/"/g, "&quot;") + '" alt="">' + content;
      }
      return {
        title: title || "Untitled X Article",
        byline: byline,
        publishedTime: publishedTime,
        content: content,
        url: location.href
      };
    }
  };
})();
