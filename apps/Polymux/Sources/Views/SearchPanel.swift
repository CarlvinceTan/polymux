import SwiftUI
import PolymuxKit

struct SearchPanel: View {
    let session: AppSession
    /// The chat a profile's search covers; nil searches everything.
    var scope: String? = nil
    var onBack: (() -> Void)? = nil
    let close: () -> Void
    let select: (SearchResult) -> Void
    @State private var store: GlobalSearchStore
    @State private var query = ""
    @State private var kind = SearchKind.all
    @State private var selected: String?
    @State private var keyed: String?
    @FocusState private var focused: Bool
    @Environment(\.colorScheme) private var scheme
    @Environment(\.panelContext) private var panel

    init(session: AppSession, scope: String? = nil, onBack: (() -> Void)? = nil,
         close: @escaping () -> Void, select: @escaping (SearchResult) -> Void) {
        self.session = session
        self.scope = scope
        self.onBack = onBack
        self.close = close
        self.select = select
        _store = State(initialValue: GlobalSearchStore(client: session.client, actor: session.sidebar.cacheActor, scope: scope))
    }

    private var kinds: [SearchKind] {
        scope == nil ? SearchKind.allCases : [.all, .messages, .media, .files]
    }

    var body: some View {
        VStack(spacing: 0) {
            VStack(spacing: 12) {
                ZStack {
                    Text("Search")
                        .font(.headline)
                        .lineLimit(1)
                        .padding(.horizontal, 64)
                        .accessibilityAddTraits(.isHeader)
                    HStack {
                        if let onBack { PanelNavButton(symbol: "chevron.left", label: "Back", action: onBack) }
                        Spacer()
                        PanelNavButton(symbol: "xmark", label: "Close search", action: dismiss)
                    }
                }
                .frame(minHeight: 44)
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").foregroundStyle(.muted)
                    TextField(scope == nil ? "Search" : "Search this chat", text: $query)
                        .accessibilityLabel(scope == nil ? "Search everything" : "Search this chat")
                        .textFieldStyle(.plain)
                        .focused($focused).focusEffectDisabled()
                        .submitLabel(.search)
                        .onSubmit(openSelection)
                        #if os(macOS)
                        .background {
                            SearchFocus(prompt: scope == nil ? "Search" : "Search this chat", autofocus: true)
                                .frame(width: 0, height: 0)
                        }
                        #endif
                }
                .padding(.horizontal, 14)
                .frame(height: 36)
                .glassEffect(.regular, in: Capsule())
            }
            .padding(16)
            ScrollView(.horizontal) {
                PillPicker("Search category", selection: $kind,
                           options: kinds.map { ($0.title, $0) })
                    .padding(.horizontal, 16).padding(.bottom, 12)
            }
            .scrollIndicators(.hidden)
            if let error = store.error, !cleanQuery.isEmpty {
                Label(error, systemImage: "internaldrive")
                    .font(.caption).foregroundStyle(.primary)
                    .padding(.horizontal, 16).padding(.bottom, 8)
            }
            ScrollViewReader { scroll in
                ScrollView {
                    LazyVStack(spacing: 8) {
                        ForEach(results, id: \.id) { result in
                            Button { select(result) } label: { row(result) }
                                .buttonStyle(.plain)
                                .accessibilityAddTraits(highlighted == result.id ? .isSelected : [])
                                .id(result.id)
                        }
                    }
                    .padding(PanelStyle.contentInset)
                }
                .onChange(of: keyed) { _, id in
                    if let id { scroll.scrollTo(id) }
                }
            }
            .scrollIndicators(.hidden)
            .mask {
                VStack(spacing: 0) {
                    LinearGradient(colors: [.clear, .black], startPoint: .top, endPoint: .bottom)
                        .frame(height: PanelStyle.contentInset)
                    Rectangle().fill(.black)
                    LinearGradient(colors: [.black, .clear], startPoint: .top, endPoint: .bottom)
                        .frame(height: PanelStyle.contentInset)
                }
            }
            .overlay {
                if results.isEmpty && !cleanQuery.isEmpty && !store.isSearching {
                    Text("No results found.")
                        .foregroundStyle(.muted)
                } else if results.isEmpty && store.isSearching {
                    ProgressView().controlSize(.small).accessibilityLabel("Searching")
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(PanelSurface.color(scheme))
        .panelDismiss(onClose: close)
        .task { focused = true }
        .task(id: kind.rawValue + "/" + query) {
            selected = nil
            guard !cleanQuery.isEmpty else { return }
            await store.search(query: query, kind: kind.rawValue)
        }
        .onKeyPress(.downArrow) { move(1); return .handled }
        .onKeyPress(.upArrow) { move(-1); return .handled }
    }

    private var cleanQuery: String { query.trimmingCharacters(in: .whitespacesAndNewlines) }

    private var results: [SearchResult] {
        guard cleanQuery.isEmpty else { return store.results }
        guard scope == nil else { return [] }
        let agents = Dictionary(session.sidebar.agents.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return session.sidebar.chats.compactMap { thread in
            if thread.id == thread.agentID, let agent = agents[thread.agentID], kind == .all || kind == .bots {
                return SearchResult(id: "bot/" + agent.id, kind: "bots", title: agent.name, snippet: agent.role,
                                    threadID: thread.id, agentID: agent.id)
            }
            guard kind == .conversations || kind == .all && thread.id != thread.agentID else { return nil }
            let names = thread.agentIDs.compactMap { agents[$0]?.name }.joined(separator: ", ")
            return SearchResult(id: "conversation/" + thread.id, kind: "conversations", title: thread.title,
                                snippet: names, threadID: thread.id, agentID: thread.agentID)
        }
    }

    private func row(_ result: SearchResult) -> some View {
        HStack(spacing: 12) {
            if let agentID = result.agentID {
                AgentAvatar(agentID: agentID).frame(width: 36, height: 36)
            } else {
                Image(systemName: symbol(result))
                    .font(.title3).foregroundStyle(.muted)
                    .frame(width: 36, height: 36)
            }
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(result.title).font(.body.weight(.medium)).lineLimit(1)
                    Spacer(minLength: 8)
                    Text(SearchKind(rawValue: result.kind)?.label ?? "")
                        .font(.caption).foregroundStyle(.primary)
                }
                if !result.snippet.isEmpty {
                    Text(result.snippet).font(.subheadline).foregroundStyle(.primary).lineLimit(2)
                }
                if let date = result.message?.date {
                    Text(date, format: .dateTime.year().month(.abbreviated).day().hour().minute())
                        .font(.caption).foregroundStyle(.muted)
                }
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(RoundedRectangle(cornerRadius: 10))
        .background(.primary.opacity(highlighted == result.id ? PanelStyle.selectedTint : 0), in: RoundedRectangle(cornerRadius: 10))
        .pointerHover { if $0 { selected = result.id } }
    }

    private func symbol(_ result: SearchResult) -> String {
        if let attachment = result.attachment {
            if attachment.mimeType.hasPrefix("image/") { return "photo" }
            if attachment.mimeType.hasPrefix("video/") { return "film" }
            if attachment.mimeType.hasPrefix("audio/") { return "waveform" }
            return "doc"
        }
        return "text.bubble"
    }

    /// What Return opens: the chosen row, or the first when nothing has been chosen.
    private var highlighted: String? {
        #if os(macOS)
        results.contains { $0.id == selected } ? selected : results.first?.id
        #else
        selected
        #endif
    }

    private func move(_ offset: Int) {
        guard !results.isEmpty else { return }
        let index = results.firstIndex { $0.id == highlighted } ?? 0
        selected = results[max(0, min(results.count - 1, index + offset))].id
        keyed = selected
    }

    private func openSelection() {
        if let result = results.first(where: { $0.id == highlighted }) ?? results.first { select(result) }
    }

    private func dismiss() {
        if let panel { panel.end(close) }
        else { close() }
    }
}

private enum SearchKind: String, CaseIterable, Identifiable {
    case all, conversations, bots, messages, media, files
    var id: String { rawValue }
    var title: String { self == .conversations ? "Chats" : rawValue.capitalized }
    var label: String {
        switch self {
        case .all: "All"
        case .conversations: "Chat"
        case .bots: "Bot"
        case .messages: "Message"
        case .media: "Media"
        case .files: "File"
        }
    }
}
