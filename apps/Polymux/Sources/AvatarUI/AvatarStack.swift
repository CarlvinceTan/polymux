import SwiftUI
import PolymuxKit

struct AvatarStack: View {
    let members: [Agent]
    var size: CGFloat = 52
    var height: CGFloat? = nil
    var conversationID = ""
    var runs: [Run] = []
    var activity: ActivityStore?
    var connected = true
    var speakingID: String?

    var body: some View {
        ZStack {
            ForEach(Array(members.prefix(4).enumerated()), id: \.element.id) { index, member in
                AgentAvatar(agentID: member.id, conversationID: conversationID, runs: runs,
                           activity: activity, connected: connected, speaking: speakingID == member.id,
                           overlapGap: index == count - 1 ? 0 : faceSize * 200 / 316 * 2 / 46)
                    .frame(width: faceSize, height: faceSize)
                    .offset(offset(index))
                    .zIndex(Double(count - index))
            }
        }
        .compositingGroup().frame(width: size, height: height ?? size)
        .accessibilityHidden(true)
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
        return size - CGFloat(max(0, min(3, count) - 1)) * gap
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
