import SwiftUI
import PolymuxKit

/// Searches one chat. Older pages join quietly until the chat's start, and a result returns to its message.
struct HubSearchPane: View {
    @Bindable var store: HubStore
    let chat: HubChat
    let client: APIClient
    let close: () -> Void
    let reveal: (HubMessage) -> Void
    @State private var query = ""
    @State private var filter = HubSearchFilter.all

    private var results: [HubSearchResult] { HubSearch.results(store.messages[chat.id] ?? [], query: query, filter: filter) }

    var body: some View {
        VStack(spacing: PanelStyle.rowInset) {
            HStack(spacing: PanelStyle.rowInset) {
                HStack { PanelSearch("Search \(chat.name)", text: $query, autofocus: true) }
                    .padding(.horizontal, PanelStyle.headingInset)
                    .frame(height: 36)
                    .glassEffect(.regular, in: Capsule())
                PanelNavButton(symbol: "xmark", label: "Close search", action: close)
                    .keyboardShortcut(.cancelAction)
            }
            PillPicker("Search filter", selection: $filter, options: HubSearchFilter.allCases.map { ($0.title, $0) }, compact: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            list
        }
        .padding(.horizontal, PanelStyle.contentInset).padding(.top, 52)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .task { await store.loadHistory(chat.id) }
    }

    @ViewBuilder private var list: some View {
        let rows = results
        if rows.isEmpty {
            Text(store.searching ? "Searching history." : "No results found.").foregroundStyle(.muted)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            List(rows) { result in
                Button { reveal(result.message) } label: { row(result) }
                    .buttonStyle(.plain)
                    .listRowSeparator(.hidden)
            }
            .listStyle(.plain).scrollContentBackground(.hidden)
        }
    }

    private func row(_ result: HubSearchResult) -> some View {
        HStack(spacing: RowDetails.gap) {
            thumbnail(result).frame(width: 40, height: 40)
            VStack(alignment: .leading, spacing: 2) {
                Text(result.title).lineLimit(2)
                if let detail = result.detail { Text(detail).font(.caption).foregroundStyle(.muted).lineLimit(1) }
                Text("\(result.message.mine ? "You" : result.message.senderName) · \(HubRow.when(result.message.at))")
                    .font(.caption).foregroundStyle(.muted).lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }

    @ViewBuilder private func thumbnail(_ result: HubSearchResult) -> some View {
        let picture = result.message.kind == HubMessageKind.image || result.message.kind == HubMessageKind.sticker
        if result.kind == .media, picture, let file = result.message.attachment {
            CachedImage(url: client.resolve(HubStore.mediaPath(file.uri, thumbnail: true)), name: file.name, client: client) { image in
                image.resizable().scaledToFill()
            } placeholder: {
                RoundedRectangle(cornerRadius: 8).fill(.quaternary)
            }
            .clipShape(.rect(cornerRadius: 8))
            .accessibilityHidden(true)
        } else {
            Image(systemName: symbol(result)).foregroundStyle(.muted).accessibilityHidden(true)
        }
    }

    private func symbol(_ result: HubSearchResult) -> String {
        switch result.kind {
        case .message: "text.bubble"
        case .link: "link"
        case .file: "doc"
        case .media:
            switch result.message.kind {
            case HubMessageKind.audio: "waveform"
            case HubMessageKind.video: "video"
            default: "photo"
            }
        }
    }
}
