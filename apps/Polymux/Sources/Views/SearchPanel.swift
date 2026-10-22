import SwiftUI
import PolymuxKit

struct SearchPanel: View {
    let session: AppSession
    let close: () -> Void
    let select: (SearchResult) -> Void
    @State private var store: GlobalSearchStore
    @State private var query = ""
    @State private var kind = SearchKind.all
    @State private var selected: String?
    @FocusState private var focused: Bool
    @Environment(\.colorScheme) private var scheme
    @Environment(\.panelContext) private var panel

    init(session: AppSession, close: @escaping () -> Void, select: @escaping (SearchResult) -> Void) {
        self.session = session
        self.close = close
        self.select = select
        _store = State(initialValue: GlobalSearchStore(client: session.client, actor: session.sidebar.cacheActor))
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
                TextField("", text: $query)
                    .accessibilityLabel("Search everything")
                    .font(.title3).textFieldStyle(.plain)
                    .focused($focused).focusEffectDisabled()
                    .submitLabel(.search)
                    .onSubmit(openSelection)
                    #if os(macOS)
                    .background { SearchFocus(prompt: "").frame(width: 0, height: 0) }
                    #endif
                Button("Close search", systemImage: "xmark", action: dismiss)
                    .labelStyle(.iconOnly).buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                    .frame(width: 36, height: 36).contentShape(Circle())
            }
            .padding(16)
            Divider()
            ScrollView(.horizontal) {
                PillPicker("Search category", selection: $kind,
                           options: SearchKind.allCases.map { ($0.title, $0) })
                    .padding(16)
            }
            .scrollIndicators(.hidden)
            if let error = store.error, !cleanQuery.isEmpty {
                Label(error, systemImage: "wifi.slash")
                    .font(.caption).foregroundStyle(.primary)
                    .padding(.horizontal, 16).padding(.bottom, 8)
            }
            ScrollView {
                LazyVStack(spacing: 8) {
                    ForEach(results, id: \.id) { result in
                        Button { select(result) } label: { row(result) }
                            .buttonStyle(.plain)
                    }
                }
                .padding(.horizontal, 16).padding(.bottom, 16)
            }
            .scrollIndicators(.hidden)
            .overlay {
                if results.isEmpty && !cleanQuery.isEmpty && !store.isSearching {
                    Text("No results found")
                        .foregroundStyle(.secondary)
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
        let agents = Dictionary(session.sidebar.agents.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return session.sidebar.threads.compactMap { thread in
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
                    .font(.title3).foregroundStyle(.secondary)
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
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(RoundedRectangle(cornerRadius: 10))
        .background(selected == result.id ? Color.primary.opacity(0.11) : .clear, in: RoundedRectangle(cornerRadius: 10))
        .onHover { if $0 { selected = result.id } }
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

    private func move(_ offset: Int) {
        guard !results.isEmpty else { return }
        let index = results.firstIndex { $0.id == selected } ?? (offset > 0 ? -1 : results.count)
        selected = results[max(0, min(results.count - 1, index + offset))].id
    }

    private func openSelection() {
        if let result = results.first(where: { $0.id == selected }) ?? results.first { select(result) }
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
