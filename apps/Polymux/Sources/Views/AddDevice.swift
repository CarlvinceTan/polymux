import CoreImage.CIFilterBuiltins
import SwiftUI
import PolymuxKit

/// Pairs another device: a QR code and link for this server, or a link pasted from another one.
struct AddDevice: View {
    let client: APIClient?
    let onBack: () -> Void
    let join: (URL) async throws -> Void
    @Environment(\.panelClose) private var close
    @State private var invite: Invite?
    @State private var issue: String?
    @State private var pasted = ""
    @State private var pasteIssue: String?
    @State private var joining = false
    @State private var joinTask: Task<Void, Never>?

    var body: some View {
        PanelView("Add device", onClose: { close() }, onBack: onBack) {
            Form {
                Section {
                    if let invite {
                        VStack(spacing: 16) {
                            QRCode(text: invite.link.absoluteString)
                                .frame(width: 220, height: 220)
                                .accessibilityLabel("Pairing QR code")
                            HStack(spacing: 10) {
                                ShareLink(item: invite.link) { Label("Share", systemImage: "square.and.arrow.up") }
                                Button("Copy link", systemImage: "doc.on.doc") { copy(invite.link) }
                            }
                            .buttonStyle(.glass)
                            Text("Single-use link. Expires in 3 minutes.")
                                .font(.footnote).foregroundStyle(.muted)
                        }
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 8)
                    } else if let issue {
                        Text(issue).font(.footnote).foregroundStyle(.muted).frame(maxWidth: .infinity)
                    } else if client == nil {
                        Text("Connect to a server first.").foregroundStyle(.muted).frame(maxWidth: .infinity)
                    } else {
                        ProgressView().frame(maxWidth: .infinity)
                    }
                }
                .panelRow()
                Section("Join with a link") {
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
                        .accessibilityLabel("Join server")
                    if let pasteIssue {
                        Text(pasteIssue).font(.footnote).foregroundStyle(.muted)
                    } else if joining {
                        Text("Connecting…").font(.footnote).foregroundStyle(.muted)
                    }
                }
                .panelRow()
            }
            .formStyle(.grouped)
        }
        .panelDismiss(onClose: { close() })
        .onDisappear { joinTask?.cancel() }
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
        catch { issue = error.isOffline ? "Offline" : "Couldn’t create a pairing code." }
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
            do { try await join(url) }
            catch is CancellationError {}
            catch {
                pasteIssue = error.isOffline ? "Can’t reach this server. Check its connection and try the link again." : error.reason(or: "Couldn’t pair with this server.")
            }
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
