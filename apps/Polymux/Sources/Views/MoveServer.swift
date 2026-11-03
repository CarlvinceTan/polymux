import SwiftUI
import PolymuxKit

/// Moves a server to a linked one, or merges it in, after saying what that will and won't do.
struct MoveServer: View {
    /// What to do: move the server on show away, or bring another one in.
    enum Kind {
        case move
        case merge
    }

    let kind: Kind
    /// The server whose data goes, and its client and store.
    let sourceName: String
    let source: APIClient?
    let network: NetworkStore?
    let target: NetworkInfo.Peer
    /// True once the source has gone from this device.
    let finished: Bool
    let onBack: () -> Void
    @Environment(\.panelClose) private var close
    @State private var preview: NetworkInfo.MovePreview?
    @State private var issue: String?
    @State private var started = false

    private var merging: Bool { kind == .merge }
    private var moving: Bool { started || network?.value?.moving != nil }
    private var action: String { merging ? "Merge" : "Move" }

    var body: some View {
        PanelView("\(action) server", onClose: { close() }, onBack: onBack) {
            Form {
                Section {
                    LabeledContent(merging ? "From" : "To", value: merging ? sourceName : target.name)
                    if merging { LabeledContent("Into", value: target.name) }
                    if let preview {
                        LabeledContent("Size", value: ByteCountFormatter.string(fromByteCount: preview.size, countStyle: .file))
                    } else if issue == nil {
                        ProgressView().frame(maxWidth: .infinity)
                    }
                } footer: {
                    if let text = issue ?? network?.value?.moveIssue.flatMap({ $0.isEmpty ? nil : $0 }) {
                        Text(text).font(.footnote).foregroundStyle(.muted)
                    } else if moving {
                        Text(merging ? "Merging…" : "Moving…").font(.footnote).foregroundStyle(.muted)
                    }
                }
                .panelRow()
                list("Can’t \(action.lowercased())", preview?.blockers)
                list(merging ? "Also" : nil, preview?.notes)
                list(merging ? "Stays behind" : "Stays on this Mac", preview?.stays)
                list("Gains access to these chats", preview?.gains)
                list("Loses access", preview?.loses)
            }
            .formStyle(.grouped)
        } actions: {
            Button(action, action: start)
                .disabled(moving || preview == nil || preview?.blockers.isEmpty == false)
                .accessibilityLabel("\(action) server")
        }
        .panelDismiss(onClose: { close() })
        // The server stops saying it's moving once it has landed, or given up and said why.
        .onChange(of: network?.value?.moving != nil) { _, moving in
            guard !moving, started else { return }
            if network?.value?.moveIssue?.isEmpty == false { started = false } else if !merging { onBack() }
        }
        .onChange(of: finished) { _, finished in if finished { onBack() } }
        .task {
            do { preview = try await source?.movePreview(target.id, merge: merging) }
            catch { issue = error.reason(or: "Couldn’t reach \(merging ? sourceName : target.name).") }
        }
    }

    @ViewBuilder private func list(_ title: String?, _ lines: [String]?) -> some View {
        if let lines, !lines.isEmpty {
            Section {
                ForEach(lines, id: \.self) { Text($0) }
            } header: {
                if let title { Text(title) }
            }
            .panelRow()
        }
    }

    private func start() {
        started = true
        issue = nil
        Task {
            do { try await source?.startMove(target.id, merge: merging) }
            catch {
                started = false
                issue = error.reason(or: "Couldn’t \(action.lowercased()) this server.")
            }
        }
    }
}
