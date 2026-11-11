import Foundation

/// How someone's state reads on the map: an icon, a few words and which tint to wear. Pure, so every state can be checked.
public struct WorldTagStyle: Hashable, Sendable {
    public var symbol: String
    public var text: String
    public var tint: String

    /// Nil while someone is resting: no tag, no bubble.
    public static func tag(state: String?, cargo: String?, doing: String?, subtitle: String?) -> WorldTagStyle? {
        switch state {
        case WorkNodeState.working:
            let said = [doing, subtitle].compactMap { $0 }.first { !$0.isEmpty }
            let what = cargo.flatMap { WorkKind.known($0) ? WorkKind.look($0).title : nil }
            return WorldTagStyle(symbol: cargo.map { WorkKind.look($0).symbol } ?? "ellipsis", text: said ?? what ?? "Working", tint: cargo ?? "")
        case WorkNodeState.waiting:
            return WorldTagStyle(symbol: "questionmark", text: doing?.isEmpty == false ? doing ?? "" : "Waiting for you", tint: "orange")
        case WorkNodeState.failed:
            return WorldTagStyle(symbol: "exclamationmark", text: doing?.isEmpty == false ? doing ?? "" : "Failed", tint: "red")
        default:
            return nil
        }
    }
}
