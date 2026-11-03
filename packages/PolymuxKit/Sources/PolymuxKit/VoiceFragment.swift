public struct VoiceFragment: Sendable, Equatable {
    public var text: String
    public var agentID: String?

    public init(text: String, agentID: String? = nil) {
        self.text = text
        self.agentID = agentID
    }
}
