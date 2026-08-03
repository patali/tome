import SwiftUI
import WebKit

/// Wraps the model's WKWebView. The model owns the view (it has to configure it
/// before it exists), so this is just a mount point.
struct WebViewContainer: UIViewRepresentable {
    let webView: WKWebView

    func makeUIView(context: Context) -> WKWebView { webView }
    func updateUIView(_ webView: WKWebView, context: Context) {}
}

/// The in-app browser: an address bar, a page, and a send button.
///
/// Sign in to a site here once and the session persists, which is how this path
/// reaches articles the share sheet can't — X in particular, which serves no
/// content at all to logged-out readers.
struct BrowserView: View {
    /// Owned by RootView so an incoming `tome://` link can drive it.
    @ObservedObject var model: BrowserModel
    @FocusState private var addressFocused: Bool

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                addressBar
                statusBar
                WebViewContainer(webView: model.webView)
            }
            .navigationTitle("Browse")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItemGroup(placement: .bottomBar) {
                    Button(action: model.goBack) {
                        Image(systemName: "chevron.left")
                    }
                    .disabled(!model.canGoBack)

                    Button(action: model.goForward) {
                        Image(systemName: "chevron.right")
                    }
                    .disabled(!model.canGoForward)

                    Button(action: model.reload) {
                        Image(systemName: "arrow.clockwise")
                    }

                    Spacer()

                    Button {
                        Task { await model.preparePreview() }
                    } label: {
                        Label("Preview", systemImage: "doc.text.magnifyingglass")
                    }
                    .disabled(isSending || model.isLoading)

                    Button {
                        Task { await model.sendToKindle() }
                    } label: {
                        Label("Send to Kindle", systemImage: "paperplane.fill")
                    }
                    .disabled(isSending || model.isLoading)
                }
            }
            .sheet(item: $model.previewSubject) { subject in
                PreviewView(subject: subject)
            }
        }
    }

    private var addressBar: some View {
        HStack(spacing: 8) {
            TextField("Search or enter address", text: $model.addressText)
                .textFieldStyle(.roundedBorder)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
                .submitLabel(.go)
                .focused($addressFocused)
                .onSubmit {
                    addressFocused = false
                    model.go(to: model.addressText)
                }

            if model.isLoading {
                ProgressView()
            }
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
    }

    @ViewBuilder
    private var statusBar: some View {
        switch model.status {
        case .idle:
            EmptyView()
        case .working(let message):
            statusRow(message, systemImage: "arrow.triangle.2.circlepath", tint: .secondary)
        case .sent(let message):
            statusRow(message, systemImage: "checkmark.circle.fill", tint: .green)
        case .failed(let message):
            statusRow(message, systemImage: "exclamationmark.triangle.fill", tint: .orange)
        }
    }

    private func statusRow(_ message: String, systemImage: String, tint: Color) -> some View {
        HStack(spacing: 8) {
            Image(systemName: systemImage).foregroundStyle(tint)
            Text(message)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(2)
            Spacer()
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
    }

    private var isSending: Bool {
        if case .working = model.status { return true }
        return false
    }
}
