import SwiftUI
import PolymuxKit

/// Manages a device. An empty name falls back to the one it reports; `actions` holds Remove and Allow.
struct DeviceView<Actions: View>: View {
    let device: NetworkInfo.Device
    let renames: Bool
    let status: StatusDot.State
    let server: Bool
    let onBack: () -> Void
    let save: (String) -> Void
    @ViewBuilder let actions: () -> Actions
    @Environment(\.panelClose) private var close
    @State private var name: String

    init(device: NetworkInfo.Device, renames: Bool, status: StatusDot.State, server: Bool, onBack: @escaping () -> Void,
         save: @escaping (String) -> Void, @ViewBuilder actions: @escaping () -> Actions) {
        self.device = device
        self.renames = renames
        self.status = status
        self.server = server
        self.onBack = onBack
        self.save = save
        self.actions = actions
        _name = State(initialValue: device.label ?? "")
    }

    var body: some View {
        PanelView(device.title, onClose: { close() }, onBack: onBack) {
            Form {
                Section {
                    if renames {
                        SettingsField(title: "Name", text: $name, prompt: device.name)
                            .accessibilityIdentifier("device.name")
                            .autocorrectionDisabled()
                            .onSubmit(done)
                    }
                    LabeledContent("Hostname", value: device.name)
                    LabeledContent("Status") {
                        HStack(spacing: 8) {
                            StatusDot(state: status).accessibilityHidden(true)
                            Text(status.title)
                        }
                    }
                    LabeledContent("Type", value: server ? "Server" : "Client")
                }
                .panelRow()
                Section { actions() }
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
