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
                                Text(hit.message.role == "user" ? "You" : agentName).font(.subheadline.weight(.medium))
                                Spacer()
                                Text(hit.message.date, format: ProgressiveTime()).font(.caption).foregroundStyle(.secondary)
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
                            if hit.indexingState == "pending" {
                                Text("Media still indexing").font(.caption2).foregroundStyle(.secondary)
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
                if store.isSearching {
                    ProgressView()
                } else if store.results.isEmpty {
                    ContentUnavailableView {
                        Label(query.isEmpty ? "Find a message" : "No messages found", systemImage: "magnifyingglass")
                    } description: {
                        Text(query.isEmpty ? "Search what you discussed with \(agentName)." : "Try another word or phrase.")
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .task(id: query) {
            do { try await Task.sleep(for: .milliseconds(250)) } catch { return }
            store.query = query
            await store.search()
        }
        .onKeyPress(.escape) { close(); return .handled }
    }

    private func highlight(_ text: String) -> AttributedString {
        var result = AttributedString(text.replacingOccurrences(of: "<b>", with: "").replacingOccurrences(of: "</b>", with: ""))
        for term in query.split(separator: " ") {
            if let range = result.range(of: String(term), options: .caseInsensitive) {
                result[range].backgroundColor = Color.accentColor.opacity(0.18)
            }
        }
        return result
    }
}
