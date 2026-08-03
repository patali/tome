import Foundation

/// Failures worth showing a person, phrased as sentences rather than codes.
enum TomeError: LocalizedError {
    case notConfigured
    case extraction(String)
    case unauthorized
    case server(String)
    case network(String)
    case notAWebPage

    var errorDescription: String? {
        switch self {
        case .notConfigured:
            return "Tome isn't set up yet. Open the app and add your server and API key."
        case .extraction(let message):
            return message
        case .unauthorized:
            // Mirrors the extension's wording for the same 401.
            return "That API key was rejected by the server."
        case .server(let message):
            return message
        case .network(let message):
            return "Couldn't reach the server: \(message)"
        case .notAWebPage:
            return "Nothing shareable came through. Try sharing from Safari."
        }
    }
}

/// What `/send-to-kindle` returns on success.
struct SendResult: Decodable {
    let ok: Bool
    let sentTo: String?
    let filename: String?
    let bytes: Int?
}

/// What `/me` returns — used to validate a key and show who you're signed in as.
struct MeResult: Decodable {
    let email: String
    let kindleEmail: String
    let isAdmin: Bool
    let resendConfigured: Bool
    let approvedSender: String?
}

/// Talks to the Tome server. Deliberately mirrors `deliver()` in
/// extension/background.js so both clients behave the same way — same endpoint,
/// same Bearer header, same error handling.
struct TomeClient {
    let serverURL: URL
    let apiKey: String

    /// Builds a client from stored configuration, or fails if it's incomplete.
    static func fromConfig() throws -> TomeClient {
        guard let url = TomeConfig.serverURL, let key = TomeConfig.apiKey else {
            throw TomeError.notConfigured
        }
        return TomeClient(serverURL: url, apiKey: key)
    }

    /// Rendering runs headless Chrome on the server and then sends mail, which
    /// on a Raspberry Pi is comfortably slower than URLSession's default. Give
    /// it room rather than surfacing a timeout the server would have satisfied.
    private static let sendTimeout: TimeInterval = 120

    private func request(
        _ path: String,
        method: String,
        timeout: TimeInterval,
        query: [URLQueryItem] = []
    ) -> URLRequest {
        var url = serverURL.appendingPathComponent(path)
        if !query.isEmpty,
           var components = URLComponents(url: url, resolvingAgainstBaseURL: false) {
            components.queryItems = query
            url = components.url ?? url
        }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.timeoutInterval = timeout
        return req
    }

    /// Renders the article and returns the document bytes without sending it
    /// anywhere. This is what the preview shows — the real PDF, at the real page
    /// geometry, so pagination on screen is what lands on the device.
    func convert(_ article: Article, format: String = "pdf") async throws -> Data {
        var req = request(
            "convert",
            method: "POST",
            timeout: Self.sendTimeout,
            query: [URLQueryItem(name: "format", value: format)]
        )
        req.httpBody = try JSONEncoder().encode(article)
        return try await perform(req)
    }

    /// Redeems an invite and returns the API key. Unauthenticated by nature —
    /// it's how you get a key in the first place — so it's static.
    static func acceptInvite(
        serverURL: URL,
        code: String,
        email: String,
        kindleEmail: String
    ) async throws -> String {
        var req = URLRequest(url: serverURL.appendingPathComponent("auth/accept-invite"))
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.timeoutInterval = 30
        req.httpBody = try JSONSerialization.data(withJSONObject: [
            "code": code, "email": email, "kindleEmail": kindleEmail,
        ])

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await URLSession.shared.data(for: req)
        } catch {
            throw TomeError.network(error.localizedDescription)
        }
        guard let http = response as? HTTPURLResponse else {
            throw TomeError.server("The server sent an unexpected response.")
        }
        guard (200..<300).contains(http.statusCode) else {
            throw TomeError.server(
                errorMessage(from: data) ?? "The server returned \(http.statusCode)."
            )
        }
        guard
            let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            let key = object["apiKey"] as? String, !key.isEmpty
        else {
            throw TomeError.server("The server didn't return an API key.")
        }
        return key
    }

    /// Renders the article and mails it to the account's Kindle address.
    func sendToKindle(_ article: Article) async throws -> SendResult {
        var req = request("send-to-kindle", method: "POST", timeout: Self.sendTimeout)
        req.httpBody = try JSONEncoder().encode(article)
        let data = try await perform(req)
        do {
            return try JSONDecoder().decode(SendResult.self, from: data)
        } catch {
            // The send may well have succeeded; only the response shape surprised
            // us. Say so rather than implying the article was lost.
            throw TomeError.server("Sent, but the server's reply wasn't understood.")
        }
    }

    /// Validates the API key and returns the account. Used by Settings.
    func me() async throws -> MeResult {
        let data = try await perform(request("me", method: "GET", timeout: 30))
        do {
            return try JSONDecoder().decode(MeResult.self, from: data)
        } catch {
            throw TomeError.server("The server's reply wasn't understood.")
        }
    }

    /// Shared transport: maps status codes onto TomeError the way the extension
    /// does, preferring the server's own `{"error": …}` text when there is one.
    private func perform(_ req: URLRequest) async throws -> Data {
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await URLSession.shared.data(for: req)
        } catch {
            throw TomeError.network(error.localizedDescription)
        }

        guard let http = response as? HTTPURLResponse else {
            throw TomeError.server("The server sent an unexpected response.")
        }
        if http.statusCode == 401 {
            throw TomeError.unauthorized
        }
        guard (200..<300).contains(http.statusCode) else {
            let message = Self.errorMessage(from: data)
                ?? "The server returned \(http.statusCode)."
            throw TomeError.server(message)
        }
        return data
    }

    /// The server reports failures as `{"error": "..."}` (see api.errJSON).
    private static func errorMessage(from data: Data) -> String? {
        guard
            let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            let message = object["error"] as? String,
            !message.isEmpty
        else {
            return nil
        }
        return message
    }
}
