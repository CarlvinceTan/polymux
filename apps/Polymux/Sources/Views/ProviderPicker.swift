import SwiftUI
import PolymuxKit

struct ProviderEditor: View {
    let client: APIClient?
    let original: Provider?
    let onBack: () -> Void
    let saved: ([Provider]) -> Void
    @State private var page: Page
    @State private var selection: Provider?
    @Environment(ProviderStore.self) private var store: ProviderStore?
    private var providers: [Provider] { store?.providers ?? original.map { [$0] } ?? [] }

    private enum Page {
        case picker, provider, create(ProviderPreset), connection(ProviderConnection?)
        /// Signing in: the option, the provider it adds to, and the connection signing in again.
        case login(ProviderOption, String?, String?)
    }

    init(client: APIClient?, original: Provider?, onBack: @escaping () -> Void, saved: @escaping ([Provider]) -> Void) {
        self.client = client
        self.original = original
        self.onBack = onBack
        self.saved = saved
        _page = State(initialValue: original == nil ? .picker : .provider)
        _selection = State(initialValue: original)
    }

    var body: some View {
        if let client {
            switch page {
            case .picker:
                ProviderPicker(client: client, onBack: onBack, choose: { page = .login($0, nil, nil) }, manual: { page = .create($0) })
            case .provider:
                if let selection {
                    ProviderPage(client: client, providerID: selection.id, onBack: onBack,
                                 connect: { option, connection in page = .login(option, selection.id, connection) },
                                 editConnection: { page = .connection($0) },
                                 removed: { publish(nil, removing: $0); onBack() })
                }
            case .create(let preset):
                ProviderCreate(client: client, preset: preset, onBack: onBack) { provider in
                    publish(provider)
                    onBack()
                }
            case .connection(let connection):
                if let selection {
                    ConnectionForm(client: client, provider: selection, connection: connection, onBack: { page = .provider },
                                   reconnect: { page = .login($0, selection.id, connection?.id) }, saved: publish)
                }
            case .login(let option, let target, let connection):
                ProviderLoginView(client: client, option: option, provider: target, connection: connection, onBack: {
                    page = selection == nil ? .picker : .provider
                }) { provider in
                    publish(provider)
                    if selection == nil { onBack() } else { selection = provider; page = .provider }
                }
            }
        }
    }

    private func publish(_ provider: Provider) { publish(provider, removing: provider.id) }

    private func publish(_ provider: Provider?, removing id: String) {
        if let provider { store?.merge([provider]) }
        else { store?.remove(id) }
        saved(store?.providers ?? providers)
    }
}

struct ProviderPicker: View {
    let client: APIClient
    let onBack: () -> Void
    let choose: (ProviderOption) -> Void
    /// Opens the form for a provider set up by address, such as Ollama.
    let manual: (ProviderPreset) -> Void
    @Environment(\.panelClose) private var close
    @Environment(ProviderStore.self) private var store: ProviderStore?
    private var options: [ProviderOption] { store?.options ?? [] }
    @State private var query = ""
    @State private var category = Category.all
    @State private var issue: String?
    private var loaded: Bool { store?.options != nil }

    private enum Category: CaseIterable {
        case all, apiKey, signIn

        var title: String {
            switch self {
            case .all: "All"
            case .apiKey: "API key"
            case .signIn: "Sign in"
            }
        }

        var auth: String? {
            switch self {
            case .all: nil
            case .apiKey: ProviderOptionAuth.apiKey
            case .signIn: ProviderOptionAuth.oauth
            }
        }
    }

    var body: some View {
        PanelView("Add provider", onClose: { close() }, onBack: onBack,
                  header: AnyView(pickerHeader),
                  accessory: PanelAccessory(symbol: "plus", label: "Add custom provider", action: { manual(.custom) }),
                  scrollUnder: false, height: PanelSize.maximumHeight) {
            Form {
                if showsPopular {
                    Section {
                        ForEach(popular, id: \.self) { option in
                            optionRow(option)
                        }
                        Button { manual(.ollama) } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(ProviderPreset.ollama.label)
                                Text("Models on this computer").font(.caption).foregroundStyle(.primary)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Ollama · Models on this computer")
                    } header: {
                        Text("Popular").foregroundStyle(Color.primary)
                    }
                    .panelRow()
                }
                Section {
                    ForEach(rest, id: \.self) { option in
                        optionRow(option)
                    }
                } header: {
                    if showsPopular { Text("All providers").foregroundStyle(Color.primary) }
                }
                .panelRow()
                if let issue {
                    Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow()
                }
            }
            .formStyle(.grouped)
            #if os(macOS)
            // Grouped forms keep an internal inset on macOS.
            .padding(.top, -PanelStyle.formInset)
            #else
            .contentMargins(.top, 0, for: .scrollContent)
            #endif
            .id(category)
            .overlay {
                if loaded && matches.isEmpty && issue == nil {
                    Text("No providers found.").foregroundStyle(.muted)
                }
            }
        }
        .panelDismiss(onClose: { close() })
        .task { await store?.refreshOptions() }
    }

    private var pickerHeader: some View {
        VStack(spacing: 12) {
            PanelSearch("Search providers", text: $query, autofocus: true)
                .focusEffectDisabled()
                .submitLabel(.search)
                #if os(macOS)
                .background { SearchFocus(prompt: "Search providers").frame(width: 0, height: 0) }
                #endif
                .padding(.horizontal, 14)
                .frame(height: 36)
                .glassEffect(.regular, in: Capsule())
            ScrollView(.horizontal) {
                PillPicker("Provider category", selection: Binding(get: { category }, set: { value in
                    category = value
                    query = ""
                }), options: Category.allCases.map { ($0.title, $0) })
            }
            .scrollIndicators(.hidden)
        }
    }

    private var terms: [Substring] { query.split(whereSeparator: \.isWhitespace) }

    private var matches: [ProviderOption] {
        options.filter { option in
            guard category.auth == nil || option.auth == category.auth else { return false }
            let text = "\(option.name) \(option.label)"
            return terms.allSatisfy { text.localizedStandardContains(String($0)) }
        }
    }

    private var showsPopular: Bool { category == .all && query.isEmpty && !popular.isEmpty }

    private var popular: [ProviderOption] {
        ProviderOption.popular(in: options)
    }

    private var rest: [ProviderOption] {
        guard showsPopular else { return matches }
        let shown = Set(popular)
        return matches.filter { !shown.contains($0) }
    }

    private func optionRow(_ option: ProviderOption) -> some View {
        Button { choose(option) } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(option.name)
                if option.label != option.name {
                    Text(option.label).font(.caption).foregroundStyle(.primary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(option.name) · \(option.label)")
    }
}

extension ProviderOption {
    /// Subscriptions first, then the most used API keys.
    private static let favourites: [(type: String, auth: String)] = [
        ("openai", ProviderOptionAuth.oauth), ("anthropic", ProviderOptionAuth.oauth),
        ("openai", ProviderOptionAuth.apiKey), ("anthropic", ProviderOptionAuth.apiKey),
        ("gemini", ProviderOptionAuth.apiKey), ("openrouter", ProviderOptionAuth.apiKey),
    ]

    /// The popular choices among what the server offers, in that order.
    static func popular(in options: [ProviderOption]) -> [ProviderOption] {
        favourites.compactMap { item in options.first { $0.type == item.type && $0.auth == item.auth } }
    }
}
