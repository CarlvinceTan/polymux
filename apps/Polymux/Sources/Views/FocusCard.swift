import SwiftUI
import PolymuxKit

/// First-run question card. The server seeds it and owns the answer, so the card only
/// renders what came back on the wire and never invents its own options.
struct FocusCard: View {
    let part: Part
    let client: APIClient
    let agentID: String
    let bubble: Color
    @State private var answering = false
    @State private var failed = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(part.question ?? "").fixedSize(horizontal: false, vertical: true)
            if let answer = part.answerID {
                HStack(spacing: 8) {
                    Image(systemName: answer == FocusAnswer.dismissed ? "arrow.turn.down.right" : "checkmark")
                        .font(.caption.weight(.semibold))
                    Text(label(answer))
                }
                .foregroundStyle(.secondary)
            } else {
                ForEach(Array((part.options ?? []).enumerated()), id: \.element.id) { index, option in
                    Button { Task { await choose(option.id) } } label: {
                        HStack(spacing: 10) {
                            Text(letter(index)).font(.caption.weight(.semibold))
                                .foregroundStyle(.secondary).frame(width: 14, alignment: .leading)
                            Text(option.label).multilineTextAlignment(.leading)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .disabled(answering)
                    .accessibilityLabel("\(letter(index)). \(option.label)")
                }
                Button("Skip") { Task { await choose(FocusAnswer.dismissed) } }
                    .font(.caption).buttonStyle(.plain).foregroundStyle(.secondary)
                    .disabled(answering)
            }
            if failed {
                Text("Couldn’t record that choice.").font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(12)
        .background(bubble, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
    }

    private func letter(_ index: Int) -> String {
        String(UnicodeScalar(UInt8(65 + min(index, 25))))
    }

    private func label(_ answer: String) -> String {
        if answer == FocusAnswer.dismissed { return "Skipped" }
        return (part.options ?? []).first { $0.id == answer }?.label ?? answer
    }

    private func choose(_ option: String) async {
        guard !answering else { return }
        answering = true
        defer { answering = false }
        do {
            _ = try await client.answerFocus(agentID: agentID, optionID: option)
            failed = false
        } catch {
            failed = true
        }
    }
}
