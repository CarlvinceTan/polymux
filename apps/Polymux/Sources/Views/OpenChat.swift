import SwiftUI
import PolymuxKit

/// The chat on screen, as the profile panel beside it sees it.
@MainActor @Observable
final class OpenChat {
    private(set) var store: ChatStore?
    @ObservationIgnored private var scroll: (String, Int64?) -> Void = { _, _ in }
    /// Puts another chat or task on screen.
    @ObservationIgnored var show: (String) -> Void = { _ in }

    func open(_ store: ChatStore, jump: @escaping (String, Int64?) -> Void) {
        self.store = store
        scroll = jump
    }

    func jump(_ id: String) { scroll(id, nil) }

    func locate(_ attachment: Attachment) {
        for message in store?.messages ?? [] {
            if let index = message.parts.firstIndex(where: { $0.attachment?.id == attachment.id }) {
                scroll(message.id, Int64(index))
                return
            }
        }
    }

    var attachments: [Attachment] { store?.messages.flatMap { $0.parts.compactMap(\.attachment) } ?? [] }
    var runs: [Run] { store?.runs ?? [] }
}

/// A chat's side panel, beside the chat on macOS and its own page on iPhone: an agent's
/// profile for a direct chat, the editor for a group chat.
struct ProfilePanel: View {
    let agent: Agent
    let thread: PolymuxKit.Thread
    let session: AppSession
    let openChat: OpenChat
    let close: () -> Void
    let jump: (String) -> Void

    var body: some View {
        Group {
            if thread.isGroup {
                GroupEditor(thread: thread, session: session, jump: jump).id(thread.id)
            } else {
                ProfileView(agent: agent, session: session, threadID: thread.id, attachments: openChat.attachments,
                            runs: runs, inspector: true, show: openChat.show, locate: { attachment in
                                #if os(iOS)
                                close()
                                #endif
                                openChat.locate(attachment)
                            }, jump: jump)
                    .id(agent.id)
            }
        }
        .environment(\.panelClose, PanelAction(action: close))
    }

    private var runs: [Run] {
        var known = Dictionary(uniqueKeysWithValues: thread.runs.map { ($0.id, $0) })
        if openChat.store?.threadID == thread.id {
            for run in openChat.runs where run.seq >= (known[run.id]?.seq ?? -1) { known[run.id] = run }
        }
        return Array(known.values)
    }
}

