import SwiftUI
import PolymuxKit
#if os(macOS)
import PolymuxUpdates
#endif

/// The Updates settings pane: Client and Server versions, channels and update modes.
struct UpdatesSettings: View {
    let network: NetworkInfo?
    let settings: SettingsStore?

    var body: some View {
        Section {
            #if os(macOS)
            LabeledContent("Version") {
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
            }
            Toggle("Check automatically", isOn: Binding(get: { Updates.shared.automaticallyChecks },
                set: { Updates.shared.automaticallyChecks = $0 }))
            SettingsPicker("Client channel", selection: Binding(get: { Updates.shared.channel.rawValue },
                set: { if let channel = AppUpdateChannel(rawValue: $0) { Updates.shared.channel = channel } }),
                options: [("Stable", AppUpdateChannel.stable.rawValue), ("Dev", AppUpdateChannel.nightly.rawValue)])
                .disabled(Updates.shared.checking)
            #else
            LabeledContent("Version", value: Self.version)
            #endif
        } header: {
            #if os(macOS)
            SectionHeader("Client", symbol: "laptopcomputer") {
                if let status = Updates.shared.status {
                    Text(status).font(.footnote).foregroundStyle(.muted).lineLimit(1)
                        .transition(.opacity)
                }
                Button("Check for updates", systemImage: "arrow.clockwise") { Updates.shared.check() }
                    .accessibilityLabel("Check for updates")
                    .help("Check for updates")
                    .symbolEffect(.rotate, isActive: Updates.shared.checking)
                    .disabled(Updates.shared.checking)
                    .labelStyle(.iconOnly)
                    .buttonStyle(.borderless)
            }
            #else
            Text("Client").foregroundStyle(.primary)
            #endif
        }
        .panelRow()
        #if os(macOS)
        .animation(.default, value: Updates.shared.status)
        #endif
        Section {
            LabeledContent("Version", value: network?.server.build ?? "Not connected")
            SettingsPicker("Server channel", selection: Binding(get: { settings?.value?.updateChannel ?? SettingsUpdateChannel.nightly },
                                                        set: { settings?.edit(SettingsPatch(updateChannel: $0)) }),
                           options: [("Dev", SettingsUpdateChannel.nightly), ("Stable", SettingsUpdateChannel.stable)])
                .disabled(settings?.value == nil)
            SettingsPicker("Updates", selection: Binding(get: { settings?.value?.updateMode ?? SettingsUpdateMode.automatic },
                                                        set: { settings?.edit(SettingsPatch(updateMode: $0)) }),
                           options: [("Automatic", SettingsUpdateMode.automatic), ("Manual", SettingsUpdateMode.manual)])
                .disabled(settings?.value == nil)
        } header: {
            SectionHeader("Server", symbol: "server.rack")
        }
        .panelRow()
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

    /// Installed build source, from the Mac update feed or iPhone receipt.
    private static func channel(_ info: [String: Any]) -> String? {
        #if os(macOS)
        guard let feed = info["SUFeedURL"] as? String else { return nil }
        return feed.contains("dev-updates") ? "Dev" : "Stable"
        #else
        switch Bundle.main.appStoreReceiptURL?.lastPathComponent {
        case "sandboxReceipt": return "Dev"
        case "receipt": return "Stable"
        default: return nil
        }
        #endif
    }
}
