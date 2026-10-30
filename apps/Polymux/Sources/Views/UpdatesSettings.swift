import SwiftUI
import PolymuxKit
#if os(macOS)
import PolymuxUpdates
#endif

/// The Updates settings pane: what runs here, and on macOS Sparkle's update checks.
struct UpdatesSettings: View {
    let server: NetworkInfo.Server?

    var body: some View {
        #if os(macOS)
        Section {
            Toggle("Check automatically", isOn: Binding(get: { Updates.shared.automaticallyChecks },
                set: { Updates.shared.automaticallyChecks = $0 }))
            HStack {
                Button("Check") { Updates.shared.check() }
                    .accessibilityLabel("Check for updates")
                    .disabled(Updates.shared.checking)
                if Updates.shared.checking { ProgressView().controlSize(.small) }
                if Updates.shared.available {
                    Button("View") { Updates.shared.checkForUpdates() }
                        .accessibilityLabel("View update")
                }
            }
            if let status = Updates.shared.status {
                Text(status).font(.footnote).foregroundStyle(.muted)
            }
        } header: {
            Text("Updates").foregroundStyle(.primary)
        }
        .panelRow()
        #endif
        Section {
            LabeledContent("This device", value: Self.version)
            LabeledContent("Server", value: server?.build ?? "Not connected")
        } header: {
            Text("Version").foregroundStyle(.primary)
        }
        .panelRow()
    }

    private static var version: String {
        let info = Bundle.main.infoDictionary ?? [:]
        let short = (info["CFBundleShortVersionString"] as? String) ?? ""
        let build = (info["CFBundleVersion"] as? String) ?? ""
        if !short.isEmpty && !build.isEmpty { return "\(short) (\(build))" }
        if !short.isEmpty { return short }
        if !build.isEmpty { return build }
        return "Development"
    }
}
