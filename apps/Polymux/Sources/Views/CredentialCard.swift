import SwiftUI
import PolymuxKit

struct CredentialCard: View {
    let request: CredentialRequest
    let client: APIClient
    let message: Message
    let bubble: Color
    @State private var value = ""
    @State private var status: String?
    @State private var saving = false
    @State private var error: String?

    private var isKey: Bool { request.kind == "ssh" }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label(request.label, systemImage: isKey ? "key.horizontal" : "key")
                .fixedSize(horizontal: false, vertical: true)
            ForEach([request.origin] + (request.origins ?? []), id: \.self) { origin in
                Text(origin).font(.caption).foregroundStyle(.muted)
            }
            if let lifetime {
                Text(lifetime).font(.caption).foregroundStyle(.muted)
            }
            if (status ?? request.status) == "pending" {
                if isKey {
                    TextEditor(text: $value)
                        .font(.caption.monospaced())
                        .scrollContentBackground(.hidden)
                        .frame(minHeight: 72, maxHeight: 120)
                        .padding(6)
                        .background(.quaternary, in: RoundedRectangle(cornerRadius: 6))
                        .overlay(alignment: .topLeading) {
                            if value.isEmpty {
                                Text("Private key")
                                    .font(.caption.monospaced())
                                    .foregroundStyle(.muted)
                                    .padding(.horizontal, 11)
                                    .padding(.vertical, 6)
                                    .allowsHitTesting(false)
                            }
                        }
                        .disabled(saving)
                        .accessibilityLabel("SSH private key")
                } else {
                    SecureField("Credential", text: $value)
                        .textFieldStyle(.roundedBorder)
                        .autocorrectionDisabled()
                        .onSubmit { Task { await submit() } }
                        .disabled(saving)
                        .accessibilityLabel("Credential value")
                }
                HStack(spacing: 12) {
                    Button("Save") { Task { await submit() } }
                        .disabled(saving)
                    Button("Skip") { Task { await submit(skip: true) } }
                        .disabled(saving)
                }
                .buttonStyle(.bordered)
            } else {
                Label((status ?? request.status) == "saved" ? "Credential saved" : "Skipped",
                      systemImage: (status ?? request.status) == "saved" ? "checkmark" : "arrow.turn.down.right")
                    .font(.caption)
            }
            if let error {
                Text(error).font(.caption)
            }
        }
        .padding(12)
        .background(bubble, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
        .onDisappear { value = "" }
    }

    // A short-lived secret says when it ends; a memory-only one says where it lives.
    private var lifetime: String? {
        var lines: [String] = []
        if request.ephemeral == true {
            lines.append("Kept in memory only.")
        }
        if let expiresAt = request.expiresAt {
            let date = Date(timeIntervalSince1970: Double(expiresAt) / 1000)
            lines.append("Expires \(date.formatted(.relative(presentation: .named))).")
        }
        return lines.isEmpty ? nil : lines.joined(separator: " ")
    }

    private func submit(skip: Bool = false) async {
        guard !saving else { return }
        guard skip || !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            error = "Enter a credential, or choose Skip."
            return
        }
        error = nil
        saving = true
        status = skip ? "skipped" : "saved"
        defer { saving = false }
        do {
            let saved = try await client.answerCredential(threadID: message.threadID, messageID: message.id,
                                                         value: skip ? nil : value, skip: skip)
            status = saved.parts.first(where: { $0.kind == PartKind.credential })?.credential?.status
            value = ""
            error = nil
        } catch {
            status = nil
            self.error = error.isOffline ? "You’re offline, so this wasn’t saved." : error.reason(or: "Couldn’t save this credential.")
        }
    }
}
