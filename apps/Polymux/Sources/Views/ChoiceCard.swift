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
    @Environment(\.colorScheme) private var scheme

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
                            .frame(width: 24, height: 24)
                    }
                    .buttonStyle(FlatButton(radius: 6, height: 24, inset: 0, quiet: true))
                    .disabled(answering)
                    .tapArea(10)
                    .help("Dismiss without answering")
                    .accessibilityLabel("Dismiss question")
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
                                .foregroundStyle(.secondary)
                                .frame(minWidth: 22, minHeight: 22)
                                .background(.primary.opacity(0.08), in: RoundedRectangle(cornerRadius: 6))
                            Text(option.label).multilineTextAlignment(.leading)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .buttonStyle(FlatButton(inset: 12, quiet: true))
                    .disabled(answering)
                    .accessibilityLabel("\(letter(index)). \(option.label)")
                }
                if part.allowText == true {
                    HStack {
                        TextField("Another answer", text: $text)
                            .textFieldStyle(.plain)
                            .padding(.horizontal, 10)
                            .frame(minHeight: 32)
                            .background(scheme == .dark ? Color.primary.opacity(0.06) : Color.white.opacity(0.72),
                                        in: RoundedRectangle(cornerRadius: 8))
                            .onSubmit { Task { await choose("_text") } }
                        Button("Send") { Task { await choose("_text") } }
                            .buttonStyle(FlatButton(prominent: true))
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

/// The card kit's button: flat fill, 8pt corners, darker on hover; quiet ones fill only on hover.
private struct FlatButton: ButtonStyle {
    var radius: CGFloat = 8
    var height: CGFloat = 32
    var inset: CGFloat = 14
    var prominent = false
    var quiet = false

    func makeBody(configuration: Configuration) -> some View {
        Surface(configuration: configuration, radius: radius, height: height, inset: inset, prominent: prominent, quiet: quiet)
    }

    private struct Surface: View {
        let configuration: Configuration
        let radius: CGFloat
        let height: CGFloat
        let inset: CGFloat
        let prominent: Bool
        let quiet: Bool
        @Environment(\.isEnabled) private var enabled
        @State private var hovered = false

        var body: some View {
            let fill: Color = prominent && enabled
                ? .accentColor.opacity(hovered ? 0.86 : 1)
                : quiet ? .primary.opacity(hovered && enabled ? PanelStyle.selectedTint : 0)
                : .primary.opacity(hovered && enabled ? 0.16 : PanelStyle.selectedTint)
            configuration.label
                .fontWeight(.medium)
                .foregroundStyle(prominent && enabled ? Color.white : enabled ? Color.primary : Color.muted)
                .padding(.horizontal, inset)
                .frame(minHeight: height)
                .background(fill, in: RoundedRectangle(cornerRadius: radius))
                .contentShape(RoundedRectangle(cornerRadius: radius))
                .scaleEffect(configuration.isPressed ? 0.97 : 1)
                .pointerHover { hovered = $0 }
        }
    }
}
