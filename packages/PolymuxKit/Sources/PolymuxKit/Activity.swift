import Foundation

extension Activity {
    public var countsUnread: Bool { kind == "agent.messaged" || kind == "subagent.finished" }
}
