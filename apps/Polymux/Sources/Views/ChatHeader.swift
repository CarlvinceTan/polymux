import SwiftUI
import PolymuxKit

struct ChatHeader: View {
    let title: String
    let subtitle: String
    let members: [Agent]
    var conversationID = ""
    var runs: [Run] = []
    var activity: ActivityStore?
    var connected = true
    var speakingID: String?
    let edit: () -> Void

    var body: some View {
        Button(action: edit) {
            VStack(spacing: -4) {
                AvatarStack(members: members, size: 80, height: 46, conversationID: conversationID,
                            runs: runs, activity: activity, connected: connected, speakingID: speakingID)
                .zIndex(1)
                .accessibilityHidden(true)
                VStack(spacing: 2) {
                    Text(title).font(.headline).lineLimit(1)
                    if !subtitle.isEmpty {
                        Text(subtitle).font(.caption).foregroundStyle(.secondary)
                            .lineLimit(1).truncationMode(.tail)
                    }
                }
                .padding(.horizontal, 16).padding(.vertical, 6)
                .glassEffect(.regular.interactive(), in: Capsule())
            }
            .frame(maxWidth: 260)
            .fixedSize(horizontal: false, vertical: true)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
