import Foundation

/// The payload the server expects. Mirrors `article.Article` in
/// server/internal/article/article.go — the JSON keys here are the contract, so
/// they must stay in step with the Go struct tags.
///
/// This is deliberately the same shape the browser extension POSTs (see
/// `deliver()` in extension/background.js), which is why the server needs no
/// changes to accept articles from iOS.
struct Article: Encodable {
    var title: String
    var byline: String
    var publishedTime: String
    /// Sanitized HTML from the extractor. The server rejects an empty value.
    var content: String
    var url: String

    /// "scribe" | "scribe3" | "paperwhite"
    var device: String
    /// "pdf" | "epub". Left empty to let the server choose (it prefers PDF when
    /// Chrome is available).
    var format: String
    /// "bw" | "color"
    var color: String
    /// Body face key — see pdfgen.BodyFonts.
    var font: String
    /// The reader stylesheet, shipped so the PDF matches the desktop output.
    var css: String

    init(
        title: String,
        byline: String = "",
        publishedTime: String = "",
        content: String,
        url: String,
        device: String = TomeConfig.device,
        format: String = "",
        color: String = TomeConfig.color,
        font: String = TomeConfig.font,
        css: String = TomeConfig.readerCSS
    ) {
        self.title = title
        self.byline = byline
        self.publishedTime = publishedTime
        self.content = content
        self.url = url
        self.device = device
        self.format = format
        self.color = color
        self.font = font
        self.css = css
    }
}

/// What the JS extractors hand back, before reading preferences are attached.
///
/// Both iOS hosts produce this same dictionary — the Share Extension gets it
/// from `NSExtensionJavaScriptPreprocessingResultsKey`, the WebView gets it
/// through a `WKScriptMessageHandler` — so decoding lives in one place.
struct ExtractionResult: Identifiable {
    /// The article's own URL — stable enough to identify a preview sheet.
    var id: String { url }

    var title: String
    var byline: String
    var publishedTime: String
    var content: String
    var url: String
    /// Which extractor claimed the page ("x-article", "generic-readability").
    /// Useful when a page silently falls through to the generic pass.
    var extractor: String

    /// Turns the raw JS dictionary into either a result or a readable failure.
    ///
    /// The glue guarantees `url` and `title` are present even on the error path,
    /// so a failure can still name the page it failed on.
    static func decode(from raw: [String: Any]) -> Result<ExtractionResult, TomeError> {
        // The extractors signal failure with an `error` key rather than throwing.
        if let message = raw["error"] as? String, !message.isEmpty {
            return .failure(.extraction(message))
        }

        let content = (raw["content"] as? String) ?? ""
        guard !content.isEmpty else {
            // Mirrors the server's own check, but caught here so the user gets a
            // sentence instead of a 400.
            return .failure(.extraction("No article content found on this page."))
        }

        let title = ((raw["title"] as? String) ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return .success(
            ExtractionResult(
                title: title.isEmpty ? "Untitled Article" : title,
                byline: (raw["byline"] as? String) ?? "",
                publishedTime: (raw["publishedTime"] as? String) ?? "",
                content: content,
                url: (raw["url"] as? String) ?? "",
                extractor: (raw["extractor"] as? String) ?? "unknown"
            )
        )
    }

    /// Attaches reading preferences and the bundled stylesheet.
    ///
    /// The overrides let the preview re-render with a different device or face
    /// without disturbing the saved defaults — you can look at a page on the
    /// Paperwhite geometry and still send it as Scribe.
    func article(
        device: String = TomeConfig.device,
        font: String = TomeConfig.font,
        color: String = TomeConfig.color
    ) -> Article {
        Article(
            title: title,
            byline: byline,
            publishedTime: publishedTime,
            content: content,
            url: url,
            device: device,
            color: color,
            font: font
        )
    }
}
