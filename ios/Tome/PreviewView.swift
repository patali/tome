import PDFKit
import SwiftUI

/// Shows the actual rendered PDF — not an approximation of it.
///
/// The extension's reader previews HTML styled with reader.css, which is close
/// but can't show where pages break. This asks the server for the real document
/// via `/convert`, so what's on screen is literally what lands on the device,
/// at the true page geometry.
///
/// The cost is a server round trip per change (headless Chrome on the far end),
/// so each control change shows a spinner rather than updating live.
struct PreviewView: View {
    let subject: ExtractionResult

    @Environment(\.dismiss) private var dismiss

    // Seeded from the saved defaults, then local to this preview: you can look
    // at a page as Paperwhite without changing what you normally send.
    @State private var device = TomeConfig.device
    @State private var font = TomeConfig.font
    @State private var color = TomeConfig.color

    @State private var document: PDFDocument?
    @State private var phase: Phase = .rendering
    @State private var renderTask: Task<Void, Never>?
    /// The rendered PDF on disk, so it can be exported. This is the iOS
    /// counterpart of the extension reader's "Save as PDF".
    @State private var exportURL: URL?

    private enum Phase: Equatable {
        case rendering
        case ready
        case failed(String)
        case sending
        case sent(String)
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                content
                controls
            }
            .navigationTitle(subject.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { dismiss() }
                }
                ToolbarItem(placement: .topBarLeading) {
                    if let exportURL {
                        ShareLink(item: exportURL) {
                            Label("Save PDF", systemImage: "square.and.arrow.up")
                        }
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        Task { await send() }
                    } label: {
                        Label("Send to Kindle", systemImage: "paperplane.fill")
                    }
                    .disabled(phase == .rendering || phase == .sending)
                }
            }
        }
        .task { await render() }
        .onDisappear { renderTask?.cancel() }
    }

    // MARK: - Content

    @ViewBuilder
    private var content: some View {
        switch phase {
        case .rendering:
            centered {
                ProgressView()
                Text("Rendering \(deviceLabel)…")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        case .failed(let message):
            centered {
                Image(systemName: "exclamationmark.triangle.fill")
                    .foregroundStyle(.orange)
                Text(message)
                    .font(.footnote)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
                Button("Try again") { Task { await render() } }
            }
        case .ready, .sending, .sent:
            if let document {
                PDFDocumentView(document: document)
            } else {
                centered { ProgressView() }
            }
        }
    }

    private func centered<Content: View>(
        @ViewBuilder _ inner: () -> Content
    ) -> some View {
        VStack(spacing: 10) { inner() }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .padding()
    }

    // MARK: - Controls

    private var controls: some View {
        VStack(spacing: 12) {
            if case .sent(let message) = phase {
                Label(message, systemImage: "checkmark.circle.fill")
                    .font(.footnote)
                    .foregroundStyle(.green)
            } else if phase == .sending {
                Label("Sending…", systemImage: "paperplane")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }

            Picker("Device", selection: $device) {
                ForEach(TomeConfig.devices, id: \.key) { Text($0.label).tag($0.key) }
            }
            .pickerStyle(.segmented)

            Picker("Images", selection: $color) {
                Text("B&W").tag("bw")
                Text("Colour").tag("color")
            }
            .pickerStyle(.segmented)

            // Six faces don't fit a segmented control legibly, so this is the
            // one that stays a menu.
            Picker("Body font", selection: $font) {
                ForEach(TomeConfig.fonts, id: \.key) { Text($0.label).tag($0.key) }
            }
            .pickerStyle(.menu)
            .frame(maxWidth: .infinity, alignment: .leading)

            Text("Extracted by \(subject.extractor)")
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
        .padding(.horizontal)
        .padding(.vertical, 10)
        .background(.bar)
        .onChange(of: device) { _, _ in Task { await render() } }
        .onChange(of: font) { _, _ in Task { await render() } }
        .onChange(of: color) { _, _ in Task { await render() } }
    }

    private var deviceLabel: String {
        TomeConfig.devices.first { $0.key == device }?.label ?? device
    }

    // MARK: - Work

    /// Re-renders, replacing any in-flight render so rapid control changes don't
    /// queue up several Chrome runs on the server.
    private func render() async {
        renderTask?.cancel()
        let task = Task {
            phase = .rendering
            do {
                let client = try TomeClient.fromConfig()
                let article = subject.article(device: device, font: font, color: color)
                let data = try await client.convert(article)
                guard !Task.isCancelled else { return }
                guard let parsed = PDFDocument(data: data) else {
                    phase = .failed("The server returned something that isn't a PDF.")
                    return
                }
                document = parsed
                exportURL = Self.writeForExport(data, title: subject.title)
                phase = .ready
            } catch is CancellationError {
                return
            } catch let error as TomeError {
                guard !Task.isCancelled else { return }
                phase = .failed(error.errorDescription ?? "Couldn't render this article.")
            } catch {
                guard !Task.isCancelled else { return }
                phase = .failed(error.localizedDescription)
            }
        }
        renderTask = task
        await task.value
    }

    /// ShareLink needs a file on disk, and the filename is what the share sheet
    /// shows — so name it after the article rather than leaving a UUID.
    ///
    /// Mirrors the sanitising in article.BaseName() server-side: strip only what
    /// is illegal in a filename, keep spaces and capitals.
    private static func writeForExport(_ data: Data, title: String) -> URL? {
        let illegal = CharacterSet(charactersIn: "/\\:*?\"<>|").union(.controlCharacters)
        var name = title.components(separatedBy: illegal).joined()
        name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        if name.isEmpty { name = "Article" }
        if name.count > 120 { name = String(name.prefix(120)) }

        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent(name)
            .appendingPathExtension("pdf")
        do {
            try data.write(to: url, options: .atomic)
            return url
        } catch {
            // Export is a convenience; losing it shouldn't break the preview.
            return nil
        }
    }

    private func send() async {
        phase = .sending
        do {
            let client = try TomeClient.fromConfig()
            let article = subject.article(device: device, font: font, color: color)
            let sent = try await client.sendToKindle(article)
            phase = .sent(sent.sentTo.map { "Sent to \($0)" } ?? "Sent to your Kindle")
        } catch let error as TomeError {
            phase = .failed(error.errorDescription ?? "Couldn't send this article.")
        } catch {
            phase = .failed(error.localizedDescription)
        }
    }
}

/// PDFKit's viewer. `autoScales` is what makes a 157×210 mm page legible on a
/// phone without manual zooming.
private struct PDFDocumentView: UIViewRepresentable {
    let document: PDFDocument

    func makeUIView(context: Context) -> PDFView {
        let view = PDFView()
        view.autoScales = true
        view.displayMode = .singlePageContinuous
        view.displayDirection = .vertical
        view.backgroundColor = .secondarySystemBackground
        view.document = document
        return view
    }

    func updateUIView(_ view: PDFView, context: Context) {
        // Identity check, not equality: PDFDocument has no cheap comparison, and
        // reassigning on every layout pass would reset the scroll position.
        if view.document !== document {
            view.document = document
            view.autoScales = true
        }
    }
}
