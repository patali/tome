import SwiftUI

struct SettingsView: View {
    @State private var serverText = TomeConfig.serverURL?.absoluteString ?? ""
    @State private var apiKeyText = TomeConfig.apiKey ?? ""
    @State private var device = TomeConfig.device
    @State private var font = TomeConfig.font
    @State private var color = TomeConfig.color

    @State private var account: MeResult?
    @State private var checkState: CheckState = .idle

    private enum CheckState: Equatable {
        case idle
        case checking
        case failed(String)
    }

    var body: some View {
        NavigationStack {
            Form {
                serverSection
                accountSection
                readingSection
                shareSheetSection
            }
            .navigationTitle("Settings")
        }
    }

    // MARK: - Sections

    private var serverSection: some View {
        Section {
            // Committed on submit rather than per keystroke: persisting every
            // intermediate value would briefly store half-typed credentials and,
            // worse, leave them stored if the app were killed mid-edit.
            TextField("https://tome.example.com", text: $serverText)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
                .submitLabel(.done)
                .onSubmit(commit)

            SecureField("tome_…", text: $apiKeyText)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.done)
                .onSubmit(commit)

            Button {
                Task { await verify() }
            } label: {
                if checkState == .checking {
                    HStack { ProgressView(); Text("Checking…") }
                } else {
                    Text("Check connection")
                }
            }
            .disabled(checkState == .checking || serverText.isEmpty || apiKeyText.isEmpty)
        } header: {
            Text("Server")
        } footer: {
            Text("Your Tome server and the API key from your invite.")
        }
    }

    @ViewBuilder
    private var accountSection: some View {
        if let account {
            Section("Account") {
                LabeledContent("Signed in as", value: account.email)
                LabeledContent("Kindle", value: account.kindleEmail)
                if !account.resendConfigured {
                    Label(
                        "Email delivery isn't configured on the server, so sending will fail.",
                        systemImage: "exclamationmark.triangle.fill"
                    )
                    .foregroundStyle(.orange)
                    .font(.footnote)
                }
                if let sender = account.approvedSender, !sender.isEmpty {
                    Text("Add \(sender) to your Approved Personal Document E-mail List in your Amazon account, or deliveries will be dropped.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        } else if case .failed(let message) = checkState {
            Section {
                Label(message, systemImage: "xmark.circle.fill")
                    .foregroundStyle(.red)
                    .font(.footnote)
            }
        }
    }

    private var readingSection: some View {
        Section {
            Picker("Device", selection: $device) {
                ForEach(TomeConfig.devices, id: \.key) { Text($0.label).tag($0.key) }
            }
            .onChange(of: device) { _, new in TomeConfig.device = new }

            Picker("Body font", selection: $font) {
                ForEach(TomeConfig.fonts, id: \.key) { Text($0.label).tag($0.key) }
            }
            .onChange(of: font) { _, new in TomeConfig.font = new }

            Picker("Images", selection: $color) {
                Text("Black & white").tag("bw")
                Text("Colour").tag("color")
            }
            .onChange(of: color) { _, new in TomeConfig.color = new }
        } header: {
            Text("Reading")
        } footer: {
            Text("Page size matches your device exactly, so the PDF renders 1:1 with no magnification.")
        }
    }

    /// The single most confusing thing about this build, stated plainly rather
    /// than left to be discovered. See TomeConfig for why.
    private var shareSheetSection: some View {
        Section {
            Text("The Safari share sheet uses the server and key built into the app, not the ones above. Changing them here affects this app only — rebuild from Xcode to change what the share sheet uses.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        } header: {
            Text("Share sheet")
        }
    }

    // MARK: - Actions

    /// Normalises and stores what's been typed.
    private func commit() {
        var cleaned = serverText.trimmingCharacters(in: .whitespacesAndNewlines)
        if !cleaned.isEmpty, !cleaned.contains("://") { cleaned = "https://" + cleaned }
        while cleaned.hasSuffix("/") { cleaned.removeLast() }
        serverText = cleaned

        TomeConfig.serverURL = cleaned.isEmpty ? nil : URL(string: cleaned)
        TomeConfig.apiKey = apiKeyText.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func verify() async {
        // Whatever is on screen is what should be tested, even if the field was
        // never submitted.
        commit()
        checkState = .checking
        account = nil
        do {
            let client = try TomeClient.fromConfig()
            account = try await client.me()
            checkState = .idle
        } catch let error as TomeError {
            checkState = .failed(error.errorDescription ?? "Couldn't reach the server.")
        } catch {
            checkState = .failed(error.localizedDescription)
        }
    }
}
