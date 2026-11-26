#if os(macOS)
import AppKit
import SwiftUI
import PolymuxKit

/// The menu bar icon: Polymux's mark, with a count of unread chats.
struct StatusLabel: View {
    let bar: StatusBar

    var body: some View {
        let unread = bar.unread
        HStack(spacing: 2) {
            Image(systemName: bar.working.isEmpty ? "bubble.left.and.bubble.right" : "bubble.left.and.bubble.right.fill")
            if unread > 0 { Text("\(unread)") }
        }
        .accessibilityLabel(unread > 0 ? "Polymux, \(unread) unread" : "Polymux")
    }
}

/// The menu bar window: chats with bots at work, then recent chats.
struct StatusMenu: View {
    let bar: StatusBar

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let session = bar.session {
                chats.environment(session.avatars)
            } else {
                Text("Connecting…")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, minHeight: 80)
            }
            Divider().padding(.vertical, 6)
            footer
        }
        .padding(8)
        .frame(width: 320)
    }

    @ViewBuilder private var chats: some View {
        let working = bar.working
        let recent = bar.recent
        if working.isEmpty && recent.isEmpty {
            Text("No chats yet.")
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, minHeight: 80)
        }
        if !working.isEmpty {
            heading("Working")
            ForEach(working) { StatusRow(entry: $0, bar: bar) }
        }
        if !recent.isEmpty {
            heading("Recent")
            ForEach(recent) { StatusRow(entry: $0, bar: bar) }
        }
    }

    private func heading(_ title: String) -> some View {
        Text(title)
            .font(.caption.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 8)
            .padding(.top, 6)
            .padding(.bottom, 2)
            .accessibilityAddTraits(.isHeader)
    }

    private var footer: some View {
        HStack {
            Button("Open") { bar.show() }
            Button("Settings") {
                bar.show()
                URLRouter.shared.handler?(URL(string: "polymux://settings")!)
            }
            Spacer()
            Button("Quit") { NSApp.terminate(nil) }
        }
        .buttonStyle(.borderless)
        .padding(.horizontal, 8)
        .padding(.bottom, 2)
    }
}

/// One chat in the menu; a click opens it in the window.
struct StatusRow: View {
    let entry: StatusBar.Entry
    let bar: StatusBar
    @State private var hovered = false

    var body: some View {
        Button { bar.open(entry.id) } label: {
            HStack(spacing: 8) {
                Group {
                    if entry.thread.isGroup {
                        AvatarStack(members: entry.members, isGroup: true, size: 32, conversationID: entry.id,
                                    runs: entry.thread.runs, activity: bar.session?.sidebar.activity, showsMarker: false)
                    } else {
                        AgentAvatar(agentID: entry.agentID, conversationID: entry.id, runs: entry.thread.runs,
                                    activity: bar.session?.sidebar.activity, showsMarker: false)
                    }
                }
                .frame(width: 32, height: 32)
                .accessibilityHidden(true)
                RowDetails(name: entry.name, preview: entry.preview, thread: entry.thread, marker: entry.marker)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 5)
            .background(hovered ? Color.primary.opacity(0.08) : .clear, in: .rect(cornerRadius: 6))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .onHover { hovered = $0 }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(entry.name), \(entry.preview)")
        .accessibilityValue(entry.working ? "Working" : entry.marker?.label ?? "")
    }
}
#endif
