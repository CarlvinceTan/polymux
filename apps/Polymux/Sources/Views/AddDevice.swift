import CoreImage.CIFilterBuiltins
import SwiftUI
import PolymuxKit

/// Pairs another device: a QR code and link for this server, or a link pasted from another one.
struct AddDevice: View {
    let client: APIClient?
    let join: (URL) -> Void
    @Environment(\.panelClose) private var close
    @State private var invite: Invite?
    @State private var issue: String?
    @State private var pasted = ""
    @State private var pasteIssue: String?

    var body: some View {
        PanelView("Add device", onClose: { close() }) {
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
                        }
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 8)
                    } else if let issue {
                        Text(issue).font(.footnote).foregroundStyle(.red)
                        Button("Retry") { Task { await load() } }
                    } else if client == nil {
                        Text("Connect to a server first.").foregroundStyle(.secondary)
                    } else {
                        ProgressView().frame(maxWidth: .infinity)
                    }
                }
                .panelRow()
                Section("Join with a link") {
                    TextField("Pairing link", text: $pasted, prompt: Text("Paste a pairing link"))
                        .labelsHidden()
                        .accessibilityIdentifier("settings.link")
                        .lineLimit(1)
                        .autocorrectionDisabled()
                        .onSubmit(submit)
                        #if os(iOS)
                        .textInputAutocapitalization(.never)
                        #endif
                    if let pasteIssue {
                        Text(pasteIssue).font(.footnote).foregroundStyle(.red)
                    }
                }
                .panelRow()
            }
            .formStyle(.grouped)
        }
        .panelDismiss(onClose: { close() })
        .task(id: client?.baseURL) { await load() }
    }

    private func load() async {
        guard let client else { return }
        issue = nil
        do { invite = try await client.invite() }
        catch { issue = error.localizedDescription }
    }

    private func submit() {
        guard let url = URL(string: pasted.trimmingCharacters(in: .whitespacesAndNewlines)), Invite(link: url) != nil else {
            pasteIssue = "Paste a valid Polymux pairing link."
            return
        }
        pasteIssue = nil
        join(url)
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
