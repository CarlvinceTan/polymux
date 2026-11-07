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
        case picker, provider, form(ProviderPreset?), login(ProviderOption, String?)
    }

    init(client: APIClient?, original: Provider?, onBack: @escaping () -> Void, saved: @escaping ([Provider]) -> Void) {
        self.client = client
        self.original = original
        self.onBack = onBack
        self.saved = saved
        _page = State(initialValue: original.map {
            ProviderPreset(rawValue: $0.type).map { .form($0) } ?? .provider
        } ?? .picker)
        _selection = State(initialValue: original)
    }

    var body: some View {
        if let client {
            switch page {
            case .picker:
                ProviderPicker(client: client, onBack: onBack, choose: { page = .login($0, nil) }, manual: { page = .form(.custom) })
            case .provider:
                let type = original?.type ?? selection?.type ?? ""
                ProviderPage(client: client, type: type, connections: providers.filter { $0.type == type }, onBack: onBack,
                             edit: { selection = $0; page = .form(nil) }, connect: { page = .login($0, nil) },
                             removed: { publish(nil, removing: $0) }, changed: publish)
            case .form(let preset):
                let type = selection?.type ?? preset?.rawValue ?? "custom"
                ProviderForm(client: client, original: selection, preset: preset,
                             connections: providers.filter { $0.type == type },
                             onBack: { if preset == nil { page = .provider } else { onBack() } }, connect: { page = .login($0, $1) },
                             select: { selection = $0; page = .form(ProviderPreset(rawValue: $0.type)) },
                             add: { selection = nil; page = .form(preset) },
                             saved: publish, deleted: { publish(nil, removing: $0) })
                    .id(selection?.id)
            case .login(let option, let target):
                ProviderLoginView(client: client, option: option, provider: target, onBack: {
                    page = original == nil ? .picker : .provider
                }) { provider in
                    publish(provider)
                    if original == nil {
                        onBack()
                    } else {
                        selection = provider
                        page = .provider
                    }
                }
            }
        }
    }

    private func publish(_ provider: Provider) { publish(provider, removing: provider.id) }

    private func publish(_ updates: [Provider]) {
        store?.merge(updates)
        saved(store?.providers ?? providers)
    }

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
    let manual: () -> Void
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
                  accessory: PanelAccessory(symbol: "plus", label: "Add custom provider", action: manual),
                  scrollUnder: false, height: PanelSize.maximumHeight) {
            Form {
                Section {
                    ForEach(matches, id: \.self) { option in
                        optionRow(option)
                    }
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
