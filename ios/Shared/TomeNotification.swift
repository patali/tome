import Foundation

/// The one contract between the share extension and the app.
///
/// A share extension cannot launch its containing app — Apple restricts that to
/// Today extensions and widgets, and the responder-chain `openURL:` workaround
/// is force-failed from iOS 18 on. A local notification is the documented way
/// across, so this is how a bare link (shared from X, Reddit, Messages) travels:
/// the extension posts, the app reads it back on tap.
///
/// It's a shared file rather than a duplicated string literal because a typo on
/// one side would fail silently — the notification would arrive and simply do
/// nothing, which is exactly the failure that's hardest to diagnose.
enum TomeNotification {
    /// Key under which the article URL rides in the notification's userInfo.
    static let urlKey = "tome.article.url"

    /// Identifier prefix, so a stale notification is easy to recognise.
    static let categoryIdentifier = "tome.open-article"
}
