import SwiftUI
import PolymuxKit

/// Where connecting a provider is, inside setup's model step.
enum SetupProviderPage: Hashable {
    case popular, other, methods(String), signIn(ProviderOption), local
}

/// A provider setup shows first, with its mark.
struct SetupBrand: Identifiable {
    let type: String
    let name: String
    var symbol = "cloud.fill"
    var tint: Color = .blue
    var id: String { type }

    static let popular = [
        SetupBrand(type: "openai", name: "OpenAI", symbol: "circle.hexagongrid.fill", tint: .primary),
        SetupBrand(type: "anthropic", name: "Anthropic", symbol: "asterisk", tint: .orange),
        SetupBrand(type: "gemini", name: "Google Gemini", symbol: "sparkle", tint: .blue),
        SetupBrand(type: "openrouter", name: "OpenRouter", symbol: "arrow.triangle.branch", tint: .indigo),
    ]

    var mark: some View {
        Image(systemName: symbol).font(.system(size: 18, weight: .semibold)).foregroundStyle(tint)
        .frame(width: 40, height: 40)
        .background(.quinary, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .accessibilityHidden(true)
    }

    /// "ChatGPT subscription or API key", read from what the server offers.
    func summary(_ options: [ProviderOption]) -> String {
        let mine = options.filter { $0.type == type }
        let signIn = mine.first { $0.auth == ProviderOptionAuth.oauth }.map { option -> String in
            guard let open = option.label.firstIndex(of: "("), let close = option.label.lastIndex(of: ")"), open < close else { return "Sign in" }
            return String(option.label[option.label.index(after: open)..<close])
        }
        let key = mine.contains { $0.auth == ProviderOptionAuth.apiKey } ? "API key" : nil
        return [signIn, key].compactMap { $0 }.joined(separator: " or ")
    }
}

/// The popular providers as tiles, and a way to every other one.
struct SetupPopular: View {
    let options: [ProviderOption]?
    let open: (String) -> Void
    let other: () -> Void
    @Environment(\.colorScheme) private var scheme

    private let columns = [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)]
    private var brands: [SetupBrand] { SetupBrand.popular.filter { brand in (options ?? []).contains { $0.type == brand.type } } }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if options == nil {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity)
            } else {
                Text("Popular").font(.headline)
                LazyVGrid(columns: columns, spacing: 12) {
                    ForEach(brands) { brand in
                        Button { open(brand.type) } label: {
                            HStack(spacing: 12) {
                                brand.mark
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(brand.name).font(.headline).lineLimit(1)
                                    Text(brand.summary(options ?? [])).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                                }
                                .frame(maxWidth: .infinity, alignment: .leading)
                            }
                            .padding(12)
                            .onboardingCard(scheme)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(brand.name)
                        .accessibilityValue(brand.summary(options ?? []))
                    }
                }
                Button(action: other) {
                    OnboardingRow(title: "Other providers", detail: "Ollama, DeepSeek, Mistral and more.",
                                  symbol: "ellipsis", tint: .secondary) {
                        Image(systemName: "chevron.right").foregroundStyle(.secondary)
                    }
                    .onboardingCard(scheme)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Other providers")
            }
        }
    }
}

/// A provider's ways to connect, when it has more than one.
struct SetupMethods: View {
    let brand: SetupBrand?
    let options: [ProviderOption]
    let choose: (ProviderOption) -> Void
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 12) {
                brand?.mark
                Text(brand?.name ?? options.first?.name ?? "").font(.title3.bold())
            }
            VStack(spacing: 0) {
                ForEach(Array(options.enumerated()), id: \.element) { index, option in
                    if index > 0 { Divider().padding(.horizontal, 12) }
                    let oauth = option.auth == ProviderOptionAuth.oauth
                    Button { choose(option) } label: {
                        OnboardingRow(title: oauth ? "Sign in" : "API key", detail: option.label,
                                      symbol: oauth ? "person.crop.circle.fill" : "key.fill", tint: oauth ? .purple : .blue) {
                            Image(systemName: "chevron.right").foregroundStyle(.secondary)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(option.name) · \(option.label)")
                }
            }
            .onboardingCard(scheme)
        }
    }
}

/// Every provider outside the popular ones, searchable.
struct SetupOtherProviders: View {
    let options: [ProviderOption]?
    let choose: (ProviderOption) -> Void
    let local: () -> Void
    @State private var query = ""
    @Environment(\.colorScheme) private var scheme

    private var terms: [Substring] { query.split(whereSeparator: \.isWhitespace) }

    private func matches(_ text: String) -> Bool { terms.allSatisfy { text.localizedStandardContains(String($0)) } }

    private var rows: [ProviderOption] {
        let popular = Set(SetupBrand.popular.map(\.type))
        return (options ?? []).filter { !popular.contains($0.type) && matches("\($0.name) \($0.label)") }
    }

    private var showsOllama: Bool { matches("\(ProviderPreset.ollama.label) local") }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            PanelSearch("Search providers", text: $query, autofocus: true)
                .focusEffectDisabled()
                .padding(.horizontal, 14)
                .frame(height: 36)
                .glassEffect(.regular, in: Capsule())
            if options == nil {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity)
            } else if rows.isEmpty && !showsOllama {
                Text("No providers found.").foregroundStyle(.secondary).frame(maxWidth: .infinity).padding(.vertical, 20)
            } else {
                VStack(spacing: 0) {
                    if showsOllama {
                        Button(action: local) {
                            OnboardingRow(title: ProviderPreset.ollama.label, detail: "Models on this computer",
                                          symbol: "desktopcomputer", tint: .green) { chevron }
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Ollama · Models on this computer")
                    }
                    ForEach(Array(rows.enumerated()), id: \.element) { index, option in
                        if index > 0 || showsOllama { Divider().padding(.horizontal, 12) }
                        row(option)
                    }
                }
                .onboardingCard(scheme)
            }
        }
    }

    private func row(_ option: ProviderOption) -> some View {
        let oauth = option.auth == ProviderOptionAuth.oauth
        return Button { choose(option) } label: {
            OnboardingRow(title: option.name, detail: option.label, symbol: oauth ? "person.crop.circle.fill" : "key.fill",
                          tint: oauth ? .purple : .blue) { chevron }
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(option.name) · \(option.label)")
    }

    private var chevron: some View { Image(systemName: "chevron.right").foregroundStyle(.secondary) }
}

/// Signs in to a provider or takes its API key, inline in setup.
struct SetupSignIn: View {
    let option: ProviderOption
    let connected: (Provider) -> Void
    @State private var signIn: ProviderSignIn
    @State private var answer = ""
    @Environment(\.colorScheme) private var scheme

    init(client: APIClient, option: ProviderOption, connected: @escaping (Provider) -> Void) {
        self.option = option
        self.connected = connected
        _signIn = State(initialValue: ProviderSignIn(client: client))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 0) {
                OnboardingRow(title: option.name, detail: option.label,
                              symbol: option.auth == ProviderOptionAuth.oauth ? "person.crop.circle.fill" : "key.fill",
                              tint: option.auth == ProviderOptionAuth.oauth ? .purple : .blue) { EmptyView() }
                ForEach(Array((signIn.state?.events ?? []).enumerated()), id: \.offset) { _, event in
                    Divider().padding(.horizontal, 12)
                    eventView(event)
                }
                if let prompt = signIn.state?.prompt {
                    Divider().padding(.horizontal, 12)
                    promptView(prompt)
                }
            }
            .onboardingCard(scheme)
            if let issue = signIn.state?.error ?? signIn.issue {
                Text(issue).font(.footnote).foregroundStyle(.secondary)
            } else if signIn.state?.done != true && signIn.state?.prompt == nil {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity).accessibilityLabel("Connecting provider")
            }
        }
        .task { await signIn.connect(option: option) }
        .onChange(of: signIn.state?.prompt?.id) { answer = "" }
        .onChange(of: signIn.state?.provider) { _, provider in
            if let provider { connected(provider) }
        }
    }

    private func eventView(_ event: LoginEvent) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            if let message = event.message { Text(message).textSelection(.enabled) }
            if let instructions = event.instructions { Text(instructions).foregroundStyle(.secondary).textSelection(.enabled) }
            if let code = event.userCode {
                Text(code).font(.title2.monospaced().weight(.semibold)).textSelection(.enabled)
            }
            if let link = link(event) {
                HStack(spacing: 10) {
                    Link(destination: link) { Label("Open", systemImage: "arrow.up.right.square") }
                        .buttonStyle(.bordered)
                    Button(event.userCode == nil ? "Copy link" : "Copy code", systemImage: "doc.on.doc") {
                        copy(event.userCode ?? link.absoluteString)
                    }
                    .buttonStyle(.bordered)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14)
    }

    @ViewBuilder private func promptView(_ prompt: LoginPrompt) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            if prompt.type != LoginPromptType.secret { Text(prompt.message) }
            if prompt.type == LoginPromptType.select {
                ForEach(prompt.options ?? [], id: \.id) { choice in
                    Button { Task { await signIn.submit(choice.id) } } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(choice.label)
                            if let description = choice.description {
                                Text(description).font(.caption).foregroundStyle(.secondary)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .buttonStyle(.bordered)
                    .disabled(signIn.sending)
                    .accessibilityLabel(choice.label)
                }
            } else {
                HStack(spacing: 8) {
                    Group {
                        if prompt.type == LoginPromptType.secret {
                            SecureField("API key", text: $answer, prompt: Text("Enter API key"))
                        } else {
                            TextField(prompt.placeholder ?? "Answer", text: $answer, prompt: Text(prompt.placeholder ?? ""))
                        }
                    }
                    .textFieldStyle(.roundedBorder)
                    .controlSize(.large)
                    .autocorrectionDisabled()
                    .onSubmit(submit)
                    .disabled(signIn.sending)
                    Button("Continue", action: submit)
                        .buttonStyle(.borderedProminent)
                        .controlSize(.large)
                        .disabled(signIn.sending || answer.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
                if prompt.type == LoginPromptType.secret, let page = KeyPage.url(for: option.type) {
                    Link(destination: page) { Label("Get a key", systemImage: "arrow.up.right.square") }
                        .accessibilityLabel("Get an API key from \(option.name)")
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14)
    }

    private func submit() {
        guard !answer.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        Task { await signIn.submit(answer) }
    }

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

/// Connects Ollama by its address and loads its models, inline in setup.
struct SetupLocal: View {
    let client: APIClient
    let connected: (Provider) -> Void
    @State private var baseURL = ProviderPreset.ollama.baseURL
    @State private var saving = false
    @State private var issue: String?
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 0) {
                OnboardingRow(title: ProviderPreset.ollama.label, detail: "Models on this computer",
                              symbol: "desktopcomputer", tint: .green) { EmptyView() }
                Divider().padding(.horizontal, 12)
                HStack(spacing: 8) {
                    TextField("Base URL", text: $baseURL, prompt: Text(ProviderPreset.ollama.baseURL))
                        .accessibilityLabel("Base URL")
                        .textFieldStyle(.roundedBorder)
                        .controlSize(.large)
                        .autocorrectionDisabled()
                        .onSubmit(connect)
                        .disabled(saving)
                    Button("Connect", action: connect)
                        .buttonStyle(.borderedProminent)
                        .controlSize(.large)
                        .disabled(saving || baseURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
                .padding(14)
            }
            .onboardingCard(scheme)
            if saving {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity).accessibilityLabel("Connecting Ollama")
            } else if let issue {
                Text(issue).font(.footnote).foregroundStyle(.secondary)
            }
        }
    }

    private func connect() {
        guard !saving else { return }
        saving = true
        issue = nil
        Task {
            defer { saving = false }
            var added: Provider?
            do {
                let provider = try await client.saveProvider(
                    Provider(id: "ollama-\(UUID().uuidString.lowercased())", type: ProviderPreset.ollama.rawValue,
                             name: ProviderPreset.ollama.label, baseURL: baseURL.trimmingCharacters(in: .whitespacesAndNewlines),
                             enabled: true, order: 0, models: []), isNew: true)
                added = provider
                connected(try await client.discoverModels(id: provider.id))
            } catch {
                // A connection without models can't run a bot, so it doesn't stay behind.
                if let added { try? await client.deleteProvider(id: added.id) }
                issue = added == nil ? error.reason(or: "Couldn’t add Ollama.")
                                     : "Couldn’t reach Ollama. Open it on this computer, then connect again."
            }
        }
    }
}
