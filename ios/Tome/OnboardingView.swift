import SwiftUI

/// Shown instead of the app when there's no working configuration.
///
/// Tome is invite-only and self-hosted, so there is no sign-up — you arrive
/// either with an invite code or with a key someone gave you. Dropping a new
/// arrival straight into an empty browser would leave them with no idea why
/// nothing sends, so this gates the app until `/me` actually answers.
struct OnboardingView: View {
    /// Called once the server has confirmed the credentials.
    let onReady: () -> Void

    @State private var serverText = TomeConfig.serverURL?.absoluteString ?? ""
    @State private var method: Method = .apiKey

    @State private var apiKeyText = ""
    @State private var inviteCode = ""
    @State private var email = ""
    @State private var kindleEmail = ""

    @State private var busy = false
    @State private var problem: String?

    private enum Method: String, CaseIterable {
        case apiKey = "I have a key"
        case invite = "I have an invite"
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("https://tome.example.com", text: $serverText)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                } header: {
                    Text("Your Tome server")
                } footer: {
                    Text("The server someone runs for you — Tome is self-hosted, so there's no shared service to sign up for.")
                }

                Section {
                    Picker("Method", selection: $method) {
                        ForEach(Method.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                    }
                    .pickerStyle(.segmented)

                    switch method {
                    case .apiKey:
                        SecureField("tome_…", text: $apiKeyText)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                    case .invite:
                        TextField("Invite code", text: $inviteCode)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                        TextField("Your email", text: $email)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .keyboardType(.emailAddress)
                        TextField("Your Kindle email", text: $kindleEmail)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .keyboardType(.emailAddress)
                    }
                } footer: {
                    switch method {
                    case .apiKey:
                        Text("From `tome init-admin`, or from whoever invited you.")
                    case .invite:
                        Text("Your Kindle address is the one Amazon gave your device — it ends in @kindle.com.")
                    }
                }

                if let problem {
                    Section {
                        Label(problem, systemImage: "exclamationmark.triangle.fill")
                            .foregroundStyle(.orange)
                            .font(.footnote)
                    }
                }

                Section {
                    Button {
                        Task { await connect() }
                    } label: {
                        if busy {
                            HStack { ProgressView(); Text("Connecting…") }
                        } else {
                            Text("Connect")
                        }
                    }
                    .disabled(busy || !isComplete)
                }
            }
            .navigationTitle("Set up Tome")
        }
    }

    private var isComplete: Bool {
        guard !serverText.trimmingCharacters(in: .whitespaces).isEmpty else { return false }
        switch method {
        case .apiKey:
            return !apiKeyText.trimmingCharacters(in: .whitespaces).isEmpty
        case .invite:
            return ![inviteCode, email, kindleEmail].contains {
                $0.trimmingCharacters(in: .whitespaces).isEmpty
            }
        }
    }

    /// Stores the configuration only after `/me` confirms it, so the app is
    /// never left in a state that looks configured but can't send.
    private func connect() async {
        busy = true
        problem = nil
        defer { busy = false }

        var cleaned = serverText.trimmingCharacters(in: .whitespacesAndNewlines)
        if !cleaned.contains("://") { cleaned = "https://" + cleaned }
        while cleaned.hasSuffix("/") { cleaned.removeLast() }

        guard let url = URL(string: cleaned), url.host != nil else {
            problem = "That doesn't look like a server address."
            return
        }

        do {
            let key: String
            switch method {
            case .apiKey:
                key = apiKeyText.trimmingCharacters(in: .whitespacesAndNewlines)
            case .invite:
                key = try await TomeClient.acceptInvite(
                    serverURL: url,
                    code: inviteCode.trimmingCharacters(in: .whitespacesAndNewlines),
                    email: email.trimmingCharacters(in: .whitespacesAndNewlines),
                    kindleEmail: kindleEmail.trimmingCharacters(in: .whitespacesAndNewlines)
                )
            }

            // Prove it works before committing it to storage.
            _ = try await TomeClient(serverURL: url, apiKey: key).me()

            TomeConfig.serverURL = url
            TomeConfig.apiKey = key
            onReady()
        } catch let error as TomeError {
            problem = error.errorDescription
        } catch {
            problem = error.localizedDescription
        }
    }
}
