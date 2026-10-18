import Foundation

public struct Activity: Codable, Hashable, Sendable {
    public var kind: String
    public var verb: String
    public var objectID: String
    public var objectLabel: String
    public var iconOrAgentID: String
    public var at: String
    public var agentIDs: [String]?
    public var messageIDs: [String]?
    public var countsUnread: Bool { kind == "agent.messaged" || kind == "subagent.finished" }

    public init(kind: String, verb: String, objectID: String = "", objectLabel: String = "",
                iconOrAgentID: String = "", at: String = WireDate.string(), agentIDs: [String]? = nil, messageIDs: [String]? = nil) {
        self.kind = kind
        self.verb = verb
        self.objectID = objectID
        self.objectLabel = objectLabel
        self.iconOrAgentID = iconOrAgentID
        self.at = at
        self.agentIDs = agentIDs
        self.messageIDs = messageIDs
    }
}
