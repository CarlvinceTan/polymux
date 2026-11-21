import SwiftUI
import PolymuxKit

/// A gate plugin's question, answered in the chat. The rule that asked is the plugin's;
/// this card only shows the question and sends back the option the person picked.
struct ApprovalCard: View {
    let part: Part
    let client: APIClient
    let message: Message
    let bubble: Color
    @State private var answering = false
    @State private var selected: String?
    @State private var failed = false
    @Environment(\.colorScheme) private var scheme

    private var approval: Approval { part.approval ?? Approval() }
    private var options: [ApprovalOption] { approval.options ?? [] }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(approval.title)
                    .font(.headline)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 12)
                if selected == nil && approval.status == ApprovalStatus.pending {
                    Circle().fill(Color.orange).frame(width: 7, height: 7)
                        .padding(.trailing, 12)
                        .accessibilityLabel("Waiting for an answer")
                }
            }
            if let detail = approval.detail, !detail.isEmpty {
                Text(detail)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(8)
                    .background(codeFill, in: RoundedRectangle(cornerRadius: 8))
                    .textSelection(.enabled)
            }
            if let answer = selected ?? approval.answerID {
                HStack(spacing: 8) {
                    Image(systemName: outcome(answer) == ApprovalOptionOutcome.deny ? "xmark" : "checkmark")
                        .font(.caption.weight(.semibold))
                    Text(label(answer))
                }
                .foregroundStyle(.muted)
            } else if approval.status != ApprovalStatus.pending {
                Text(approval.status == ApprovalStatus.expired ? "Expired" : "Closed")
                    .font(.caption)
                    .foregroundStyle(.muted)
            } else {
                ForEach(options, id: \.id) { option in
                    Button { Task { await choose(option.id) } } label: {
                        HStack(spacing: 10) {
                            Image(systemName: option.outcome == ApprovalOptionOutcome.deny ? "xmark" : "checkmark")
                                .font(.caption.weight(.semibold))
                                .foregroundStyle(option.outcome == ApprovalOptionOutcome.deny ? .secondary : .primary)
                                .frame(minWidth: 22, minHeight: 22)
                                .background(.primary.opacity(0.08), in: RoundedRectangle(cornerRadius: 6))
                            Text(option.label).multilineTextAlignment(.leading)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .buttonStyle(FlatButton(inset: 0, quiet: true))
                    .disabled(answering)
                    .accessibilityLabel(option.label)
                }
            }
            if failed {
                Text("Couldn’t record that answer.").font(.caption).foregroundStyle(.muted)
            }
        }
        .padding(.top, 12)
        .padding(.bottom, 12)
        .padding(.leading, 18)
        .padding(.trailing, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(bubble, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
    }

    private var codeFill: Color {
        scheme == .dark ? Color.primary.opacity(0.06) : Color.white.opacity(0.72)
    }

    private func option(_ id: String) -> ApprovalOption? {
        options.first { $0.id == id }
    }

    private func label(_ id: String) -> String {
        option(id)?.label ?? id
    }

    private func outcome(_ id: String) -> String {
        option(id)?.outcome ?? ApprovalOptionOutcome.deny
    }

    private func choose(_ id: String) async {
        guard !answering else { return }
        answering = true
        selected = id
        defer { answering = false }
        do {
            _ = try await client.answerApproval(threadID: message.threadID, messageID: message.id, optionID: id)
            failed = false
        } catch {
            selected = nil
            failed = true
        }
    }
}
