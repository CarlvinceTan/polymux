import SwiftUI
import PolymuxKit

struct ReplyPreview: View {
    let reply: ReplyContext
    let agentName: String
    let client: APIClient
    var jump: (() -> Void)? = nil

    private var author: String { reply.role == "user" ? "You" : agentName }
    private var text: String { reply.text.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var summary: String {
        if !text.isEmpty { return text }
        return reply.attachment?.name ?? "Message"
    }

    var body: some View {
        Group {
            if let jump {
                Button(action: jump) { quote }
                    .buttonStyle(.plain)
                    .accessibilityHint("Show original message")
            } else {
                quote
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Reply to \(author): \(summary)")
    }

    private var quote: some View {
        HStack(alignment: .top, spacing: 10) {
            VStack(alignment: .leading, spacing: 3) {
                Text(author).font(.caption.weight(.semibold)).foregroundStyle(.primary).lineLimit(1)
                Text(summary).font(.caption).foregroundStyle(.secondary)
                    .lineLimit(3).multilineTextAlignment(.leading)
            }
            if let attachment = reply.attachment {
                AttachmentPreview(attachment: attachment, client: client, showsName: false, square: 52)
            }
        }
        .padding(.leading, 11).padding(.vertical, 3)
        .overlay(alignment: .leading) {
            Capsule().fill(.primary.opacity(0.22)).frame(width: 3).padding(.vertical, 3)
        }
        .contentShape(Rectangle())
    }
}

struct ReplyBubble: View {
    @Environment(\.colorScheme) private var colorScheme
    let reply: ReplyContext
    let author: String
    let client: APIClient
    let jump: () -> Void
    var width: CGFloat = BubbleStyle.width

    var body: some View {
        Button(action: jump) {
            ReplySize(width: width, minimum: reply.attachment == nil ? 160 : 0) {
                HStack(alignment: .top, spacing: 8) {
                    Text("\(author):").lineLimit(1)
                    if let attachment = reply.attachment {
                        AttachmentPreview(attachment: attachment, client: client, showsName: false, square: 48)
                    } else {
                        Text(reply.text.split(whereSeparator: \.isWhitespace).joined(separator: " "))
                            .lineLimit(1).truncationMode(.tail)
                    }
                }
                .font(.body).foregroundStyle(.secondary)
                .padding(.horizontal, 12).padding(.vertical, 10)
            }
            .background(BubbleStyle.reply(colorScheme),
                        in: .rect(cornerRadius: 12))
            .contentShape(.rect(cornerRadius: 12))
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Reply to \(author): \(reply.attachment?.name ?? reply.text)")
        .accessibilityHint("Show original message")
    }
}

private struct ReplySize: Layout {
    let width: CGFloat
    let minimum: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard let child = subviews.first else { return .zero }
        let ideal = child.sizeThatFits(.unspecified)
        let compact = child.sizeThatFits(ProposedViewSize(width: 0, height: nil))
        let floor = max(minimum, compact.width)
        let cap = min(max(floor, width), max(floor, proposal.width ?? width))
        let target = min(max(floor, ideal.width), cap)
        let size = child.sizeThatFits(ProposedViewSize(width: target, height: nil))
        return CGSize(width: target, height: size.height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        subviews.first?.place(at: bounds.origin, anchor: .topLeading, proposal: ProposedViewSize(bounds.size))
    }
}
