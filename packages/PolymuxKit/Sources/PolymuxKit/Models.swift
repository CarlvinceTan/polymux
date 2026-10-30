import Foundation

extension Agent: Identifiable {}
extension Provider: Identifiable {}
extension Thread: Identifiable {
    public var isGroup: Bool { id != agentID }
    public var participantIDs: [String] { agentIDs.isEmpty ? [agentID] : agentIDs }
}
extension MessageStatus {
    /// Client-only: a send the outbox stopped retrying.
    public static let failed = "failed"
}
extension Reaction {
    public static let emojis = ["❤️", "👍", "👎", "😂", "‼️", "❓"]
    /// Apple's starting recents, minus the fixed tapbacks; fills the row until there is history.
    public static let defaultRecents = ["😍", "😒", "👌", "😊", "😘"]

    public static func isEmoji(_ text: String) -> Bool {
        guard text.utf8.count <= 128, text.count == 1,
              let first = text.unicodeScalars.first, first.properties.isEmoji else { return false }
        guard first.value > 127 || text.unicodeScalars.contains(where: { $0.value == 0x20E3 }) else { return false }
        return text.unicodeScalars.allSatisfy {
            $0.properties.isEmoji || [0x200D, 0x20E3, 0xFE0E, 0xFE0F].contains($0.value) || (0xE0020...0xE007F).contains($0.value)
        }
    }

    public static func suggestions(recent: [String]) -> [String] {
        var choices: [String] = []
        for emoji in recent where isEmoji(emoji) && !emojis.contains(emoji) {
            if !choices.contains(emoji) { choices.append(emoji) }
            if choices.count == 5 { break }
        }
        return choices
    }
}
extension Message: Identifiable {
    public var text: String { parts.compactMap(\.text).joined() }
    public var attachments: [Attachment] { parts.compactMap(\.attachment) }
    public var date: Date { WireDate.parse(createdAt) ?? .distantPast }
    public var replyContext: ReplyContext {
        ReplyContext(messageID: id, role: role, text: text, attachment: attachments.first, agentID: agentID)
    }

    public func replyContext(part index: Int) -> ReplyContext? {
        guard parts.indices.contains(index) else { return nil }
        let part = parts[index]
        switch part.kind {
        case PartKind.text:
            guard let text = part.text, !text.isEmpty else { return nil }
            return ReplyContext(messageID: id, role: role, text: text, agentID: agentID, partIndex: Int64(index))
        case PartKind.attachment:
            guard let attachment = part.attachment else { return nil }
            return ReplyContext(messageID: id, role: role, attachment: attachment, agentID: agentID, partIndex: Int64(index))
        default:
            return nil
        }
    }
}
extension Attachment: Identifiable {}
extension Attachment {
    /// The composer's file while the upload is still in flight. Server attachments never use file URLs.
    public var localURL: URL? {
        let file = URL(string: url)
        return file?.isFileURL == true ? file : nil
    }
}
extension SearchHit: Identifiable {
    public var id: String { message.id }
}

extension ChoiceAnswer {
    /// Recorded instead of an option id when the user skips a question card.
    public static let dismissed = "_dismissed"
}

public enum WireDate {
    private static let lock = NSLock()
    private nonisolated(unsafe) static let fractional: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        formatter.timeZone = .gmt
        return formatter
    }()
    private nonisolated(unsafe) static let plain: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        formatter.timeZone = .gmt
        return formatter
    }()

    public static func string(_ date: Date = .now) -> String {
        lock.lock()
        defer { lock.unlock() }
        return fractional.string(from: date)
    }

    public static func parse(_ value: String) -> Date? {
        lock.lock()
        defer { lock.unlock() }
        if let date = fractional.date(from: value) { return date }
        return plain.date(from: value)
    }
}

/// A composer file handed to `ChatStore.send` before the server knows about it.
public struct LocalFile: Identifiable, Hashable, Sendable {
    public let id: String
    public let url: URL
    public let name: String
    public let kind: String
    public let mimeType: String
    public let size: Int64

    public init(id: String = UUID().uuidString, url: URL, name: String? = nil, kind: String = "",
                mimeType: String = "", size: Int64 = 0) {
        self.id = id
        self.url = url
        self.name = name ?? url.lastPathComponent
        self.kind = kind
        self.mimeType = mimeType
        self.size = size
    }

    /// The optimistic attachment the bubble renders until the upload replaces it.
    public var placeholder: Attachment {
        Attachment(id: id, kind: kind, name: name, mimeType: mimeType, size: size,
                   url: url.absoluteString, indexingState: AttachmentIndexingState.pending)
    }
}

public enum ClientError: LocalizedError, Sendable {
    case response(Int, String)
    case invalidURL
    case invalidResponse
    case unavailable
    case cache(String)
    case gap(expected: Int64, actual: Int64)

    public var errorDescription: String? {
        switch self {
        case let .response(_, message): message
        case .invalidURL: "The server address is invalid."
        case .invalidResponse: "The server returned an unexpected response."
        case .unavailable: "Couldn’t complete this request."
        case let .cache(message): "Local cache: \(message)"
        case let .gap(expected, actual): "Conversation replay skipped event \(expected) and received \(actual)."
        }
    }
}
