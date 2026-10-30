import SwiftUI

struct SettingsField: View {
    let title: String
    @Binding var text: String
    let prompt: String
    var secure = false

    var body: some View {
        LabeledContent(title) {
            Group {
                if secure {
                    SecureField("", text: $text)
                } else {
                    TextField("", text: $text)
                }
            }
            .labelsHidden()
            .textFieldStyle(.plain)
            .foregroundStyle(.primary)
            .multilineTextAlignment(.trailing)
            .autocorrectionDisabled()
            .accessibilityLabel(title)
            .accessibilityHint(prompt)
            .overlay(alignment: .trailing) {
                if text.isEmpty {
                    Text(prompt).foregroundStyle(.muted).lineLimit(1)
                        .allowsHitTesting(false).accessibilityHidden(true)
                }
            }
        }
    }
}

struct SettingsPicker: View {
    let title: String
    @Binding var selection: String
    let options: [(String, String)]
    @State private var choosing = false

    init(_ title: String, selection: Binding<String>, options: [(String, String)]) {
        self.title = title
        _selection = selection
        self.options = options
    }

    private var value: String { options.first { $0.1 == selection }?.0 ?? selection }

    var body: some View {
        Button { choosing = true } label: {
            HStack {
                Text(title).lineLimit(1).layoutPriority(1)
                Spacer()
                Text(value).lineLimit(1)
                Image(systemName: "chevron.up.chevron.down")
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityValue(value)
        .dropdown(isPresented: $choosing) {
            DropdownOptions(selection: $selection, options: options)
        }
    }
}
