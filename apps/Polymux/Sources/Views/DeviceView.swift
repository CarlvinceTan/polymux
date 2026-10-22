import SwiftUI
import PolymuxKit

/// Names a paired device; an empty name falls back to the one it reports.
struct DeviceView: View {
    let device: NetworkInfo.Device
    let onBack: () -> Void
    let save: (String) -> Void
    @Environment(\.panelClose) private var close
    @State private var name: String

    init(device: NetworkInfo.Device, onBack: @escaping () -> Void, save: @escaping (String) -> Void) {
        self.device = device
        self.onBack = onBack
        self.save = save
        _name = State(initialValue: device.label ?? "")
    }

    var body: some View {
        PanelView("Rename device", onClose: { close() }, onBack: onBack) {
            Form {
                Section {
                    TextField("Name", text: $name, prompt: Text(device.name))
                        .accessibilityIdentifier("device.name")
                        .autocorrectionDisabled()
                        .onSubmit(done)
                    LabeledContent("Hostname", value: device.name)
                }
                .panelRow()
            }
            .formStyle(.grouped)
        } actions: {
            Button("Done", action: done)
        }
        .panelDismiss(onClose: { close() })
    }

    private func done() {
        save(name)
        onBack()
    }
}
