import SwiftUI
import UserNotifications

@main
struct TomeApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate

    var body: some Scene {
        WindowGroup {
            RootView(pending: AppDelegate.pendingLink)
        }
    }
}

/// Carries a link from a tapped notification into the browser.
///
/// The share extension can't launch this app — Apple allows that only for Today
/// extensions and widgets, and the old responder-chain `openURL:` trick is
/// force-failed from iOS 18 on. A local notification is the sanctioned way
/// across, so a bare link shared from X arrives here as a notification tap.
@MainActor
final class PendingLink: ObservableObject {
    @Published var url: String?
}

final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    /// Static because the notification delegate is set before any SwiftUI view
    /// exists, and a tap can arrive during launch.
    @MainActor static let pendingLink = PendingLink()

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        return true
    }

    /// Fires when the user taps the "Open in Tome" notification the share
    /// extension posted.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let info = response.notification.request.content.userInfo
        if let link = info[TomeNotification.urlKey] as? String {
            Task { @MainActor in
                AppDelegate.pendingLink.url = link
            }
        }
        completionHandler()
    }
}

/// Two tabs, because the app has exactly two jobs: browse to an article the
/// share sheet can't reach, and hold the account settings.
///
/// The browser model lives here rather than inside BrowserView so an incoming
/// link — from a notification tap or a `tome://` URL — can drive it.
struct RootView: View {
    @ObservedObject var pending: PendingLink
    @StateObject private var browser = BrowserModel()
    @State private var tab = Tab.browse
    /// Gates the app on first run, until `/me` has confirmed a working setup.
    ///
    /// Evaluated once at launch, deliberately: editing credentials afterwards is
    /// Settings' job, and being thrown back to onboarding mid-edit — the moment
    /// you cleared a field — would be worse than a clear 401 on the next send.
    @State private var configured = TomeConfig.isConfigured

    private enum Tab: Hashable {
        case browse
        case settings
    }

    var body: some View {
        if configured {
            main
        } else {
            OnboardingView { configured = true }
        }
    }

    private var main: some View {
        TabView(selection: $tab) {
            BrowserView(model: browser)
                .tabItem { Label("Browse", systemImage: "safari") }
                .tag(Tab.browse)

            SettingsView()
                .tabItem { Label("Settings", systemImage: "gear") }
                .tag(Tab.settings)
        }
        .task {
            // Asked for on first launch because the share extension can't
            // request it — and without it, links shared from other apps have no
            // way to reach this app.
            _ = try? await UNUserNotificationCenter.current()
                .requestAuthorization(options: [.alert])
        }
        .onChange(of: pending.url) { _, link in
            guard let link else { return }
            open(link)
            pending.url = nil
        }
        .onOpenURL { incoming in
            // Kept as an escape hatch: the share extension can't use this, but
            // Shortcuts and other apps can open tome://article?url=…
            guard let article = Self.articleURL(from: incoming) else { return }
            open(article)
        }
    }

    /// Loading it in the app's WebView is the point: this is the browser holding
    /// the site logins, so a link no one else could read logged-out works here.
    private func open(_ link: String) {
        tab = .browse
        browser.go(to: link)
    }

    /// Unpacks `tome://article?url=…`, ignoring anything else.
    private static func articleURL(from url: URL) -> String? {
        guard
            url.scheme == "tome",
            url.host == "article",
            let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
            let target = components.queryItems?.first(where: { $0.name == "url" })?.value,
            !target.isEmpty
        else {
            return nil
        }
        return target
    }
}
