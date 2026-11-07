import SwiftUI
import PolymuxKit

struct AvatarStack: View {
    @Environment(AvatarStore.self) private var avatars
    let members: [Agent]
    var isGroup = false
    var size: CGFloat = 52
    var height: CGFloat? = nil
    var conversationID = ""
    var runs: [Run] = []
    var activity: ActivityStore?
    var connected = true
    var unread = false
    var showsMarker = true
    var speakingID: String?
    @State private var groupID = UUID()

    var body: some View {
        ZStack {
            ForEach(Array(members.prefix(4).enumerated()), id: \.element.id) { index, member in
                let gap = index == count - 1 ? 0 : faceSize * 200 / 316 * 2 / 46
                Group {
                    if isGroup {
                        AvatarView(options: avatars.options(for: member.id), playing: false,
                                   frozenAt: 0, overlapGap: gap)
                    } else {
                        AgentAvatar(agentID: member.id, conversationID: conversationID, runs: runs,
                                     activity: activity, connected: connected, showsMarker: false, speaking: speakingID == member.id,
                                    overlapGap: gap)
                    }
                }
                    .environment(\.avatarGroup, AvatarGroup(id: groupID, order: index))
                    .frame(width: faceSize, height: faceSize)
                    .offset(offset(index))
                    .zIndex(Double(count - index))
            }
        }
        .frame(width: size, height: height ?? size)
        .avatarPlane()
        .activityBadge(showsMarker ? marker : nil)
        .accessibilityHidden(true)
    }

    private var marker: ActivityMarker? {
        if let activity {
            return activity.marker(conversationID: conversationID, agentIDs: members.map(\.id), unread: unread)
        }
        return ActivityStore.marker(runs: runs, conversationID: conversationID, agentIDs: members.map(\.id), unread: unread)
    }

    private var count: Int { min(4, members.count) }
    private var gap: CGFloat { size > 60 ? 14 : 10 }
    private var faceSize: CGFloat {
        if let height {
            let span: CGFloat = switch count {
            case 2: 1.6
            case 3: 1.5
            case 4: 1.8
            default: 1
            }
            return height / span * 316 / 200
        }
        // Groups fill the same footprint as a pair: three span the same width, four a wider grid.
        guard count > 1 else { return size }
        return (size - gap) * (count == 4 ? 1.6 / 1.8 : 1)
    }

    private func offset(_ index: Int) -> CGSize {
        let diameter = faceSize * 200 / 316
        switch count {
        case 2:
            let offset = (CGFloat(index) - 0.5) * diameter * 0.6
            return CGSize(width: offset, height: offset)
        case 3:
            return CGSize(width: index == 0 ? 0 : (index == 1 ? -diameter * 0.3 : diameter * 0.3),
                          height: index == 0 ? -diameter * 0.25 : diameter * 0.25)
        case 4:
            return CGSize(width: (index.isMultiple(of: 2) ? -1 : 1) * diameter * 0.4,
                          height: (index < 2 ? -1 : 1) * diameter * 0.4)
        default:
            return .zero
        }
    }
}
