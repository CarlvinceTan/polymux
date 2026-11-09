import SwiftUI
import PolymuxKit
#if os(macOS)
import PolymuxUpdates
#endif

/// The Updates settings pane: what runs here, and on macOS Sparkle's update checks.
struct UpdatesSettings: View {
    let network: NetworkInfo?
    let settings: SettingsStore?

    var body: some View {
        #if os(macOS)
        Section {
            Toggle("Check automatically", isOn: Binding(get: { Updates.shared.automaticallyChecks },
                set: { Updates.shared.automaticallyChecks = $0 }))
        } header: {
            SectionHeader("Updates", symbol: "arrow.down.circle.fill")
        }
        .panelRow()
        #endif
        Section {
            #if os(macOS)
            LabeledContent {
                HStack(spacing: 8) {
                    Text(Self.version)
                    if Updates.shared.available {
                        Button("Update") { Updates.shared.checkForUpdates() }
                            .buttonStyle(.bordered)
                            .buttonBorderShape(.roundedRectangle)
                            .controlSize(.small)
                            .accessibilityLabel("Update Polymux")
                    }
                }
            } label: {
                Text(clientLabel)
            }
            #else
            LabeledContent(clientLabel, value: Self.version)
            #endif
            LabeledContent(serverLabel, value: network?.server.build ?? "Not connected")
            SettingsPicker("Server channel", selection: Binding(get: { settings?.value?.updateChannel ?? SettingsUpdateChannel.nightly },
                                                               set: { settings?.edit(SettingsPatch(updateChannel: $0)) }),
                           options: [("Nightly", SettingsUpdateChannel.nightly), ("Stable", SettingsUpdateChannel.stable)])
                .disabled(settings?.value == nil)
            SettingsPicker("Server updates", selection: Binding(get: { settings?.value?.updateMode ?? SettingsUpdateMode.automatic },
                                                               set: { settings?.edit(SettingsPatch(updateMode: $0)) }),
                           options: [("Automatic", SettingsUpdateMode.automatic), ("Manual", SettingsUpdateMode.manual)])
                .disabled(settings?.value == nil)
            #if os(macOS)
            if let status = Updates.shared.status {
                Text(status).font(.footnote).foregroundStyle(.muted)
            }
            #endif
        } header: {
            #if os(macOS)
            SectionHeader("Version", symbol: "info.circle.fill") {
                Button("Check for updates", systemImage: "arrow.clockwise") { Updates.shared.check() }
                    .accessibilityLabel("Check for updates")
                    .help("Check for updates")
                    .symbolEffect(.rotate, isActive: Updates.shared.checking)
                    .disabled(Updates.shared.checking)
                    .labelStyle(.iconOnly)
                    .buttonStyle(.borderless)
            }
            #else
            Text("Version").foregroundStyle(.primary)
            #endif
        }
        .panelRow()
    }

    private var clientLabel: String {
        guard let network, let current = network.current,
              let name = network.devices.first(where: { $0.id == current })?.title else { return "Client" }
        return "Client (\(name))"
    }

    private var serverLabel: String {
        guard let network else { return "Server" }
        let name = network.devices.first { $0.id == network.host }?.title ?? network.server.name
        return name.isEmpty ? "Server" : "Server (\(name))"
    }

    private static var version: String {
        let info = Bundle.main.infoDictionary ?? [:]
        let short = (info["CFBundleShortVersionString"] as? String) ?? ""
        let build = (info["CFBundleVersion"] as? String) ?? ""
        let channel = channel(info).map { " \($0)" } ?? ""
        if !short.isEmpty && !build.isEmpty { return "\(short) (\(build))\(channel)" }
        if !short.isEmpty { return short + channel }
        if !build.isEmpty { return build + channel }
        return "Development"
    }

    /// Nightly or Stable, from the build's update feed (Mac) or TestFlight receipt (iPhone).
    private static func channel(_ info: [String: Any]) -> String? {
        #if os(macOS)
        guard let feed = info["SUFeedURL"] as? String else { return nil }
        return feed.contains("dev-updates") ? "Nightly" : "Stable"
        #else
        switch Bundle.main.appStoreReceiptURL?.lastPathComponent {
        case "sandboxReceipt": return "Nightly"
        case "receipt": return "Stable"
        default: return nil
        }
        #endif
    }
}
