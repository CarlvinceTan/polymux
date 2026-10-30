import SwiftUI
import PolymuxKit

struct ProviderLoginView: View {
    let option: ProviderOption
    let provider: String?
    let onBack: () -> Void
    let connected: (Provider) -> Void
    @Environment(\.panelClose) private var close
    @State private var signIn: ProviderSignIn
    @State private var answer = ""

    init(client: APIClient, option: ProviderOption, provider: String?, onBack: @escaping () -> Void,
         connected: @escaping (Provider) -> Void) {
        self.option = option
        self.provider = provider
        self.onBack = onBack
        self.connected = connected
        _signIn = State(initialValue: ProviderSignIn(client: client))
    }

    var body: some View {
        PanelView(option.name, onClose: { close() }, onBack: onBack) {
            Form {
                ForEach(Array((signIn.state?.events ?? []).enumerated()), id: \.offset) { _, event in
                    Section {
                        if let message = event.message { Text(message).textSelection(.enabled) }
                        if let instructions = event.instructions { Text(instructions).textSelection(.enabled) }
                        if let code = event.userCode {
                            LabeledContent("Code") { Text(code).monospaced().textSelection(.enabled) }
                        }
                        if let link = link(event) {
                            HStack {
                                Link("Open", destination: link)
                                Button("Copy") { copy(event.userCode ?? link.absoluteString) }
                                    .accessibilityLabel(event.userCode == nil ? "Copy link" : "Copy code")
                            }
                        }
                    }
                    .panelRow()
                }
                if let prompt = signIn.state?.prompt {
                    Section {
                        if prompt.type != LoginPromptType.secret { Text(prompt.message) }
                        if prompt.type == LoginPromptType.select {
                            ForEach(prompt.options ?? [], id: \.id) { choice in
                                Button { Task { await signIn.submit(choice.id) } } label: {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(choice.label)
                                        if let description = choice.description {
                                            Text(description).font(.caption).foregroundStyle(.primary)
                                        }
                                    }
                                }
                                .disabled(signIn.sending)
                            }
                        } else {
                            Group {
                                if prompt.type == LoginPromptType.secret {
                                    SettingsField(title: "API key", text: $answer, prompt: "Enter API key", secure: true)
                                } else {
                                    TextField("", text: $answer, prompt: Text(prompt.placeholder ?? "").foregroundStyle(.muted))
                                        .accessibilityLabel(prompt.placeholder ?? "Answer")
                                }
                            }
                            .autocorrectionDisabled()
                            .onSubmit(submit)
                            .disabled(signIn.sending)
                        }
                    }
                    .panelRow()
                }
                if let issue = signIn.state?.error ?? signIn.issue {
                    Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow()
                } else if signIn.state?.done != true && signIn.state?.prompt == nil {
                    Section {
                        ProgressView().frame(maxWidth: .infinity).accessibilityLabel("Connecting provider")
                    }
                    .panelRow()
                }
            }
            .formStyle(.grouped)
        } actions: {
            if let prompt = signIn.state?.prompt, prompt.type != LoginPromptType.select {
                Button("Continue", action: submit)
                    .disabled(signIn.sending || (prompt.type == LoginPromptType.secret && answer.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty))
                    .keyboardShortcut(.defaultAction)
            }
        }
        .panelDismiss(onClose: { close() })
        .task { await signIn.connect(option: option, provider: provider) }
        .onChange(of: signIn.state?.prompt?.id) { answer = "" }
        .onChange(of: signIn.state?.provider) { _, provider in
            if let provider { connected(provider) }
        }
    }

    private func submit() { Task { await signIn.submit(answer) } }

    private func link(_ event: LoginEvent) -> URL? {
        guard let value = event.url ?? event.verificationUri, let url = URL(string: value),
              ["https", "http"].contains(url.scheme?.lowercased() ?? "") else { return nil }
        return url
    }

    private func copy(_ text: String) {
        #if os(macOS)
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        #else
        UIPasteboard.general.string = text
        #endif
    }
}
