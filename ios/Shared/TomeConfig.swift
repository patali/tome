import Foundation

/// Where the app's server URL, API key, and reading preferences come from.
///
/// This type exists to quarantine one constraint: on a free Apple ID, **App
/// Groups and Keychain Sharing are unavailable**, so the Share Extension and
/// the container app cannot share storage. That has a specific consequence:
///
///   - The container app can persist changes (UserDefaults) and does.
///   - The Share Extension cannot see them, so it falls back to values compiled
///     in from `Config.xcconfig` via Info.plist.
///
/// So changing the API key in Settings updates the app but NOT the share sheet;
/// the share sheet keeps using the built-in key until the next build. That is a
/// real limitation, not an oversight, and it disappears with a paid membership.
///
/// When that day comes, the migration is confined to this file: point `defaults`
/// at `UserDefaults(suiteName:)` for the App Group and move `apiKey` into a
/// shared Keychain. Nothing else in the app reads configuration directly.
enum TomeConfig {

    // MARK: - Storage

    /// The container app's own defaults. Not visible to the extension.
    private static let defaults = UserDefaults.standard

    private enum Key {
        static let serverURL = "tome.serverURL"
        static let apiKey = "tome.apiKey"
        static let device = "tome.device"
        static let font = "tome.font"
        static let color = "tome.color"
    }

    /// Values baked in at build time from `Config.xcconfig`. These are the only
    /// configuration the Share Extension ever sees.
    private static func bundleValue(_ key: String) -> String? {
        guard let raw = Bundle.main.object(forInfoDictionaryKey: key) as? String else {
            return nil
        }
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    // MARK: - Server

    static var serverURL: URL? {
        get {
            let raw = defaults.string(forKey: Key.serverURL)
                ?? bundleValue("TOMEServerURL")
                ?? ""
            // Trailing slashes are trimmed for the same reason background.js
            // does it: every call site appends "/path" and would otherwise
            // produce a double slash.
            var cleaned = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            while cleaned.hasSuffix("/") { cleaned.removeLast() }
            return cleaned.isEmpty ? nil : URL(string: cleaned)
        }
        set { defaults.set(newValue?.absoluteString ?? "", forKey: Key.serverURL) }
    }

    /// Bearer token for the Tome server. Sent as `Authorization: Bearer …`, and
    /// the server requires the `tome_` prefix (see auth/middleware.go).
    static var apiKey: String? {
        get {
            let raw = defaults.string(forKey: Key.apiKey)
                ?? bundleValue("TOMEAPIKey")
                ?? ""
            let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            return trimmed.isEmpty ? nil : trimmed
        }
        set { defaults.set(newValue ?? "", forKey: Key.apiKey) }
    }

    /// True when there is enough configuration to attempt a send.
    static var isConfigured: Bool { serverURL != nil && apiKey != nil }

    // MARK: - Reading preferences

    /// Kindle model, which drives the PDF page geometry server-side. Must match
    /// a key in pdfgen.devices — an unknown value silently falls back to
    /// "scribe" there, which would be a confusing way to discover a typo.
    static var device: String {
        get { defaults.string(forKey: Key.device) ?? "scribe" }
        set { defaults.set(newValue, forKey: Key.device) }
    }

    /// Body face. Must match a key in pdfgen.BodyFonts.
    static var font: String {
        get { defaults.string(forKey: Key.font) ?? "literata" }
        set { defaults.set(newValue, forKey: Key.font) }
    }

    /// "bw" (e-ink default) or "color".
    static var color: String {
        get { defaults.string(forKey: Key.color) ?? "bw" }
        set { defaults.set(newValue, forKey: Key.color) }
    }

    // MARK: - Choices, for pickers

    /// Mirrors pdfgen.devices. Sizes are the physical screen dimensions the
    /// server renders 1:1 against.
    static let devices: [(key: String, label: String)] = [
        ("scribe", "Kindle Scribe (10.2\")"),
        ("scribe3", "Kindle Scribe 3 (11\")"),
        ("paperwhite", "Paperwhite (6.8\")"),
    ]

    /// Mirrors pdfgen.BodyFonts.
    static let fonts: [(key: String, label: String)] = [
        ("literata", "Literata"),
        ("sourceserif", "Source Serif 4"),
        ("merriweather", "Merriweather"),
        ("baskerville", "Libre Baskerville"),
        ("inter", "Inter"),
        ("atkinson", "Atkinson Hyperlegible"),
    ]

    // MARK: - Bundled resources

    /// The e-ink stylesheet, copied from extension/reader.css at build time.
    ///
    /// Shipping it in the payload is what makes the PDF match the desktop
    /// extension's output: pdfgen prefers the stylesheet in the request and only
    /// falls back to its own deliberately-compact approximation when none is
    /// sent (see pdfgen.buildHTML).
    static var readerCSS: String {
        guard let url = Bundle.main.url(forResource: "reader", withExtension: "css"),
              let css = try? String(contentsOf: url, encoding: .utf8)
        else {
            // Not fatal: the server still renders, just with its fallback CSS.
            return ""
        }
        return css
    }

    /// The concatenated extractor bundle, for the WebView path. The Share
    /// Extension does not read this — iOS loads it itself via
    /// NSExtensionJavaScriptPreprocessingFile.
    static var extractionJS: String? {
        guard let url = Bundle.main.url(forResource: "TomeExtraction", withExtension: "js"),
              let js = try? String(contentsOf: url, encoding: .utf8)
        else {
            return nil
        }
        return js
    }
}
