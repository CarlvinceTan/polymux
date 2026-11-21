import Foundation

/// How Spaces fold a platform's chats: each Space is one row, and its chats sit behind it.
public enum HubSpaces {
    private static func matches(_ chat: HubChat, _ needle: String) -> Bool {
        needle.isEmpty || chat.name.localizedStandardContains(needle) || chat.preview.localizedStandardContains(needle)
    }

    private static func needle(_ query: String) -> String { query.trimmingCharacters(in: .whitespacesAndNewlines) }

    public static func children(of space: String, in chats: [HubChat], query: String = "") -> [HubChat] {
        let needle = needle(query)
        return chats.filter { !$0.space && $0.parents.contains(space) && matches($0, needle) }
    }

    /// Unfiled chats and one row per Space. A search for a child keeps its Space reachable.
    public static func root(_ chats: [HubChat], query: String = "") -> [HubChat] {
        let needle = needle(query)
        let spaces = chats.filter(\.space)
        let ids = Set(spaces.map(\.id))
        let summaries = spaces.compactMap { space -> HubChat? in
            let inside = children(of: space.id, in: chats)
            guard matches(space, needle) || inside.contains(where: { matches($0, needle) }) else { return nil }
            return summary(space, inside)
        }
        // A parent that isn't listed is the bridge's own envelope, so those chats stay unfiled.
        let loose = chats.filter { !$0.space && ids.isDisjoint(with: $0.parents) && matches($0, needle) }
        return (summaries + loose).sorted { $0.at == $1.at ? $0.id < $1.id : $0.at > $1.at }
    }

    /// A Space row borrows its chats' activity, unread total and names.
    public static func summary(_ space: HubChat, _ children: [HubChat]) -> HubChat {
        var value = space
        value.at = children.map(\.at).max() ?? space.at
        value.preview = children.map(\.name).joined(separator: ", ")
        value.unread = children.reduce(0) { $0 + $1.unread }
        return value
    }
}

public enum HubSearchFilter: String, CaseIterable, Sendable {
    case all, messages, media, files, links
    public var title: String { rawValue.capitalized }
}

public enum HubResultKind: Sendable { case message, media, file, link }

public struct HubSearchResult: Identifiable, Hashable, Sendable {
    public let id: String
    public let kind: HubResultKind
    public let message: HubMessage
    public let title: String
    public let detail: String?
}

/// One searchable view of everything a chat has shared. A result points back to its message.
public enum HubSearch {
    public static func results(_ messages: [HubMessage], query: String, filter: HubSearchFilter) -> [HubSearchResult] {
        let needle = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return messages.reversed().filter { $0.kind != HubMessageKind.notice }.flatMap { message in
            found(in: message).filter { pass($0, filter) && hit($0, needle) }
        }
    }

    private static func found(in message: HubMessage) -> [HubSearchResult] {
        var out: [HubSearchResult] = []
        let body = message.text.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        if !body.isEmpty {
            out.append(HubSearchResult(id: message.id + ":message", kind: .message, message: message, title: body, detail: nil))
        }
        if let file = message.attachment {
            let kind: HubResultKind = message.kind == HubMessageKind.file ? .file : .media
            let title = file.name.isEmpty ? message.kind.capitalized : file.name
            out.append(HubSearchResult(id: message.id + ":attachment", kind: kind, message: message, title: title, detail: file.mime))
        }
        for (index, link) in links(in: message.text).enumerated() {
            out.append(HubSearchResult(id: "\(message.id):link:\(index)", kind: .link, message: message, title: host(link), detail: link))
        }
        return out
    }

    private static func pass(_ result: HubSearchResult, _ filter: HubSearchFilter) -> Bool {
        switch filter {
        case .all: true
        case .messages: result.kind == .message
        case .media: result.kind == .media
        case .files: result.kind == .file
        case .links: result.kind == .link
        }
    }

    private static func hit(_ result: HubSearchResult, _ needle: String) -> Bool {
        guard !needle.isEmpty else { return true }
        let message = result.message
        return [result.title, result.detail ?? "", message.text, message.senderName, message.sender, message.attachment?.name ?? ""]
            .contains { $0.localizedStandardContains(needle) }
    }

    /// Trims trailing punctuation, so a link in a sentence is the link someone meant.
    public static func links(in text: String) -> [String] {
        var out: [String] = []
        for match in text.matches(of: /https?:\/\/[^\s<>]+/.ignoresCase()) {
            var link = String(match.output)
            while let last = link.last, ".,!?;:".contains(last) { link.removeLast() }
            if link.hasSuffix(")"), link.filter({ $0 == "(" }).count < link.filter({ $0 == ")" }).count { link.removeLast() }
            if !link.isEmpty, !out.contains(link) { out.append(link) }
        }
        return out
    }

    private static func host(_ link: String) -> String {
        guard let host = URL(string: link)?.host() else { return link }
        return host.hasPrefix("www.") ? String(host.dropFirst(4)) : host
    }
}

/// The unfinished @handle at the caret, in characters.
public struct HubToken: Equatable, Sendable {
    public let start: Int
    public let end: Int
    public let query: String
}

public struct HubSuggestion: Identifiable, Equatable, Sendable {
    public let id: String
    public let label: String
    public let value: String
    public let detail: String
    public let avatar: String
}

/// Mention text in a draft. The Matrix id travels beside the handle, so a display name never decides who's notified.
public enum HubMentionText {
    private static let limit = 6
    public static let everyone = "@everyone"

    private static func handleCharacter(_ value: Character) -> Bool {
        value.isLetter || value.isNumber || "_.-".contains(value)
    }

    /// Text before the caret decides this, so editing mid-draft behaves like normal typing.
    public static func token(in text: String, cursor: Int) -> HubToken? {
        let before = Array(text.prefix(max(0, cursor)))
        var index = before.count
        while index > 0, handleCharacter(before[index - 1]) { index -= 1 }
        guard index > 0, before[index - 1] == "@" else { return nil }
        if index > 1 {
            let previous = before[index - 2]
            guard previous.isWhitespace || "([{".contains(previous) else { return nil }
        }
        return HubToken(start: index - 1, end: before.count, query: String(before[index...]))
    }

    /// A readable handle, safe on every platform.
    public static func handle(_ member: HubMember) -> String {
        let folded = member.name.decomposedStringWithCompatibilityMapping
            .unicodeScalars.filter { !(0x300...0x36f).contains($0.value) }
        var name = String(String.UnicodeScalarView(folded)).lowercased()
        if name.hasPrefix("@") { name.removeFirst() }
        if let cleaned = clean(name) { return "@" + cleaned }
        let localpart = member.id.dropFirst().prefix { $0 != ":" }
        let remote = localpart.firstIndex(of: "_").map { localpart[localpart.index(after: $0)...] } ?? localpart
        return "@" + (clean(String(remote)) ?? "user")
    }

    /// One . - or _ stays; any other gap between words becomes one _.
    private static func clean(_ text: String) -> String? {
        var out = ""
        var gap = ""
        for value in text {
            guard value.isLetter || value.isNumber else {
                gap.append(handleCharacter(value) ? value : " ")
                continue
            }
            if !out.isEmpty, !gap.isEmpty { out += gap.count == 1 && gap != " " ? gap : "_" }
            gap = ""
            out.append(value)
        }
        return out.isEmpty ? nil : out
    }

    private static func handles(_ members: [HubMember]) -> [(member: HubMember, handle: String)] {
        var used: [String: Int] = [:]
        return members.map { member in
            let base = handle(member)
            used[base, default: 0] += 1
            let count = used[base] ?? 1
            return (member, count == 1 ? base : "\(base)_\(count)")
        }
    }

    public static func suggestions(_ token: HubToken?, members: [HubMember], group: Bool) -> [HubSuggestion] {
        guard let token else { return [] }
        let query = token.query.lowercased()
        var out: [HubSuggestion] = []
        if group, query.isEmpty || "everyone".contains(query) {
            out.append(HubSuggestion(id: everyone, label: "Everyone", value: everyone, detail: "All members", avatar: ""))
        }
        for (member, handle) in handles(members) where query.isEmpty || "\(member.name) \(handle)".lowercased().contains(query) {
            out.append(HubSuggestion(id: member.id, label: member.name, value: handle, detail: handle, avatar: member.avatar))
        }
        return Array(out.prefix(limit))
    }

    public static func replace(_ text: String, _ token: HubToken, with value: String) -> (text: String, caret: Int) {
        let characters = Array(text)
        let after = String(characters[min(token.end, characters.count)...])
        let spaced = after.first.map { $0.isWhitespace || ",.;:!?)".contains($0) } == true
        let inserted = value + (spaced ? "" : " ")
        return (String(characters[..<token.start]) + inserted + after, token.start + inserted.count)
    }

    /// Rebuilt from the text at send, so editing a mention by hand can't leave a stale target.
    public static func mentions(in text: String, members: [HubMember], group: Bool) -> HubMentions? {
        let users = handles(members).compactMap { present(text, $0.handle) ? HubMention(id: $0.member.id, label: $0.handle) : nil }
        let all = group && present(text, everyone)
        return users.isEmpty && !all ? nil : HubMentions(users: users, everyone: all)
    }

    private static func present(_ text: String, _ label: String) -> Bool {
        var rest = text[...]
        while let range = rest.range(of: label) {
            let before = range.lowerBound > text.startIndex ? text[text.index(before: range.lowerBound)] : nil
            let after = range.upperBound < text.endIndex ? text[range.upperBound] : nil
            if !(before.map(handleCharacter) ?? false), !(after.map(handleCharacter) ?? false) { return true }
            rest = text[range.upperBound...]
        }
        return false
    }

    /// A failed member list can still offer people who've written here.
    public static func merge(_ members: [HubMember], _ messages: [HubMessage]) -> [HubMember] {
        var known = Dictionary(members.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for message in messages where !message.mine && message.kind != HubMessageKind.notice && message.sender.hasPrefix("@") && message.sender.contains(":") && known[message.sender] == nil {
            known[message.sender] = HubMember(id: message.sender, name: message.senderName, avatar: "")
        }
        return known.values.sorted { $0.name == $1.name ? $0.id < $1.id : $0.name < $1.name }
    }
}
