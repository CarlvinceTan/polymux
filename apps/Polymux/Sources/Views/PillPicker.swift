import SwiftUI

struct PillPicker<Value: Hashable>: View {
    let title: String
    @Binding var selection: Value
    let options: [(title: String, value: Value)]

    init(_ title: String, selection: Binding<Value>, options: [(title: String, value: Value)]) {
        self.title = title
        _selection = selection
        self.options = options
    }

    var body: some View {
        Picker(title, selection: $selection) {
            ForEach(options, id: \.value) { option in
                Text(option.title).tag(option.value)
            }
        }
        .pickerStyle(.segmented)
        .labelsHidden()
    }
}
