import SwiftUI
import PolymuxKit

struct ChoiceCard: View {
    let part: Part
    let client: APIClient
    let message: Message
    let bubble: Color
    @State private var answering = false
    @State private var failed = false
    @State private var selected: String?
    @State private var text = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(part.question ?? "").fixedSize(horizontal: false, vertical: true)
            if let answer = selected ?? part.answerID {
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
                if part.allowText == true {
                    HStack {
                        TextField("Another answer", text: $text)
                            .textFieldStyle(.roundedBorder)
                            .onSubmit { Task { await choose("_text") } }
                        Button("Send answer") { Task { await choose("_text") } }
                            .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || answering)
                    }
                    .disabled(answering)
                }
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
        if answer == "_text" { return part.answerText ?? text }
        return (part.options ?? []).first { $0.id == answer }?.label ?? answer
    }

    private func choose(_ option: String) async {
        guard !answering else { return }
        let answer = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard option != "_text" || !answer.isEmpty else { return }
        answering = true
        selected = option
        defer { answering = false }
        do {
            if message.id.hasPrefix("focus:") {
                _ = try await client.answerFocus(agentID: message.threadID, optionID: option)
            } else {
                _ = try await client.answerChoice(threadID: message.threadID, messageID: message.id,
                                                  optionID: option, text: option == "_text" ? answer : nil)
            }
            failed = false
        } catch {
            selected = nil
            failed = true
        }
    }
}
