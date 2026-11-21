import CoreImage.CIFilterBuiltins
import SwiftUI
import PolymuxKit

/// Pairs a device: a QR code and link for this network, or joins another with a pasted link.
struct AddDevice: View {
    let client: APIClient?
    let onBack: () -> Void
    /// Joins another network; nil shows this network's invite.
    var join: ((URL) async throws -> Void)? = nil
    @Environment(\.panelClose) private var close
    @State private var pasted = ""
    @State private var pasteIssue: String?
    @State private var joining = false
    @State private var joinTask: Task<Void, Never>?
    @State private var scanning = false

    var body: some View {
        PanelView(join == nil ? "Add device" : "Join network", onClose: { close() }, onBack: onBack) {
            Form {
                if join == nil { inviteSection } else { joinSection }
            }
            .formStyle(.grouped)
        }
        .panelDismiss(onClose: { close() })
        #if os(iOS)
        .fullScreenCover(isPresented: $scanning) {
            QRScanner { text in
                scanning = false
                pasted = text.absoluteString
                submit()
            }
        }
        #endif
        .onDisappear { joinTask?.cancel() }
    }

    private var inviteSection: some View {
                Section {
                    InviteCode(client: client)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 8)
                }
                .panelRow()
    }

    private var joinSection: some View {
                Section("Join with a link") {
                    #if os(iOS)
                    if QRScanner.available {
                        Button("Scan QR code", systemImage: "qrcode.viewfinder") { scanning = true }
                            .disabled(joining)
                    }
                    #endif
                    TextField("Pairing link", text: $pasted, prompt: Text("Paste a pairing link"))
                        .labelsHidden()
                        .lineLimit(1)
                        .autocorrectionDisabled()
                        .onSubmit(submit)
                        .disabled(joining)
                        #if os(iOS)
                        .textInputAutocapitalization(.never)
                        #endif
                    Button("Join", action: submit)
                        .disabled(joining || pasted.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                        .accessibilityLabel("Join network")
                    if let pasteIssue {
                        Text(pasteIssue).font(.footnote).foregroundStyle(.muted)
                    } else if joining {
                        Text("Connecting…").font(.footnote).foregroundStyle(.muted)
                    }
                }
                .panelRow()
    }

    private func submit() {
        guard !joining else { return }
        guard let url = URL(string: pasted.trimmingCharacters(in: .whitespacesAndNewlines)), Invite(link: url) != nil else {
            pasteIssue = "Paste a valid Polymux pairing link."
            return
        }
        pasteIssue = nil
        joining = true
        joinTask = Task {
            defer { joining = false }
            do { try await join?(url) }
            catch is CancellationError {}
            catch {
                pasteIssue = error.isOffline ? "Can’t reach this server. Check its connection and try the link again." : error.reason(or: "Couldn’t pair with this server.")
            }
        }
    }
}

/// This network's pairing QR code and link, renewed before it expires.
struct InviteCode: View {
    let client: APIClient?
    var size: CGFloat = 220
    /// Puts the link actions beside the code instead of under it.
    var beside = false
    @State private var invite: Invite?
    @State private var issue: String?

    var body: some View {
        Group {
            if let invite {
                let layout = beside ? AnyLayout(HStackLayout(spacing: 20)) : AnyLayout(VStackLayout(spacing: 16))
                layout {
                    QRCode(text: invite.link.absoluteString)
                        .frame(width: size, height: size)
                        .accessibilityLabel("Pairing QR code")
                    VStack(alignment: beside ? .leading : .center, spacing: 12) {
                        HStack(spacing: 10) {
                            ShareLink(item: invite.shareLink) { Label("Share", systemImage: "square.and.arrow.up") }
                            Button("Copy link", systemImage: "doc.on.doc") { copy(invite.shareLink) }
                        }
                        .buttonStyle(.glass)
                        Text("Single-use link. Expires in 3 minutes.")
                            .font(.footnote).foregroundStyle(.muted)
                    }
                }
            } else if let issue {
                Text(issue).font(.footnote).foregroundStyle(.muted)
            } else if client == nil {
                Text("Connect to a server first.").foregroundStyle(.muted)
            } else {
                ProgressView()
            }
        }
        .task(id: client?.id) {
            // Tries again on its own until a code is ready.
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(invite == nil ? 5 : 120))
            }
        }
    }

    private func load() async {
        guard let client else { return }
        do { invite = try await client.invite(); issue = nil }
        catch is CancellationError {}
        catch let error as URLError where error.code == .cancelled {}
        catch {
            var text = error.isOffline ? "Offline" : error.reason(or: "Couldn’t create a pairing code.")
            if case let .response(status, _)? = error as? ClientError, status >= 500 { text += " (HTTP \(status))" }
            issue = text
        }
    }

    private func copy(_ link: URL) {
        #if os(macOS)
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(link.absoluteString, forType: .string)
        #else
        UIPasteboard.general.url = link
        #endif
    }
}

#if os(iOS)
import VisionKit

/// Reads a pairing QR code with the camera.
struct QRScanner: View {
    let found: (URL) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var failed = false

    static var available: Bool { DataScannerViewController.isSupported }

    var body: some View {
        ZStack(alignment: .topTrailing) {
            if failed {
                ContentUnavailableView("Camera unavailable", systemImage: "camera",
                                       description: Text("Check camera access in Settings or paste the pairing link."))
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                Scanner(found: found, failed: { failed = true }).ignoresSafeArea()
            }
            Button("Close scanner", systemImage: "xmark") { dismiss() }
                .labelStyle(.iconOnly)
                .buttonStyle(.glass)
                .padding()
        }
    }

    private struct Scanner: UIViewControllerRepresentable {
        let found: (URL) -> Void
        let failed: () -> Void

        func makeUIViewController(context: Context) -> DataScannerViewController {
            let scanner = DataScannerViewController(recognizedDataTypes: [.barcode(symbologies: [.qr])],
                                                    isHighlightingEnabled: true)
            scanner.delegate = context.coordinator
            do { try scanner.startScanning() }
            catch { Task { @MainActor in failed() } }
            return scanner
        }

        func updateUIViewController(_ controller: DataScannerViewController, context: Context) {}
        func makeCoordinator() -> Coordinator { Coordinator(found: found, failed: failed) }

        final class Coordinator: NSObject, DataScannerViewControllerDelegate {
            let found: (URL) -> Void
            let failed: () -> Void
            private var done = false
            init(found: @escaping (URL) -> Void, failed: @escaping () -> Void) {
                self.found = found
                self.failed = failed
            }

            func dataScanner(_ scanner: DataScannerViewController, becameUnavailableWithError error: DataScannerViewController.ScanningUnavailable) {
                failed()
            }

            func dataScanner(_ scanner: DataScannerViewController, didAdd items: [RecognizedItem], allItems: [RecognizedItem]) {
                guard !done else { return }
                for case .barcode(let code) in items {
                    if let text = code.payloadStringValue, let url = URL(string: text), Invite(link: url) != nil {
                        done = true
                        scanner.stopScanning()
                        found(url)
                        return
                    }
                }
            }
        }
    }
}
#endif

/// A crisp QR code on a white quiet zone, so cameras read it in both themes.
private struct QRCode: View {
    let text: String

    var body: some View {
        if let image = Self.render(text) {
            Image(decorative: image, scale: 1)
                .interpolation(.none)
                .resizable()
                .padding(12)
                .background(.white, in: RoundedRectangle(cornerRadius: 12))
        }
    }

    private static func render(_ text: String) -> CGImage? {
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(text.utf8)
        filter.correctionLevel = "M"
        guard let output = filter.outputImage else { return nil }
        return CIContext().createCGImage(output, from: output.extent)
    }
}
