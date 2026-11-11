import Foundation
import Observation

/// What Hub's sidebar lists.
public enum HubSource: Hashable, Sendable {
    case all, unread, platform(String), contacts, broadcasts, mail
}

/// A modal Hub shows over its sidebar.
public enum HubPanel: String, Identifiable, Sendable {
    case search, chat, accounts, link, list
    public var id: String { rawValue }
}

public struct HubLogin: Equatable, Sendable {
    public let platform: String
    public var step: HubLoginStep
}

@MainActor @Observable
public final class HubStore {
    public nonisolated static let plugin = "hub"
    /// Mail has no change feed here, so it's asked every sixth status check.
    private nonisolated static let mailEvery = 6
    public private(set) var status: HubStatus?
    public private(set) var chats: [HubChat] = []
    public private(set) var links: [HubLink] = []
    public private(set) var broadcasts: [HubBroadcast] = []
    public private(set) var people: [HubPerson] = []
    public private(set) var messages: [String: [HubMessage]] = [:]
    public private(set) var members: [String: [HubMember]] = [:]
    public private(set) var stickers: [HubAttachment] = []
    public private(set) var searching = false
    public private(set) var login: HubLogin?
    public private(set) var loginBusy = false
    public var issue: String?
    public var source = HubSource.all {
        didSet {
            guard source != oldValue else { return }
            space = nil
            account = nil
        }
    }
    public var space: String?
    /// The one linked account a platform's list is narrowed to.
    public var account: String?
    public var chat: String?
    public var broadcast: String?
    public let mail: HubMailStore
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private var earlier: [String: String] = [:]
    @ObservationIgnored private var revision: Int64?
    @ObservationIgnored private var reading: Set<String> = []

    public init(client: APIClient, cache: LocalCacheActor) {
        self.client = client
        self.cache = cache
        mail = HubMailStore(client: client, cache: cache)
    }

    public var unread: Int { Int(chats.filter { !$0.space }.reduce(0) { $0 + $1.unread }) }

    public var connected: [HubPlatform] { (status?.platforms ?? []).filter { !$0.accounts.isEmpty } }

    public var incognito: Bool { status?.incognito ?? false }

    /// Whether a platform's bridge carries only text, so anything else would vanish on the way.
    public func textOnly(_ platform: String) -> Bool { status?.textOnly.contains(platform) ?? false }
    public var selector: String { status?.selector ?? HubSettingsSelector.text }
    public var panel: HubPanel?

    public func openSpace(_ chat: HubChat) {
        let kept = account
        source = .platform(chat.platform)
        account = kept ?? (chat.accounts?.count == 1 ? chat.accounts?.first : nil)
        space = chat.id
    }

    /// Accounts a platform's list can be narrowed to, or nothing when there's only one.
    public func accounts(on platform: String) -> [HubAccount] {
        let linked = status?.platforms.first { $0.id == platform }?.accounts ?? []
        return linked.count > 1 ? linked : []
    }

    public func unread(on platform: String, account: String) -> Int {
        Int(chats.filter { !$0.space && $0.platform == platform && ($0.accounts ?? []).contains(account) }.reduce(0) { $0 + $1.unread })
    }

    public func restore() async {
        if let saved: [HubChat] = await cache.snapshot("hub-chats"), chats.isEmpty { chats = saved }
        if let saved: HubStatus = await cache.snapshot("hub-status"), status == nil {
            status = saved
            mail.update(accounts: saved.mail)
        }
        await mail.restore()
        if let saved: [HubPerson] = await cache.snapshot("hub-people"), people.isEmpty { people = saved }
        if let saved: [HubLink] = await cache.snapshot("hub-links"), links.isEmpty { links = saved }
        if let saved: [HubBroadcast] = await cache.snapshot("hub-broadcasts"), broadcasts.isEmpty { broadcasts = saved }
    }

    /// Follows the server while Hub is open, and refreshes what surrounds the chats.
    public func run() async {
        await refresh()
        async let lists: Void = followStatus()
        while !Task.isCancelled {
            do {
                var query: [URLQueryItem] = []
                if let revision { query.append(URLQueryItem(name: "since", value: String(revision))) }
                let changes: HubChanges = try await client.pluginGet(Self.plugin, "chats", query: query)
                let moved = revision != nil && revision != changes.revision
                revision = changes.revision
                if chats != changes.chats {
                    chats = changes.chats
                    await cache.saveSnapshot("hub-chats", changes.chats)
                }
                if moved, let chat { await reload(chat) }
            } catch {
                try? await Task.sleep(for: .seconds(3))
            }
        }
        _ = await lists
    }

    private func followStatus() async {
        var ticks = 0
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(5))
            await refresh()
            ticks += 1
            if ticks % Self.mailEvery == 0 { await mail.poll(showing: source == .mail) }
        }
    }

    public func refresh() async {
        if let next: HubStatus = try? await client.pluginGet(Self.plugin, "status"), status != next {
            status = next
            mail.update(accounts: next.mail)
            await cache.saveSnapshot("hub-status", next)
        }
        if let next: [HubLink] = try? await client.pluginGet(Self.plugin, "links"), links != next {
            links = next
            await cache.saveSnapshot("hub-links", next)
        }
        if let next: [HubBroadcast] = try? await client.pluginGet(Self.plugin, "broadcasts"), broadcasts != next {
            broadcasts = next
            await cache.saveSnapshot("hub-broadcasts", next)
        }
    }

    public nonisolated static func mediaPath(_ uri: String, thumbnail: Bool = false) -> String {
        let value = uri.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? ""
        return "plugins/\(plugin)/api/media?uri=\(value)" + (thumbnail ? "&thumbnail=1" : "")
    }

    // MARK: Chats

    public func open(_ id: String) {
        chat = id
        Task {
            await restoreMessages(id)
            await reload(id)
        }
    }

    private static func savedPage(_ id: String) -> String { "hub-messages-" + id }

    private func restoreMessages(_ id: String) async {
        guard messages[id] == nil, let saved: [HubMessage] = await cache.snapshot(Self.savedPage(id)), messages[id] == nil else { return }
        messages[id] = saved
    }

    private func reload(_ id: String) async {
        if await fetch(id) { await markRead(id) }
    }

    /// Takes a chat's latest page and keeps it for next time. Only opening one marks it read.
    private func fetch(_ id: String) async -> Bool {
        guard let page: HubPage = try? await client.pluginGet(Self.plugin, "chats/\(id)/messages") else { return false }
        let pending = (messages[id] ?? []).filter { $0.id.hasPrefix(Self.sendingPrefix) }
        messages[id] = page.messages + pending
        earlier[id] = page.before
        await cache.saveSnapshot(Self.savedPage(id), page.messages)
        return true
    }

    /// Readies the chats most likely to open next, newest first, and hands them back.
    public func preload(_ limit: Int = 10) async -> [HubChat] {
        if chats.isEmpty, let changes: HubChanges = try? await client.pluginGet(Self.plugin, "chats") { chats = changes.chats }
        let recent = Array(chats.filter { !$0.space }.sorted { $0.at > $1.at }.prefix(limit))
        for item in recent where !Task.isCancelled {
            await restoreMessages(item.id)
            _ = await fetch(item.id)
        }
        if status == nil { await refresh() }
        await mail.warm()
        return recent
    }

    private func markRead(_ id: String) async {
        guard let index = chats.firstIndex(where: { $0.id == id }), chats[index].unread > 0,
              let last = messages[id]?.last(where: { !$0.id.hasPrefix(Self.sendingPrefix) })?.id,
              reading.insert(id).inserted else { return }
        chats[index].unread = 0
        try? await client.pluginPerform(Self.plugin, "chats/\(id)/messages/\(last)/read", body: EmptyBody())
        reading.remove(id)
    }

    /// Reports whether a page arrived, so a loop over history stops on a failure.
    @discardableResult
    public func loadOlder(_ id: String) async -> Bool {
        guard let before = earlier[id] else { return false }
        earlier[id] = nil
        let query = [URLQueryItem(name: "before", value: before)]
        guard let page: HubPage = try? await client.pluginGet(Self.plugin, "chats/\(id)/messages", query: query) else {
            earlier[id] = before
            return false
        }
        messages[id] = page.messages + (messages[id] ?? [])
        earlier[id] = page.messages.isEmpty || page.before == before ? nil : page.before
        return true
    }

    /// Pages back to the chat's start, so a search covers all of it.
    public func loadHistory(_ id: String) async {
        searching = true
        defer { searching = false }
        while !Task.isCancelled, await loadOlder(id) {}
    }

    public func loadMembers(_ id: String) async {
        if let next: [HubMember] = try? await client.pluginGet(Self.plugin, "chats/\(id)/members") { members[id] = next }
    }

    public func loadStickers() async {
        if let next: [HubAttachment] = try? await client.pluginGet(Self.plugin, "stickers") { stickers = next }
    }

    public func rename(_ id: String, to name: String) async {
        guard let index = chats.firstIndex(where: { $0.id == id }) else { return }
        let previous = chats[index].name
        chats[index].name = name
        do {
            try await client.pluginPerform(Self.plugin, "chats/\(id)/name", method: "PUT", body: HubRename(name: name))
        } catch {
            if let index = chats.firstIndex(where: { $0.id == id }) { chats[index].name = previous }
            issue = "Couldn’t rename that group."
        }
    }

    public func setIncognito(_ on: Bool) async {
        await save(incognito: on, selector: selector)
    }

    public func setSelector(_ value: String) async {
        await save(incognito: incognito, selector: value)
    }

    private func save(incognito: Bool, selector: String) async {
        guard var next = status else { return }
        let previous = next
        next.incognito = incognito
        next.selector = selector
        status = next
        do {
            try await client.pluginPerform(Self.plugin, "settings", method: "PUT",
                                           body: HubSettings(incognito: incognito, selector: selector))
        } catch {
            status = previous
            issue = "Couldn’t save that."
        }
    }

    private static let sendingPrefix = "sending-"

    /// Shows the message at once; a failure takes it back and returns the text.
    @discardableResult
    public func send(_ text: String, chat id: String, replyTo: String? = nil, file: URL? = nil, mime: String = "",
                     mentions: HubMentions? = nil, duration: Int64? = nil) async -> Bool {
        var attachment: HubAttachment?
        if let file {
            guard let data = try? Data(contentsOf: file),
                  let reply = try? await client.pluginCall(Self.plugin, "media", method: "POST",
                                                           query: [URLQueryItem(name: "name", value: file.lastPathComponent)],
                                                           body: data, contentType: mime),
                  let uri = try? JSONDecoder().decode([String: String].self, from: reply)["uri"] else {
                issue = "Couldn’t send that file."
                return false
            }
            attachment = HubAttachment(uri: uri, name: file.lastPathComponent, mime: mime, size: Int64(data.count),
                                       voice: duration != nil, duration: duration ?? 0)
        }
        return await post(HubSend(id: UUID().uuidString, text: text, replyTo: replyTo, attachment: attachment, mentions: mentions),
                          kind: HubMessageKind.text, chat: id)
    }

    public func sendSticker(_ sticker: HubAttachment, chat id: String) async {
        await post(HubSend(id: UUID().uuidString, text: "", attachment: sticker, sticker: true), kind: HubMessageKind.sticker, chat: id)
    }

    @discardableResult
    private func post(_ message: HubSend, kind: String, chat id: String) async -> Bool {
        let pending = Self.sendingPrefix + message.id
        messages[id, default: []].append(HubMessage(id: pending, chat: id, sender: "", senderName: "", mine: true, text: message.text,
                                                    at: Int64(Date.now.timeIntervalSince1970 * 1000), kind: kind,
                                                    attachment: message.attachment, replyTo: message.replyTo))
        do {
            try await client.pluginPerform(Self.plugin, "chats/\(id)/messages", body: message)
            messages[id]?.removeAll { $0.id == pending }
            await reload(id)
            return true
        } catch {
            messages[id]?.removeAll { $0.id == pending }
            issue = "Message not sent."
            return false
        }
    }

    public func react(_ key: String, message: String, chat id: String) async {
        let previous = messages[id]
        if let index = messages[id]?.firstIndex(where: { $0.id == message }), let shown = messages[id]?[index].reactions {
            messages[id]?[index].reactions = HubReactions.toggled(shown, key: key)
        }
        do {
            try await client.pluginPerform(Self.plugin, "chats/\(id)/messages/\(message)/reactions", body: HubReact(key: key))
            await reload(id)
        } catch {
            messages[id] = previous
            issue = "Reaction not sent."
        }
    }

    // MARK: Connecting

    public func connect(_ platform: String, flow: String = "") async {
        login = nil
        issue = nil
        loginBusy = true
        defer { loginBusy = false }
        do {
            let step: HubLoginStep = try await client.pluginWrite(Self.plugin, "platforms/\(platform)/logins", body: HubLoginStart(flow: flow))
            await advance(platform, step)
        } catch {
            issue = "Couldn’t start that connection."
        }
    }

    private func advance(_ platform: String, _ step: HubLoginStep) async {
        login = HubLogin(platform: platform, step: step)
        if step.kind == HubLoginStepKind.done {
            login = nil
            await refresh()
        } else if step.kind == HubLoginStepKind.display {
            await waitForLogin(platform, step)
        }
    }

    private func waitForLogin(_ platform: String, _ step: HubLoginStep) async {
        let path = "platforms/\(platform)/logins/\(step.login)/steps/\(step.step)/wait"
        do {
            let next: HubLoginStep = try await client.pluginWrite(Self.plugin, path, body: EmptyBody())
            guard login?.step == step else { return }
            await advance(platform, next)
        } catch {
            if login?.step == step {
                login = nil
                issue = "That code expired."
            }
        }
    }

    public func answer(_ values: [String: String]) async {
        guard let login else { return }
        loginBusy = true
        defer { loginBusy = false }
        do {
            let next: HubLoginStep = try await client.pluginWrite(
                Self.plugin, "platforms/\(login.platform)/logins/\(login.step.login)/steps/\(login.step.step)",
                body: HubLoginAnswer(values: values))
            await advance(login.platform, next)
        } catch {
            issue = "That didn’t work."
        }
    }

    public func cancelLogin() {
        guard let login else { return }
        self.login = nil
        Task { try? await client.pluginDelete(Self.plugin, "platforms/\(login.platform)/logins/\(login.step.login)") }
    }

    public func setup(_ platform: String, values: [String: String]) async {
        do {
            try await client.pluginPerform(Self.plugin, "platforms/\(platform)/setup", method: "PUT", body: HubLoginAnswer(values: values))
            await refresh()
        } catch {
            issue = "Couldn’t save that."
        }
    }

    public func disconnect(_ platform: String, account: String) async {
        do {
            try await client.pluginPerform(Self.plugin, "platforms/\(platform)/accounts/\(account)", method: "DELETE", body: EmptyBody())
            await refresh()
        } catch {
            issue = "Couldn’t disconnect that account."
        }
    }

    // MARK: Contacts

    public func loadPeople() async {
        guard let next: [HubPerson] = try? await client.pluginGet(Self.plugin, "people"), next != people else { return }
        people = next
        await cache.saveSnapshot("hub-people", next)
    }

    public func merge(name: String, members: [HubLinkMember]) async {
        do {
            let _: HubLink = try await client.pluginWrite(Self.plugin, "links", body: HubLinkEdit(name: name, members: members))
            await refresh()
        } catch {
            issue = "Couldn’t link those chats."
        }
    }

    public func unlink(_ id: String) async {
        links.removeAll { $0.id == id }
        try? await client.pluginDelete(Self.plugin, "links/\(id)")
    }

    /// Opens a chat with one person, or a named group with several, all on one account.
    public func createChat(_ chosen: [HubPerson], name: String = "") async -> Bool {
        guard let first = chosen.first else { return false }
        if chosen.count == 1, !first.chat.isEmpty {
            show(first.chat)
            return true
        }
        do {
            let input = HubChatCreate(platform: first.platform, account: first.account, people: chosen.map(\.id), name: name)
            let reply: [String: String] = try await client.pluginWrite(Self.plugin, "chats", body: input)
            guard let id = reply["chat"] else { return false }
            show(id)
            return true
        } catch {
            issue = chosen.count == 1 ? "Couldn’t open that chat." : "Couldn’t create that group."
            return false
        }
    }

    /// Leaves a list that wouldn't show the chat, so it opens in view.
    private func show(_ id: String) {
        if source == .contacts || source == .broadcasts || source == .unread { source = .all }
        if source == .mail { source = .all }
        space = nil
        account = nil
        open(id)
    }

    // MARK: Broadcasts

    public func saveBroadcast(name: String, recipients: [HubLinkMember]) async {
        do {
            let value: HubBroadcast = try await client.pluginWrite(Self.plugin, "broadcasts", body: HubBroadcastEdit(name: name, recipients: recipients))
            broadcasts.append(value)
            broadcast = value.id
        } catch {
            issue = "Couldn’t save that list."
        }
    }

    public func removeBroadcast(_ id: String) async {
        broadcasts.removeAll { $0.id == id }
        if broadcast == id { broadcast = nil }
        try? await client.pluginDelete(Self.plugin, "broadcasts/\(id)")
    }

    public func sendBroadcast(_ text: String, id: String) async {
        do {
            let sent: HubBroadcastMessage = try await client.pluginWrite(Self.plugin, "broadcasts/\(id)/messages",
                                                                         body: HubSend(id: "", text: text))
            guard let index = broadcasts.firstIndex(where: { $0.id == id }) else { return }
            broadcasts[index].messages.append(sent)
        } catch {
            issue = "Broadcast not sent."
        }
    }

    // MARK: Mail

    public func addMail(_ setup: HubMailSetup) async -> Bool {
        do {
            let _: HubMailAccount = try await client.pluginWrite(Self.plugin, "mail", body: setup)
            await refresh()
            return true
        } catch {
            issue = "Couldn’t sign in to that mailbox."
            return false
        }
    }

    public func setMailClient(_ provider: String, client value: HubMailClient) async -> Bool {
        do {
            try await client.pluginPerform(Self.plugin, "mail/oauth/\(provider)/client", method: "PUT", body: value)
            await refresh()
            return true
        } catch {
            issue = "Couldn’t save that."
            return false
        }
    }

    /// The server makes the sign-in and redeems it; the app only carries the browser between them.
    public func signIn(_ provider: String, browser: (URL) async throws -> HubMailCallback) async -> Bool {
        do {
            let begun: HubMailSignIn = try await client.pluginWrite(Self.plugin, "mail/oauth/\(provider)", body: EmptyBody())
            guard let url = URL(string: begun.url) else { throw URLError(.badURL) }
            let callback = try await browser(url)
            let _: HubMailAccount = try await client.pluginWrite(Self.plugin, "mail/oauth/\(provider)/sessions/\(begun.id)", body: callback)
            await refresh()
            return true
        } catch is CancellationError {
            return false
        } catch {
            issue = "Couldn’t sign in to that mailbox."
            return false
        }
    }

    public func removeMail(_ id: String) async {
        try? await client.pluginDelete(Self.plugin, "mail/\(id)")
        await refresh()
        guard source == .mail, mail.account == id || mail.account == nil else { return }
        if let next = mail.accounts.first { await mail.select(next.id) } else { source = .all }
    }

    public func editMail(_ id: String, _ setup: HubMailSetup) async -> Bool {
        let saved = await mail.editMailbox(id, setup)
        if saved { await refresh() } else { issue = "Couldn’t sign in to that mailbox." }
        return saved
    }

    public func saveSignatures(_ changes: [String: HubMailSignatures]) async -> Bool {
        for (id, value) in changes where await !mail.saveSignatures(id, value) {
            issue = "Couldn’t save the signatures."
            return false
        }
        await refresh()
        return true
    }

    /// Checks every mailbox, which keeps their status true while Accounts is open.
    public func checkMail() async {
        for item in mail.accounts { _ = await mail.test(item.id) }
        await refresh()
    }
}

private struct EmptyBody: Encodable {}
