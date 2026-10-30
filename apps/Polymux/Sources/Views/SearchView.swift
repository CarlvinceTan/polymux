import SwiftUI
import PolymuxKit

struct SearchView: View {
    let client: APIClient
    let agentName: String
    let topInset: CGFloat
    let bottomInset: CGFloat
    let close: () -> Void
    let select: (String) -> Void
    @Binding var query: String
    @State private var store: SearchStore

    init(session: AppSession, threadID: String, agentName: String, query: Binding<String>, topInset: CGFloat = 0, bottomInset: CGFloat = 0, close: @escaping () -> Void,
         select: @escaping (String) -> Void) {
        self.client = session.client
        self.agentName = agentName
        self.topInset = topInset
        self.bottomInset = bottomInset
        self.close = close
        self.select = select
        _query = query
        _store = State(initialValue: SearchStore(client: session.client, actor: session.sidebar.cacheActor, threadID: threadID))
    }

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(store.results, id: \.message.id) { hit in
                    Button {
                        select(hit.message.id)
                    } label: {
                        VStack(alignment: .leading, spacing: 8) {
                            HStack {
                                Text(hit.message.role == MessageRole.user ? "You" : agentName).font(.subheadline.weight(.medium))
                                Spacer()
                                Text(hit.message.date, format: ProgressiveTime()).font(.caption).foregroundStyle(.muted)
                            }
                            Text(highlight(hit.snippet)).lineLimit(4).font(.subheadline)
                            if !hit.message.parts.compactMap(\.attachment).isEmpty {
                                ScrollView(.horizontal) {
                                    HStack(alignment: .bottom, spacing: 12) {
                                        ForEach(hit.message.parts.compactMap(\.attachment), id: \.id) { attachment in
                                            AttachmentPreview(attachment: attachment, client: client)
                                        }
                                    }
                                }
                                .scrollIndicators(.hidden)
                            }
                            if hit.indexingState == SearchHitIndexingState.pending {
                                Text("Media still indexing").font(.caption2).foregroundStyle(.muted)
                            }
                        }
                        .padding(.vertical, 14)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    Divider()
                }
                }
                .padding(.horizontal, 20)
            }
            .scrollIndicators(.hidden)
            .scrollContentBackground(.hidden)
            .scrollEdgeEffectHidden()
            .contentMargins(.top, topInset, for: .scrollContent)
            .contentMargins(.bottom, bottomInset, for: .scrollContent)
            .frame(maxWidth: 720).frame(maxWidth: .infinity)
            .overlay {
                if store.results.isEmpty && store.isSearching {
                    Text("Searching…").foregroundStyle(.muted)
                } else if store.results.isEmpty {
                    ContentUnavailableView {
                        Label(query.isEmpty ? "Find a message" : "No messages found", systemImage: "magnifyingglass")
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .task(id: query) {
            store.query = query
            await store.search()
        }
        .onKeyPress(.escape) { close(); return .handled }
    }

    private func highlight(_ text: String) -> AttributedString {
        var result = AttributedString(text.replacingOccurrences(of: "<b>", with: "").replacingOccurrences(of: "</b>", with: ""))
        let plain = String(result.characters) as NSString
        for term in query.split(separator: " ") {
            let raw = String(term)
            var location = 0
            while location <= plain.length {
                let found = plain.range(of: raw, options: [.caseInsensitive],
                                        range: NSRange(location: location, length: plain.length - location))
                guard found.location != NSNotFound, found.length > 0 else { break }
                if let range = Range(found, in: result) {
                    result[range].backgroundColor = Color.accentColor.opacity(0.18)
                }
                location = found.location + found.length
            }
        }
        return result
    }
}
