import SwiftUI
import PolymuxKit

struct ModelLabel: View {
    let name: ModelName

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 0) {
            Text(name.model.isEmpty ? "Not set" : name.model)
                .layoutPriority(1)
            if !name.model.isEmpty && !name.provider.isEmpty {
                Text(" " + name.provider).foregroundStyle(.muted)
            }
        }
        .lineLimit(1)
        .truncationMode(.tail)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(name.title)
    }
}
