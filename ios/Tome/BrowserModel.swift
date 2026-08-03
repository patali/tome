import Foundation
import WebKit

/// Drives the in-app browser and the extraction that runs inside it.
///
/// This is the path for everything the share sheet can't reach: pages opened
/// from apps other than Safari, and — more importantly — sites whose logged-in
/// session has to live somewhere. The WebView keeps its own cookie jar in
/// `WKWebsiteDataStore.default()`, which persists across launches, so signing
/// in to a site here is a one-time cost.
///
/// Extraction results come back over a script message handler rather than
/// `evaluateJavaScript`'s return value: an article is routinely hundreds of KB
/// of HTML, and the return path is the one that gets unreliable at that size.
@MainActor
final class BrowserModel: NSObject, ObservableObject {

    enum Status: Equatable {
        case idle
        case working(String)
        case sent(String)
        case failed(String)
    }

    @Published var addressText = ""
    @Published var isLoading = false
    @Published var canGoBack = false
    @Published var canGoForward = false
    @Published var status: Status = .idle

    let webView: WKWebView

    /// Resumed by the script message handler when the page hands back an article.
    private var pendingExtraction: CheckedContinuation<[String: Any], Error>?

    /// Auto-scroll runs to an 8s deadline in JS, plus decode time. This is the
    /// backstop for a page that never calls back at all.
    private static let extractionTimeout: Duration = .seconds(25)

    private static let messageHandlerName = "tome"

    override init() {
        let configuration = WKWebViewConfiguration()
        // Explicitly the persistent store: an ephemeral one would silently throw
        // away every site login between launches, which is the whole point here.
        configuration.websiteDataStore = .default()

        // Makes the user-agent read as mobile Safari rather than a bare
        // WKWebView, so sites serve their normal mobile layout instead of a
        // degraded one. This is the sanctioned API for it — it appends to
        // WebKit's own UA rather than replacing it.
        //
        // It does NOT make "Sign in with Google" work. Google blocks OAuth in
        // embedded webviews deliberately (`disallowed_useragent`) using signals
        // beyond the UA string, to stop apps harvesting credentials. Sites that
        // only offer Google sign-in should go through the Safari share sheet,
        // which uses the real Safari session.
        configuration.applicationNameForUserAgent = "Version/17.0 Mobile/15E148 Safari/604.1"

        if let js = TomeConfig.extractionJS {
            // Defines the extractors and __tomeExtract(); runs nothing on load.
            // Extraction happens only when the user asks for it.
            configuration.userContentController.addUserScript(
                WKUserScript(
                    source: js,
                    injectionTime: .atDocumentEnd,
                    forMainFrameOnly: true
                )
            )
        }

        webView = WKWebView(frame: .zero, configuration: configuration)
        super.init()

        webView.navigationDelegate = self
        webView.allowsBackForwardNavigationGestures = true

        // WKUserContentController retains its handler strongly, so registering
        // `self` directly would make this object immortal and leak a WebView per
        // browser screen. The weak box breaks that cycle.
        configuration.userContentController.add(
            WeakScriptMessageHandler(self),
            name: Self.messageHandlerName
        )
    }

    // MARK: - Navigation

    /// Accepts what a person actually types: a bare domain, a full URL, or a
    /// search phrase.
    func go(to text: String) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }

        let target: URL?
        if trimmed.contains(" ") || !trimmed.contains(".") {
            var components = URLComponents(string: "https://duckduckgo.com/")
            components?.queryItems = [URLQueryItem(name: "q", value: trimmed)]
            target = components?.url
        } else if trimmed.hasPrefix("http://") || trimmed.hasPrefix("https://") {
            target = URL(string: trimmed)
        } else {
            target = URL(string: "https://" + trimmed)
        }

        guard let url = target else { return }
        status = .idle
        webView.load(URLRequest(url: url))
    }

    func goBack() { webView.goBack() }
    func goForward() { webView.goForward() }

    /// Worth having its own control: a login wall or an app-install interstitial
    /// often clears on a reload, and this is the everyday route for X.
    func reload() { webView.reload() }

    // MARK: - Preview

    /// Set when the user asks for a preview; drives the sheet in BrowserView.
    @Published var previewSubject: ExtractionResult?

    /// Extracts without sending, so the preview can render the real PDF and let
    /// the reading controls be adjusted before anything is delivered.
    func preparePreview() async {
        status = .working("Reading the article…")
        do {
            let raw = try await extract()
            switch ExtractionResult.decode(from: raw) {
            case .failure(let error):
                status = .failed(error.errorDescription ?? "Extraction failed.")
            case .success(let result):
                status = .idle
                previewSubject = result
            }
        } catch let error as TomeError {
            status = .failed(error.errorDescription ?? "Something went wrong.")
        } catch {
            status = .failed(error.localizedDescription)
        }
    }

    // MARK: - Send

    func sendToKindle() async {
        status = .working("Reading the article…")
        do {
            let client = try TomeClient.fromConfig()
            let raw = try await extract()

            switch ExtractionResult.decode(from: raw) {
            case .failure(let error):
                status = .failed(error.errorDescription ?? "Extraction failed.")
            case .success(let result):
                status = .working("Sending “\(result.title)”…")
                let sent = try await client.sendToKindle(result.article())
                let destination = sent.sentTo.map { "Sent to \($0)" } ?? "Sent to your Kindle"
                // Naming the extractor is how you can tell at a glance whether
                // the X extractor claimed the page or it quietly fell through to
                // the generic Readability pass — which looks like success but
                // produces noticeably worse output on X Articles.
                status = .sent("\(destination) · \(result.extractor)")
            }
        } catch let error as TomeError {
            status = .failed(error.errorDescription ?? "Something went wrong.")
        } catch {
            status = .failed(error.localizedDescription)
        }
    }

    /// Asks the page to extract itself, and waits for the message handler.
    ///
    /// Deliberately a single continuation with a separate timeout task that
    /// *resumes* it, rather than racing two tasks in a group. A group would
    /// deadlock here: when the timeout task throws, the group cancels its
    /// sibling and then awaits it — but `withCheckedThrowingContinuation` does
    /// not resume on cancellation, so the continuation would never fire and the
    /// await would never return. Every exit path below resumes exactly once.
    private func extract() async throws -> [String: Any] {
        // A leftover continuation would mean a previous attempt never completed;
        // fail it rather than leaking it.
        resumePending(with: .failure(TomeError.extraction("Extraction was restarted.")))

        let timeout = Task { @MainActor [weak self] in
            try? await Task.sleep(for: Self.extractionTimeout)
            guard !Task.isCancelled else { return }
            self?.resumePending(with: .failure(
                TomeError.extraction("The page took too long to read.")
            ))
        }
        defer { timeout.cancel() }

        return try await withCheckedThrowingContinuation { continuation in
            pendingExtraction = continuation
            webView.evaluateJavaScript("__tomeExtract()") { [weak self] _, error in
                // __tomeExtract returns immediately and reports later via the
                // message handler, so only an outright failure to run it is
                // interesting here.
                guard error != nil else { return }
                Task { @MainActor in
                    self?.resumePending(with: .failure(TomeError.extraction(
                        "Couldn't run on this page. Try reloading it."
                    )))
                }
            }
        }
    }

    fileprivate func handleExtraction(_ body: Any) {
        guard let dictionary = body as? [String: Any] else {
            resumePending(with: .failure(
                TomeError.extraction("The page sent back something unreadable.")
            ))
            return
        }
        resumePending(with: .success(dictionary))
    }

    /// Resumes at most once; a continuation resumed twice traps.
    private func resumePending(with result: Result<[String: Any], Error>) {
        guard let continuation = pendingExtraction else { return }
        pendingExtraction = nil
        continuation.resume(with: result)
    }
}

// MARK: - Navigation delegate

// WebKit's delegate protocols are @MainActor-annotated, and BrowserModel is
// already @MainActor, so these satisfy the requirements directly — no hop, and
// no touching main-actor state (or WKScriptMessage.body) from off the actor.
extension BrowserModel: WKNavigationDelegate {
    func webView(_ webView: WKWebView, didStartProvisionalNavigation navigation: WKNavigation!) {
        isLoading = true
        status = .idle
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        isLoading = false
        canGoBack = webView.canGoBack
        canGoForward = webView.canGoForward
        if let url = webView.url?.absoluteString { addressText = url }
    }

    func webView(
        _ webView: WKWebView,
        didFail navigation: WKNavigation!,
        withError error: Error
    ) {
        isLoading = false
        status = .failed(error.localizedDescription)
    }

    func webView(
        _ webView: WKWebView,
        didFailProvisionalNavigation navigation: WKNavigation!,
        withError error: Error
    ) {
        isLoading = false
        // A cancelled load is usually a redirect overtaking itself, not
        // something worth showing anyone.
        if (error as NSError).code != NSURLErrorCancelled {
            status = .failed(error.localizedDescription)
        }
    }
}

// MARK: - Weak message handler

/// Breaks the retain cycle `WKUserContentController` would otherwise create by
/// holding its message handler strongly.
@MainActor
private final class WeakScriptMessageHandler: NSObject, WKScriptMessageHandler {
    private weak var target: BrowserModel?

    init(_ target: BrowserModel) {
        self.target = target
    }

    func userContentController(
        _ controller: WKUserContentController,
        didReceive message: WKScriptMessage
    ) {
        target?.handleExtraction(message.body)
    }
}
