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
    @State private var dismissHovered = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline) {
                Text(part.question ?? "").fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 12)
                // The corner X dismisses, like Grok Bot's choice card.
                if selected == nil && part.answerID == nil {
                    Button {
                        Task { await choose(ChoiceAnswer.dismissed) }
                    } label: {
                        Image(systemName: "xmark")
                            .font(.caption)
                            // Optical: centre the glyph on the question's first line.
                            .offset(y: -1)
                            .foregroundStyle(dismissHovered ? Color.primary : Color.muted)
                            .frame(width: 24, height: 24)
                            .background(dismissHovered ? Color.primary.opacity(0.14) : .clear,
                                        in: RoundedRectangle(cornerRadius: 6))
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .disabled(answering)
                    .help("Dismiss without answering")
                    .accessibilityLabel("Dismiss question")
                    .onHover { dismissHovered = $0 }
                }
            }
            if let answer = selected ?? part.answerID {
                HStack(spacing: 8) {
                    Image(systemName: answer == ChoiceAnswer.dismissed ? "arrow.turn.down.right" : "checkmark")
                        .font(.caption.weight(.semibold))
                    Text(label(answer))
                }
                .foregroundStyle(.muted)
            } else {
                ForEach(Array((part.options ?? []).enumerated()), id: \.element.id) { index, option in
                    Button { Task { await choose(option.id) } } label: {
                        HStack(spacing: 10) {
                            Text(letter(index)).font(.caption.weight(.semibold))
                                .foregroundStyle(.muted).frame(minWidth: 14, alignment: .leading)
                            Text(option.label).multilineTextAlignment(.leading)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .disabled(answering)
                    .accessibilityLabel("\(letter(index)). \(option.label)")
                }
                if part.allowText == true {
                    HStack {
                        TextField("Another answer", text: $text)
                            .textFieldStyle(.roundedBorder)
                            .onSubmit { Task { await choose("_text") } }
                        Button("Send") { Task { await choose("_text") } }
                            .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || answering)
                    }
                    .disabled(answering)
                }
            }
            if failed {
                Text("Couldn’t record that choice.").font(.caption).foregroundStyle(.muted)
            }
        }
        .padding(12)
        .background(bubble, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
    }

    /// A..Z, then AA.. — option counts above 26 still get distinct letters.
    private func letter(_ index: Int) -> String {
        var number = index + 1
        var letters = ""
        while number > 0 {
            number -= 1
            letters = String(UnicodeScalar(UInt8(65 + number % 26))) + letters
            number /= 26
        }
        return letters
    }

    private func label(_ answer: String) -> String {
        if answer == ChoiceAnswer.dismissed { return "Skipped" }
        if answer == "_text" { return part.answerText ?? (text.isEmpty ? "Custom answer" : text) }
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
            _ = try await client.answerChoice(threadID: message.threadID, messageID: message.id,
                                              optionID: option, text: option == "_text" ? answer : nil)
            failed = false
        } catch {
            selected = nil
            failed = true
        }
    }
}
