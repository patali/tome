import UIKit
import UniformTypeIdentifiers
import UserNotifications

/// The Safari share sheet entry point.
///
/// iOS has already run the extraction bundle inside the live page by the time
/// this loads — that's what `NSExtensionJavaScriptPreprocessingFile` in
/// Info.plist buys us, and it's the whole reason the iOS path handles logged-in
/// sites without asking anyone to sign in again. All that's left here is to
/// unwrap the results, POST them, and report.
///
/// This view controller does the upload itself rather than handing off to the
/// container app, because App Groups (the usual handoff mechanism) require a
/// paid membership. The consequence: the sheet must stay on screen until the
/// request finishes, so the UI is built around waiting rather than dismissing
/// immediately. See TomeConfig for the full picture.
///
/// Built programmatically — no storyboard — so there is one place to look when
/// something doesn't appear.
final class ShareViewController: UIViewController {

    private let card = UIView()
    private let titleLabel = UILabel()
    private let statusLabel = UILabel()
    private let spinner = UIActivityIndicatorView(style: .medium)
    private let dismissButton = UIButton(type: .system)
    private let copyButton = UIButton(type: .system)

    /// Set when the share was a bare link this extension can't read itself.
    private var pendingLink: URL?

    // MARK: - Lifecycle

    override func viewDidLoad() {
        super.viewDidLoad()
        buildUI()
        Task { await run() }
    }

    // MARK: - Work

    /// What the host app actually handed over.
    private enum ShareInput {
        /// Safari: our JavaScript ran in the live page and returned an article.
        case page([String: Any])
        /// Everything else (X, Reddit, Messages): a bare link, no DOM.
        case link(URL)
    }

    private func run() async {
        do {
            switch try await resolveInput() {
            case .page(let raw):
                try await send(raw)
            case .link(let url):
                await handOff(url)
            }
        } catch let error as TomeError {
            finish(with: error)
        } catch {
            finish(with: TomeError.network(error.localizedDescription))
        }
    }

    private func send(_ raw: [String: Any]) async throws {
        switch ExtractionResult.decode(from: raw) {
        case .failure(let error):
            finish(with: error)
        case .success(let result):
            show(title: result.title, status: "Sending to Kindle…", busy: true)
            let client = try TomeClient.fromConfig()
            let sent = try await client.sendToKindle(result.article())
            let destination = sent.sentTo.map { "Sent to \($0)" } ?? "Sent to your Kindle"
            show(title: result.title, status: destination, busy: false)
            // Long enough to read, short enough not to feel stuck.
            try? await Task.sleep(nanoseconds: 1_400_000_000)
            complete()
        }
    }

    /// A link with no page behind it can't be read from here — this extension
    /// has its own cookie jar, so loading it would arrive logged out, which for
    /// X means no article content at all. The article has to be opened where a
    /// session exists: Safari, or the app's own browser.
    ///
    /// A share extension cannot launch its containing app. That isn't a missing
    /// piece of this implementation — Apple restricts it to Today extensions and
    /// widgets, and the responder-chain `openURL:` walk that used to work is
    /// force-failed from iOS 18 on ("BUG IN CLIENT OF UIKIT: The caller of
    /// UIApplication.openURL(:) needs to migrate…"). So this offers the link
    /// instead of pretending to route it.
    private func handOff(_ url: URL) async {
        let center = UNUserNotificationCenter.current()
        let settings = await center.notificationSettings()

        guard settings.authorizationStatus == .authorized else {
            // Permission is requested by the container app on first launch —
            // this extension can't ask for it itself.
            showCopyFallback(
                url,
                reason: "Allow notifications for Tome to open links in one tap. "
                    + "For now, copy this and paste it into Tome's browser."
            )
            return
        }

        let content = UNMutableNotificationContent()
        content.title = "Open in Tome"
        content.body = url.host ?? url.absoluteString
        content.userInfo = [TomeNotification.urlKey: url.absoluteString]
        content.categoryIdentifier = TomeNotification.categoryIdentifier

        // nil trigger delivers immediately.
        let request = UNNotificationRequest(
            identifier: UUID().uuidString,
            content: content,
            trigger: nil
        )

        do {
            try await center.add(request)
            show(title: "Open in Tome", status: "Tap the notification to load it.", busy: false)
            try? await Task.sleep(nanoseconds: 900_000_000)
            complete()
        } catch {
            showCopyFallback(
                url,
                reason: "Couldn't post a notification. Copy this and paste it into Tome's browser."
            )
        }
    }

    /// Shown when the one-tap route isn't available. Honest about what happened
    /// rather than dismissing and leaving nothing behind.
    private func showCopyFallback(_ url: URL, reason: String) {
        pendingLink = url
        titleLabel.text = "Open this in Tome"
        statusLabel.text = reason
        spinner.stopAnimating()
        copyButton.isHidden = false
        dismissButton.isHidden = false
    }

    @objc private func copyTapped() {
        guard let pendingLink else { return }
        UIPasteboard.general.url = pendingLink
        copyButton.setTitle("Copied", for: .normal)
        copyButton.isEnabled = false
    }

    /// Reads whichever attachment is actually usable.
    ///
    /// Safari supplies both a property list (our JavaScript's results) and a
    /// URL, and the order isn't guaranteed — so scan for the property list
    /// across every attachment first rather than trusting `attachments.first`.
    /// It's the richer input and the only one carrying the logged-in DOM.
    private func resolveInput() async throws -> ShareInput {
        guard
            let item = extensionContext?.inputItems.first as? NSExtensionItem,
            let attachments = item.attachments
        else {
            throw TomeError.notAWebPage
        }

        let plistType = UTType.propertyList.identifier
        for provider in attachments
        where provider.hasItemConformingToTypeIdentifier(plistType) {
            guard
                let dictionary = try? await loadItem(provider, type: plistType) as? NSDictionary,
                let results = dictionary[NSExtensionJavaScriptPreprocessingResultsKey]
                    as? [String: Any]
            else { continue }
            return .page(results)
        }

        let urlType = UTType.url.identifier
        for provider in attachments
        where provider.hasItemConformingToTypeIdentifier(urlType) {
            guard let url = try? await loadItem(provider, type: urlType) as? URL else { continue }
            return .link(url)
        }

        throw TomeError.notAWebPage
    }

    private func loadItem(_ provider: NSItemProvider, type: String) async throws -> NSSecureCoding? {
        try await withCheckedThrowingContinuation { continuation in
            provider.loadItem(forTypeIdentifier: type, options: nil) { value, error in
                if let error {
                    continuation.resume(throwing: error)
                } else {
                    continuation.resume(returning: value)
                }
            }
        }
    }


    // MARK: - Completion

    private func finish(with error: TomeError) {
        show(
            title: "Couldn't send",
            status: error.errorDescription ?? "Something went wrong.",
            busy: false
        )
        dismissButton.isHidden = false
    }

    private func complete() {
        extensionContext?.completeRequest(returningItems: nil)
    }

    @objc private func cancelTapped() {
        extensionContext?.cancelRequest(withError: NSError(
            domain: "com.tome.share", code: 0
        ))
    }

    // MARK: - UI

    private func show(title: String, status: String, busy: Bool) {
        titleLabel.text = title
        statusLabel.text = status
        if busy { spinner.startAnimating() } else { spinner.stopAnimating() }
    }

    private func buildUI() {
        view.backgroundColor = UIColor.black.withAlphaComponent(0.25)

        card.backgroundColor = .systemBackground
        card.layer.cornerRadius = 16
        card.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(card)

        titleLabel.font = .preferredFont(forTextStyle: .headline)
        titleLabel.numberOfLines = 2
        titleLabel.text = "Tome"

        statusLabel.font = .preferredFont(forTextStyle: .subheadline)
        statusLabel.textColor = .secondaryLabel
        statusLabel.numberOfLines = 0
        statusLabel.text = "Reading the article…"

        dismissButton.setTitle("Close", for: .normal)
        dismissButton.addTarget(self, action: #selector(cancelTapped), for: .touchUpInside)
        dismissButton.isHidden = true

        copyButton.setTitle("Copy link", for: .normal)
        copyButton.addTarget(self, action: #selector(copyTapped), for: .touchUpInside)
        copyButton.isHidden = true

        let textStack = UIStackView(arrangedSubviews: [titleLabel, statusLabel])
        textStack.axis = .vertical
        textStack.spacing = 4

        let row = UIStackView(arrangedSubviews: [spinner, textStack])
        row.axis = .horizontal
        row.spacing = 12
        row.alignment = .center

        let stack = UIStackView(arrangedSubviews: [row, copyButton, dismissButton])
        stack.axis = .vertical
        stack.spacing = 12
        stack.translatesAutoresizingMaskIntoConstraints = false
        card.addSubview(stack)

        spinner.startAnimating()

        NSLayoutConstraint.activate([
            card.centerYAnchor.constraint(equalTo: view.centerYAnchor),
            card.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 24),
            card.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -24),

            stack.topAnchor.constraint(equalTo: card.topAnchor, constant: 20),
            stack.bottomAnchor.constraint(equalTo: card.bottomAnchor, constant: -20),
            stack.leadingAnchor.constraint(equalTo: card.leadingAnchor, constant: 20),
            stack.trailingAnchor.constraint(equalTo: card.trailingAnchor, constant: -20),
        ])
    }
}
