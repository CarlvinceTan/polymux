import SwiftUI
import PolymuxKit

struct AgentRow: View {
    let agent: Agent
    let thread: PolymuxKit.Thread?
    let duplicate: Bool
    var gap: CGFloat? = nil
    var activity: ActivityStore?
    var connected = true
    /// The message to preview while a newer reply is still streaming.
    var finished: Message? = nil

    var body: some View {
        #if DEBUG
        let _ = RenderCount.hit("row")
        #endif
        HStack(spacing: gap ?? RowDetails.gap) {
            AgentAvatar(agentID: agent.id, conversationID: thread?.id ?? agent.id,
                        runs: thread?.runs ?? [], activity: activity, connected: connected,
                        unread: unread, showsMarker: false)
                .frame(width: avatarSize, height: avatarSize)
                #if os(macOS)
                .frame(width: 34)
                #endif
                .accessibilityHidden(true)
            RowDetails(name: agent.name, role: agent.role.trimmingCharacters(in: .whitespacesAndNewlines),
                       preview: preview, thread: thread, marker: presentation.marker)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(agent.name), \(agent.role), \(preview)")
        .accessibilityValue([agent.active ? "Active" : nil, presentation.marker?.label, presentation.label]
            .compactMap { $0 }.joined(separator: ", "))
    }

    private var unread: Bool { (thread?.unreadCount ?? 0) > 0 }

    private var avatarSize: CGFloat {
        #if os(iOS)
        60
        #else
        52
        #endif
    }

    private var presentation: ActivityView {
        activity?.view(conversationID: thread?.id ?? agent.id, agentID: agent.id, connected: connected, unread: unread)
            ?? ActivityStore.present(runs: thread?.runs ?? [], conversationID: thread?.id ?? agent.id,
                                     agentID: agent.id, connected: connected, unread: unread)
    }

    private var preview: String {
        guard let message = previewMessage else { return duplicate ? agent.role : "" }
        let text: String
        if let question = message.parts.first(where: { $0.kind == PartKind.choice })?.question, !question.isEmpty {
            text = question
        } else if !message.text.isEmpty {
            text = message.text.replacingOccurrences(of: "\n", with: " ")
        } else if let name = message.parts.compactMap(\.attachment).first?.name {
            text = name
        } else {
            text = ""
        }
        return duplicate ? "\(agent.role) · \(agent.id.prefix(6)) · \(text)" : text
    }

    private var previewMessage: Message? {
        guard let last = thread?.lastMessage else { return finished }
        return last.status == MessageStatus.streaming ? finished : last
    }
}

struct GroupRow: View {
    let thread: PolymuxKit.Thread
    let members: [Agent]
    var activity: ActivityStore?
    var connected = true
    /// The message to preview while a newer reply is still streaming.
    var finished: Message? = nil

    var body: some View {
        HStack(spacing: RowDetails.gap) {
            #if os(macOS)
            AvatarStack(members: members, isGroup: true, conversationID: thread.id, runs: thread.runs, activity: activity,
                        connected: connected, showsMarker: false).frame(width: 34)
            #else
            AvatarStack(members: members, isGroup: true, size: 60, conversationID: thread.id, runs: thread.runs, activity: activity,
                        connected: connected, showsMarker: false)
            #endif
            RowDetails(name: thread.title, preview: preview, thread: thread, marker: marker)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityLabel([thread.title, memberLabel, preview]
            .filter { !$0.isEmpty }.joined(separator: ", "))
        .accessibilityValue(marker?.label ?? "")
    }

    private var marker: ActivityMarker? {
        if let activity {
            return activity.marker(conversationID: thread.id, agentIDs: members.map(\.id), unread: thread.unreadCount > 0)
        }
        return ActivityStore.marker(runs: thread.runs, conversationID: thread.id, agentIDs: members.map(\.id),
                                    unread: thread.unreadCount > 0)
    }

    private var memberLabel: String {
        let names = members.map(\.name).joined(separator: ", ")
        return names == thread.title ? "" : names
    }

    private var preview: String {
        // An empty chat shows nothing: the row's name already lists the members.
        guard let message = previewMessage else { return "" }
        if !message.text.isEmpty { return message.text.replacingOccurrences(of: "\n", with: " ") }
        return message.attachments.first?.name ?? ""
    }

    private var previewMessage: Message? {
        guard let last = thread.lastMessage else { return finished }
        return last.status == MessageStatus.streaming ? finished : last
    }
}

struct RowDetails: View {
    let name: String
    var role = ""
    let preview: String
    let thread: PolymuxKit.Thread?
    let marker: ActivityMarker?

    static var gap: CGFloat {
        #if os(macOS)
        8
        #else
        10
        #endif
    }

    private var foreground: Color {
        #if os(macOS)
        Color(nsColor: .labelColor)
        #else
        .primary
        #endif
    }

    private var secondary: Color {
        #if os(macOS)
        Color(nsColor: .secondaryLabelColor)
        #else
        .secondary
        #endif
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(name)
                    #if os(iOS)
                    .font(.headline)
                    #else
                    .font(.body.weight(.medium))
                    #endif
                    .foregroundStyle(foreground).lineLimit(1)
                    .layoutPriority(1)
                if !role.isEmpty {
                    // Secondary text yields first: a long role truncates before it can
                    // squeeze out the name or the date.
                    Text(role).font(.caption).foregroundStyle(secondary).lineLimit(1)
                }
                Spacer(minLength: 4)
                if thread?.pinned == true {
                    Image(systemName: "pin.fill")
                        .font(.caption2).foregroundStyle(secondary)
                        .accessibilityLabel("Pinned")
                }
                if let date = thread?.lastMessage?.date {
                    Text(date, format: ProgressiveTime())
                        .font(.caption2).foregroundStyle(secondary).lineLimit(1).layoutPriority(2)
                }
            }
            if !preview.isEmpty || marker != nil {
                HStack(spacing: 6) {
                    if preview.isEmpty {
                        Spacer(minLength: 0)
                    } else {
                        Text(preview)
                            #if os(iOS)
                            .font(.subheadline)
                            #else
                            .font(.caption)
                            #endif
                            .lineLimit(1).foregroundStyle(secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    if let marker {
                        ActivityDot(marker: marker).accessibilityHidden(true)
                    }
                }
            }
        }
    }
}
