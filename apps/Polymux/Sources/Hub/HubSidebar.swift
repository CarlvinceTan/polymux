import SwiftUI
import PolymuxKit

/// A platform's logo, from the brand marks Hub ships with.
struct PlatformMark: View {
    let platform: String
    var size: CGFloat = 16

    var body: some View {
        Group {
            if PlatformMark.known.contains(platform) {
                Image("platform-" + platform).resizable().scaledToFit()
            } else {
                Image(systemName: "bubble.left").resizable().scaledToFit().foregroundStyle(.muted)
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }

    static let known: Set<String> = ["bluesky", "gmessages", "imessage", "instagram", "linkedin", "matrix", "messenger",
                                     "signal", "slack", "telegram", "twitter", "wechat", "whatsapp"]
}

struct HubAvatar: View {
    let name: String
    let uri: String
    let client: APIClient
    var size: CGFloat = 44

    var body: some View {
        Group {
            if !uri.isEmpty, let url = HubAvatar.address(uri, client: client) {
                CachedImage(url: url, name: HubAvatar.file(name), client: client) { image in
                    image.resizable().scaledToFill()
                } placeholder: { initial } failure: { AnyView(initial) }
            } else {
                initial
            }
        }
        .frame(width: size, height: size)
        .clipShape(shape)
        .accessibilityHidden(true)
    }

    static func address(_ uri: String, client: APIClient) -> URL? { client.resolve(HubStore.mediaPath(uri, thumbnail: true)) }

    static func file(_ name: String) -> String { "avatar-" + name }

    /// A rounded square, matching the app icon rather than a circle.
    static func shape(_ size: CGFloat) -> RoundedRectangle {
        RoundedRectangle(cornerRadius: size * 0.25, style: .continuous)
    }

    private var shape: RoundedRectangle { HubAvatar.shape(size) }

    private var initial: some View {
        shape.fill(.primary.opacity(PanelStyle.selectedTint)).overlay {
            Text(name.prefix(1).uppercased()).font(.system(size: size * 0.42, weight: .medium)).foregroundStyle(.muted)
        }
    }
}

/// Two lines: the time sits against the name, the unread count against the preview.
struct HubRow: View {
    let title: String
    var preview = ""
    var at: Int64 = 0
    var unread: Int64 = 0
    var opens = false
    let leading: AnyView

    var body: some View {
        HStack(spacing: RowDetails.gap) {
            leading
            VStack(alignment: .leading, spacing: 5) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(title).font(.body.weight(.medium)).lineLimit(1)
                    Spacer(minLength: 4)
                    if opens {
                        Image(systemName: "chevron.right").font(.caption).foregroundStyle(.muted).accessibilityHidden(true)
                    } else if at > 0 {
                        Text(HubRow.when(at)).font(.caption).foregroundStyle(.muted).lineLimit(1).layoutPriority(1)
                    }
                }
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(preview.isEmpty ? " " : preview).font(.subheadline).foregroundStyle(.muted).lineLimit(1)
                    Spacer(minLength: 4)
                    if unread > 0 { UnreadPill(count: Int(unread)) }
                }
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }

    static func when(_ at: Int64) -> String {
        let date = Date(timeIntervalSince1970: Double(at) / 1000)
        let calendar = Calendar.current
        if calendar.isDateInToday(date) { return date.formatted(date: .omitted, time: .shortened) }
        if calendar.isDateInYesterday(date) { return "Yesterday" }
        if calendar.isDate(date, equalTo: .now, toGranularity: .weekOfYear) { return date.formatted(.dateTime.weekday(.wide)) }
        let locale = Locale(identifier: "en_GB")
        if calendar.component(.year, from: date) == calendar.component(.year, from: .now) {
            return date.formatted(.dateTime.day().month(.abbreviated).locale(locale))
        }
        return date.formatted(.dateTime.day().month(.abbreviated).year().locale(locale))
    }
}

struct HubSidebar: View {
    @Bindable var store: HubStore
    let client: APIClient

    var body: some View {
        VStack(spacing: PanelStyle.rowInset) {
            sources
            accountPicker
            spaceHeading
            content
                #if os(macOS)
                .sidebarFade(top: 16, bottom: SidebarFade.switcher)
                #endif
            if let issue = store.issue ?? store.mail.issue {
                QuietNote(text: issue)
                    .task(id: issue) {
                        try? await Task.sleep(for: .seconds(6))
                        store.issue = nil
                        store.mail.issue = nil
                    }
            }
        }
        .padding(.top, PanelStyle.rowInset)
        .task(id: store.source) {
            guard store.source == .mail else { return }
            // Selecting closes what's open, so it's only for a mailbox nothing has chosen yet.
            if store.mail.account != nil {
                await store.mail.load()
            } else if let id = store.mail.accounts.first?.id {
                await store.mail.select(id)
            }
        }
        .panel(item: $store.panel) { panel in
            switch panel {
            case .search: HubFind(store: store, client: client)
            case .chat: HubNewChat(store: store, client: client)
            case .accounts: HubAccounts(store: store)
            case .link: HubMerge(store: store)
            case .list: HubBroadcastEditor(store: store)
            }
        }
    }

    private var options: [(title: String, value: HubSource)] {
        var out: [(title: String, value: HubSource)] = [("All", .all), ("Unread", .unread)]
        out += platforms.map { ($0.name, .platform($0.id)) }
        if !store.mail.accounts.isEmpty { out.append(("Mail", .mail)) }
        out += [("Contacts", .contacts), ("Broadcasts", .broadcasts)]
        return out
    }

    /// Connected platforms, then any others that have chats, such as one with its own bridge.
    private var platforms: [(id: String, name: String)] {
        let connected = store.connected.map { (id: $0.id, name: $0.name) }
        let others = Set(store.chats.filter { !$0.space }.map(\.platform)).subtracting(connected.map(\.id))
            .map { id in (id: id, name: store.status?.platforms.first { $0.id == id }?.name ?? id.capitalized) }
        return connected + others.sorted { $0.name < $1.name }
    }

    private var sources: some View {
        ScrollView(.horizontal) {
            PillPicker("Hub source", selection: $store.source, options: options, compact: true, label: sourceLabel)
                .padding(.horizontal, PanelStyle.rowInset)
        }
        .scrollIndicators(.hidden)
        .edgeFade()
    }

    private func sourceLabel(_ source: HubSource) -> AnyView {
        let title = options.first { $0.value == source }?.title ?? ""
        switch store.selector {
        case HubSettingsSelector.logo: return AnyView(sourceMark(source))
        case HubSettingsSelector.both: return AnyView(HStack(spacing: 5) { sourceMark(source); Text(title) })
        default: return AnyView(Text(title))
        }
    }

    @ViewBuilder private func sourceMark(_ source: HubSource) -> some View {
        switch source {
        case let .platform(id): PlatformMark(platform: id)
        case .all: Image(systemName: "square.grid.2x2")
        case .unread: Image(systemName: "message.badge")
        case .contacts: Image(systemName: "person.2")
        case .broadcasts: Image(systemName: "megaphone")
        case .mail: Image(systemName: "envelope")
        }
    }

    /// Narrows a platform's list to one account, when it has more than one.
    @ViewBuilder private var accountPicker: some View {
        if case let .platform(id) = store.source {
            let accounts = store.accounts(on: id)
            if !accounts.isEmpty {
                Picker("Account", selection: $store.account) {
                    Text("All accounts").tag(String?.none)
                    ForEach(accounts, id: \.id) { account in
                        let unread = store.unread(on: id, account: account.id)
                        Text(unread > 0 ? "\(account.name) (\(unread))" : account.name).tag(String?.some(account.id))
                    }
                }
                .labelsHidden()
                .padding(.horizontal, PanelStyle.headingInset)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    private var focused: HubChat? { store.chats.first { $0.id == store.space } }

    @ViewBuilder private var spaceHeading: some View {
        if let focused {
            HStack(spacing: PanelStyle.rowInset) {
                let platform = store.status?.platforms.first { $0.id == focused.platform }?.name ?? focused.platform.capitalized
                PanelNavButton(symbol: "chevron.backward", label: "Back to \(platform)") {
                    store.space = nil
                }
                Text(focused.name).font(.headline).lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.horizontal, PanelStyle.headingInset)
        }
    }

    @ViewBuilder private var content: some View {
        switch store.source {
        case .contacts: contacts
        case .broadcasts: broadcasts
        case .mail: HubMailList(store: store, client: client)
        case .all, .unread, .platform: chatList
        }
    }

    private var visibleChats: [HubChat] {
        var chats = store.chats
        if case let .platform(id) = store.source { chats = chats.filter { $0.platform == id } }
        if let account = store.account { chats = chats.filter { ($0.accounts ?? []).contains(account) } }
        let rows = store.space.map { HubSpaces.children(of: $0, in: chats, query: "") } ?? HubSpaces.root(chats, query: "")
        return store.source == .unread ? rows.filter { $0.unread > 0 || $0.id == store.chat } : rows
    }

    private func rowFill(_ selected: Bool) -> some View {
        RoundedRectangle(cornerRadius: PanelStyle.rowRadius)
            .fill(.primary.opacity(selected ? PanelStyle.selectedTint : 0))
            .padding(.horizontal, PanelStyle.rowInset)
    }

    private func placeholder(_ text: String) -> some View {
        Text(text).foregroundStyle(.muted).multilineTextAlignment(.center)
            .padding(.horizontal, PanelStyle.contentInset)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private enum Row: Identifiable {
        case chat(HubChat), mail(HubMailEnvelope)

        var id: String {
            switch self {
            case let .chat(value): "chat-" + value.id
            case let .mail(value): "mail-\(value.account)-\(value.folder)-\(value.id)"
            }
        }

        var at: Int64 {
            switch self {
            case let .chat(value): value.at
            case let .mail(value): value.at
            }
        }
    }

    /// Chats, with every inbox's latest messages mixed in on the combined lists.
    private var rows: [Row] {
        let chats = visibleChats.map(Row.chat)
        guard mixed, store.space == nil else { return chats }
        let mail = store.mail.recent.filter { store.source == .all || !$0.seen }.map(Row.mail)
        return (chats + mail).sorted { $0.at > $1.at }
    }

    @ViewBuilder private var chatList: some View {
        let rows = rows
        if store.status?.state == HubStatusState.failed {
            placeholder(store.status?.issue ?? "Hub isn’t available.")
        } else if rows.isEmpty {
            placeholder(store.space == nil ? "No chats yet." : "No chats in this space.")
        } else {
            List(rows) { row in
                switch row {
                case let .chat(chat): chatRow(chat)
                case let .mail(envelope): mailRow(envelope)
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
        }
    }

    private func chatRow(_ chat: HubChat) -> some View {
        Group {
            if chat.space { spaceRow(chat) } else {
                Button { store.open(chat.id) } label: {
                    HubRow(title: chat.name, preview: chat.preview, at: chat.at, unread: chat.unread, leading: AnyView(leading(chat)))
                }
                .accessibilityLabel(chat.name)
            }
        }
        .buttonStyle(.plain)
        .accessibilityValue(chat.unread > 0 ? "\(chat.unread) unread" : "")
        .listRowSeparator(.hidden)
        .listRowBackground(rowFill(chat.id == store.chat))
    }

    private func mailRow(_ envelope: HubMailEnvelope) -> some View {
        Button {
            store.source = .mail
            Task { await store.mail.openRecent(envelope) }
        } label: {
            HubRow(title: envelope.from, preview: envelope.subject, at: envelope.at, unread: envelope.seen ? 0 : 1,
                   leading: AnyView(HubAvatar(name: envelope.from, uri: "", client: client)
                    .overlay(alignment: .bottomTrailing) {
                        Image(systemName: "envelope.circle.fill").font(.system(size: 15)).foregroundStyle(.muted)
                    }))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(envelope.subject.isEmpty ? "No subject" : envelope.subject)
        .accessibilityValue(envelope.from)
        .listRowSeparator(.hidden)
    }

    private func spaceRow(_ space: HubChat) -> some View {
        let count = HubSpaces.children(of: space.id, in: store.chats).count
        return Button {
            store.openSpace(space)
        } label: {
            HubRow(title: space.name, preview: space.preview.isEmpty ? "\(count) chats" : space.preview,
                   unread: space.unread, opens: true, leading: AnyView(leading(space)))
        }
        .accessibilityLabel("Open \(space.name), \(count) chats")
    }

    private func leading(_ chat: HubChat) -> some View {
        HubAvatar(name: chat.name, uri: chat.avatar, client: client)
            .overlay(alignment: .bottomTrailing) {
                if mixed { PlatformMark(platform: chat.platform, size: 15) }
            }
    }

    /// Whether the list holds more than one platform, so each row shows its own.
    private var mixed: Bool { store.source == .all || store.source == .unread }

    @ViewBuilder private var contacts: some View {
        let rows = store.links
        if rows.isEmpty {
            placeholder("No contacts yet.")
        } else {
            List(rows, id: \.id) { link in
                Menu {
                    ForEach(link.members, id: \.chat) { member in
                        Button(member.platform.capitalized) { store.open(member.chat) }
                    }
                    Divider()
                    Button("Unlink", role: .destructive) { Task { await store.unlink(link.id) } }
                } label: {
                    HubRow(title: link.name, preview: link.members.map(\.platform).joined(separator: ", "),
                           unread: link.members.reduce(0) { total, member in
                               total + (store.chats.first { $0.id == member.chat }?.unread ?? 0)
                           },
                           leading: AnyView(HubAvatar(name: link.name, uri: "", client: client)))
                }
                .menuStyle(.button).buttonStyle(.plain).menuIndicator(.hidden)
                .listRowSeparator(.hidden)
            }
            .listStyle(.plain).scrollContentBackground(.hidden)
        }
    }

    @ViewBuilder private var broadcasts: some View {
        let rows = store.broadcasts
        if rows.isEmpty {
            placeholder("No lists yet.")
        } else {
            List(rows, id: \.id) { list in
                Button { store.broadcast = list.id } label: {
                    HubRow(title: list.name, preview: list.messages.last?.text ?? "\(list.recipients.count) people",
                           at: list.messages.last?.at ?? 0,
                           leading: AnyView(Image(systemName: "megaphone").foregroundStyle(.muted).frame(width: 44, height: 44)))
                }
                .buttonStyle(.plain)
                .accessibilityLabel(list.name)
                .listRowSeparator(.hidden)
                .listRowBackground(rowFill(list.id == store.broadcast))
            }
            .listStyle(.plain).scrollContentBackground(.hidden)
        }
    }
}
