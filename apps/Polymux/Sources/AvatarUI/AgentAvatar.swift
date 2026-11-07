import SwiftUI
import AvatarKit
import PolymuxKit

struct AgentAvatar: View {
    @Environment(AvatarStore.self) private var avatars
    let agentID: String
    var conversationID = ""
    var runs: [Run] = []
    var activity: ActivityStore?
    var connected = true
    var unread = false
    var showsMarker = true
    var speaking = false
    var overlapGap: CGFloat = 0

    private var presentation: ActivityView {
        activity?.view(conversationID: conversationID, agentID: agentID, connected: connected,
                       unread: unread, speaking: speaking)
        ?? ActivityStore.present(runs: runs, conversationID: conversationID, agentID: agentID,
                                 connected: connected, unread: unread, speaking: speaking)
    }

    var body: some View {
        let view = presentation
        // Idle bots rest; only activity plays their clips.
        AvatarView(options: avatars.options(for: agentID),
                   state: StateID(rawValue: view.animation) ?? .idle,
                   expression: view.expression.flatMap(ExpressionID.init(rawValue:)),
                   playing: view.animating,
                   overlapGap: overlapGap, repeating: view.working || speaking, cueID: view.cueID,
                   poke: avatars.pokes[agentID], mood: view.mood == "idle" ? nil : view.mood.flatMap(MoodID.init(rawValue:)),
                   interactive: view.mood == "idle" && view.expression == nil, personality: agentID)
            .activityBadge(showsMarker ? view.marker : nil)
            .anchorPreference(key: AvatarAnchors.self, value: .bounds) {
                avatars.options(for: agentID).animates ? [agentID: $0] : [:]
            }
            .accessibilityLabel(view.label)
            .accessibilityValue(view.marker?.label ?? "")
    }
}
