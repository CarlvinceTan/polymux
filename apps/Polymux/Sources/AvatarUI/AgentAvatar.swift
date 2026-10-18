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
        AvatarView(options: avatars.options(for: agentID),
                   state: StateID(rawValue: view.animation) ?? .idle,
                    expression: view.expression.flatMap(ExpressionID.init(rawValue:)),
                    frozenAt: view.animation == "notify" && view.cueID == nil ? POSES[.notify] : nil,
                    overlapGap: overlapGap, repeating: view.working || speaking, cueID: view.cueID)
            .accessibilityLabel(view.label)
    }
}
