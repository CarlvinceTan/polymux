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

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label(request.label, systemImage: "key")
                .fixedSize(horizontal: false, vertical: true)
            Text(request.origin).font(.caption)
            if (status ?? request.status) == "pending" {
                SecureField("Credential", text: $value)
                    .textFieldStyle(.roundedBorder)
                    .autocorrectionDisabled()
                    .onSubmit { Task { await submit() } }
                    .disabled(saving)
                    .accessibilityLabel("Credential value")
                HStack(spacing: 12) {
                    Button("Save credential") { Task { await submit() } }
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
            status = saved.parts.first(where: { $0.kind == "credential" })?.credential?.status
            value = ""
            error = nil
        } catch {
            status = nil
            self.error = "Couldn’t save this credential. Try again."
        }
    }
}
