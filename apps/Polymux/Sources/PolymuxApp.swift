import CoreImage
import CryptoKit
import SwiftUI
import PolymuxKit
#if os(macOS)
import AppKit
import ApplicationServices
#else
import UIKit
#endif

@main
enum PolymuxMain {
    @MainActor static func main() {
        #if os(macOS)
        if let index = CommandLine.arguments.firstIndex(of: "--verify-accessibility") {
            guard CommandLine.arguments.indices.contains(index + 1),
                  let nonce = UUID(uuidString: CommandLine.arguments[index + 1]) else { return }
            let result = FileManager.default.temporaryDirectory
                .appending(path: "polymux-ui-accessibility-\(nonce.uuidString).json")
            let value = ["accessibility": AXIsProcessTrusted()]
            if let data = try? JSONEncoder().encode(value) { try? data.write(to: result, options: .atomic) }
            return
        }
        #endif
        PolymuxApp.main()
    }
}

struct PolymuxApp: App {
    #if os(macOS)
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    #else
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    #endif

    init() {
        _ = Notifications.shared
        FrameLog.install()
    }
    var body: some Scene {
        WindowGroup {
            AppRoot()
                .tint(Color("AccentColor"))
                #if os(macOS)
                .frame(minWidth: 780, minHeight: 720)
                #else
                .frame(minWidth: 360, minHeight: 500)
                #endif
        }
        #if os(macOS)
        .defaultSize(width: 1060, height: 760)
        .commands {
            CommandGroup(replacing: .sidebar) {}
            SettingsCommands()
        }
        #endif
    }
}

@MainActor
final class AppSession {
    let address: String
    let client: APIClient
    let cache: LocalCache
    let sidebar: SidebarStore
    let layout: SidebarLayout
    let settings: SettingsStore
    let avatars: AvatarStore
    /// Carries the connection when the server is only reachable from another network.
    let tunnel: Tunnel?

    init(address: String) async throws {
        self.address = address
        guard let url = ServerAddress.resolve(address, defaults: .standard) else {
            throw URLError(.badURL)
        }
        #if DEBUG
        if VerifyFixture.local {
            client = APIClient(baseURL: url, session: VerifyFixture.session(), token: "")
            tunnel = nil
        } else {
            (client, tunnel) = try await Self.connect(address, url: url)
        }
        #else
        (client, tunnel) = try await Self.connect(address, url: url)
        #endif
        cache = try await Task.detached {
            let file = ClientPaths.cache(address)
            let directory = file.deletingLastPathComponent()
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                                                    attributes: [.posixPermissions: 0o700])
            return try LocalCache(url: file)
        }.value
        sidebar = SidebarStore(client: client, cache: cache)
        await sidebar.load()
        layout = SidebarLayout(client: client, cache: sidebar.cacheActor)
        await layout.load()
        settings = SettingsStore(client: client)
        avatars = AvatarStore(sidebar: sidebar)
    }

    private static func connect(_ address: String, url: URL) async throws -> (APIClient, Tunnel?) {
        let (ticket, token) = await Task.detached {
            (try? String(contentsOf: ClientPaths.ticket(address), encoding: .utf8),
             try? String(contentsOf: ClientPaths.token(address), encoding: .utf8))
        }.value
        let (base, tunnel) = try await Tunnel.route(url, ticket: ticket)
        guard tunnel != nil else { return (APIClient(baseURL: url, token: token), nil) }
        // The tunnel's local address is not the one the credential was saved under.
        return (APIClient(baseURL: base, token: token), tunnel)
    }
}

struct AppRoot: View {
    @AppStorage("serverAddress") private var address = "http://127.0.0.1:7337"
    @State private var servers = ServerStore()
    @State private var session: AppSession?
    @State private var error: String?
    @State private var showSettings = false

    var body: some View {
        Group {
            #if DEBUG
            if CommandLine.arguments.contains("--verify-approval") {
                ApprovalFixture()
            } else {
                content
            }
            #else
            content
            #endif
        }
        #if os(macOS)
        .allowsWindowActivationEvents()
        .focusedSceneValue(\.settingsPanel, $showSettings)
        #endif
        .neutralBackground()
        .task(id: address) {
            #if DEBUG
            guard !CommandLine.arguments.contains("--verify-approval") else { return }
            #endif
            servers.remember(address)
            let target = address
            while !Task.isCancelled {
                do {
                    let opened = try await AppSession(address: target)
                    try Task.checkCancellation()
                    session = opened
                    error = nil
                    Task { await announce(opened) }
                    #if DEBUG
                    if !VerifyFixture.local { Notifications.shared.connect(opened.client) }
                    #else
                    Notifications.shared.connect(opened.client)
                    #endif
                    while !Task.isCancelled {
                        await opened.settings.refresh()
                        do { try await Task.sleep(for: .seconds(3)) }
                        catch { break }
                    }
                } catch {
                    guard !Task.isCancelled else { return }
                    session = nil
                    self.error = error.localizedDescription
                }
                if Task.isCancelled { break }
                try? await Task.sleep(for: .seconds(2))
            }
        }
        .onOpenURL { handleConnect($0) }
        .onAppear {
            URLRouter.shared.handler = { handleConnect($0) }
        }
        .panel(isPresented: $showSettings) {
            ConnectionView(address: $address, servers: servers, settings: activeSession?.settings,
                           client: activeSession?.client,
                           onClose: { showSettings = false })
                .id(address)
        }
        .panelHost()
        #if os(macOS)
        .toolbar(removing: .title)
        #endif
        .environment(session?.avatars)
        .appAppearance(appearance)
        #if os(macOS)
        .background { InputFocus().frame(width: 0, height: 0).accessibilityHidden(true) }
        #endif
    }

    private func handleConnect(_ url: URL) {
        if url.scheme == "polymux", url.host == "settings" {
            showSettings = true
            return
        }
        guard let invite = Invite(link: url), let parsed = ServerAddress.parse(invite.url) else { return }
        let target = parsed.absoluteString
        let file = ClientPaths.token(target)
        try? FileManager.default.createDirectory(at: file.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true,
                                                 attributes: [.posixPermissions: 0o700])
        for (value, path) in [(invite.token, file), (invite.ticket, ClientPaths.ticket(target))] {
            guard let value else {
                try? FileManager.default.removeItem(at: path)
                continue
            }
            try? value.write(to: path, atomically: true, encoding: .utf8)
            try? FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: path.path)
        }
        servers.remember(target)
        address = target
    }

    /// Names this device on the server so the Devices list can tell it apart.
    private func announce(_ session: AppSession) async {
        #if os(macOS)
        let name = await Task.detached { Host.current().localizedName ?? "Mac" }.value
        #else
        let name = UIDevice.current.name
        #endif
        try? await session.client.renameDevice(name)
        if let network = try? await session.client.network() {
            servers.remember(session.address, name: network.server.name)
        }
    }

    private var activeSession: AppSession? { session?.address == address ? session : nil }

    private var appearance: String {
        #if DEBUG
        if VerifyFixture.enabled { return UserDefaults.standard.string(forKey: "AppleInterfaceStyle")?.lowercased() ?? "system" }
        if CommandLine.arguments.contains("--verify-approval") {
            return CommandLine.arguments.contains("--approval-dark") ? "dark" : "light"
        }
        #endif
        return session?.settings.value?.appearance ?? "system"
    }

    private var content: some View {
        Group {
            if let session = activeSession {
                ShellView(session: session, showSettings: $showSettings)
                    .id(address)
            } else if let error {
                ContentUnavailableView {
                    Label("Connect to Polymux", systemImage: "network")
                } description: {
                    Text(error)
                } actions: {
                    Button("Connection settings") { showSettings = true }
                }
            } else {
                Color.clear
            }
        }
    }
}

struct ConnectionView: View {
    @Binding var address: String
    let servers: ServerStore
    let settings: SettingsStore?
    let client: APIClient?
    let onClose: () -> Void
    @State private var network: NetworkInfo? = nil
    @State private var networkIssue: String?
    @State private var deviceIssue: String?
    @State private var renaming: NetworkInfo.Device?
    @State private var refreshing = false
    @State private var adding = false

    var body: some View {
        // One container, so returning from a sub-view doesn't refetch over optimistic edits.
        ZStack {
            if adding {
                AddDevice(client: client, onBack: { adding = false }) { link in
                    URLRouter.shared.handler?(link)
                    onClose()
                }
            } else if let renaming {
                DeviceView(device: renaming, onBack: { self.renaming = nil }) { rename(renaming, to: $0) }
            } else {
                settingsPanel
            }
        }
        .task(id: client?.baseURL) { await loadNetwork() }
    }

    private var settingsPanel: some View {
        PanelView("Settings", onClose: onClose) {
            Form {
                Section("Appearance") {
                    Picker("Theme", selection: Binding(get: { settings?.value?.appearance ?? "system" },
                                                      set: { settings?.edit(SettingsPatch(appearance: $0)) })) {
                        Text("Light").tag("light")
                        Text("Dark").tag("dark")
                        Text("System").tag("system")
                    }
                    .disabled(settings?.value == nil || settings?.isConnected != true)
                }
                .panelRow()
                Section("Server") {
                    Picker("Server", selection: $address) {
                        ForEach(servers.devices) { device in
                            Text(device.name).tag(device.address)
                        }
                    }
                    .pickerStyle(.menu)
                    .labelsHidden()
                    .accessibilityIdentifier("settings.server")
                }
                .panelRow()
                Section {
                    devices
                } header: {
                    HStack {
                        Text("Devices")
                        Spacer()
                        Button("Refresh devices", systemImage: "arrow.clockwise") { Task { await loadNetwork() } }
                            .disabled(refreshing)
                        Button("Add device", systemImage: "plus") { adding = true }
                    }
                    .labelStyle(.iconOnly)
                    .buttonStyle(.borderless)
                } footer: {
                    if let deviceIssue {
                        Text(deviceIssue).font(.footnote).foregroundStyle(.red)
                    } else if let networkIssue, settings?.error == nil {
                        Text(networkIssue).font(.footnote).foregroundStyle(.red)
                            .accessibilityIdentifier("settings.network-error")
                    }
                }
                .panelRow()
                if let settings {
                    VoiceSettings(store: settings)
                    if let issue = settings.error {
                        Section {
                            Text(issue).font(.footnote).foregroundStyle(.red)
                            Button("Retry") { Task { await settings.retry() } }
                        }
                        .panelRow()
                    }
                }
                Section("Notifications") {
                    Toggle("Allow notifications", isOn: Binding(get: { Notifications.shared.enabled },
                        set: { enabled in Task { await Notifications.shared.allow(enabled) } }))
                    if let issue = Notifications.shared.issue {
                        Text(issue).font(.footnote).foregroundStyle(.secondary)
                    }
                }
                .panelRow()
            }
            .formStyle(.grouped)
            .accessibilityIdentifier("settings.form")
        }
        .panelDismiss(onClose: onClose)
    }

    /// Client devices, this one first.
    @ViewBuilder private var devices: some View {
        if network == nil && networkIssue == nil {
            ProgressView().frame(maxWidth: .infinity).accessibilityIdentifier("settings.network-loading")
        }
        ForEach(servers.devices.filter { server in
            !(network?.devices.contains { $0.name == server.name } ?? false)
        }) { device in
            Text(device.name).lineLimit(1)
        }
        ForEach((network?.devices ?? []).sorted { $0.id == network?.current && $1.id != network?.current }) { device in
            Button { renaming = device } label: {
                LabeledContent {
                    if device.id == network?.current { Text("This device") }
                } label: {
                    Text(device.title).lineLimit(1)
                    if device.title != device.name { Text(device.name).lineLimit(1) }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .appMenu {
                Button("Rename", systemImage: "pencil") { renaming = device }
                if device.id != network?.current {
                    Button("Remove", systemImage: "trash", role: .destructive) { remove(device) }
                }
            }
        }
    }

    private func loadNetwork() async {
        refreshing = true
        defer { refreshing = false }
        guard let url = ServerAddress.parse(address) else { return }
        if network == nil, let cached = Self.networks[address] { network = cached }
        do {
            let target = address
            let token = await Task.detached {
                try? String(contentsOf: ClientPaths.token(target), encoding: .utf8)
            }.value
            let api = client ?? APIClient(baseURL: url, token: token)
            let fresh = try await api.network()
            try Task.checkCancellation()
            Self.networks[address] = fresh
            network = fresh
            servers.remember(address, name: fresh.server.name)
            networkIssue = nil
        } catch {
            // Keep what is already shown; only say why it could not refresh.
            networkIssue = error.localizedDescription
        }
    }

    private static var networks: [String: NetworkInfo] = [:]

    private func rename(_ device: NetworkInfo.Device, to name: String) {
        let label = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let client, label != (device.label ?? ""),
              let index = network?.devices.firstIndex(where: { $0.id == device.id }) else { return }
        network?.devices[index].label = label
        deviceIssue = nil
        Task {
            do { try await client.labelDevice(device.id, label: label) }
            catch {
                if let at = network?.devices.firstIndex(where: { $0.id == device.id }) { network?.devices[at].label = device.label }
                deviceIssue = "Couldn't rename \(device.title): \(error.localizedDescription)"
            }
        }
    }

    private func remove(_ device: NetworkInfo.Device) {
        guard let client, let index = network?.devices.firstIndex(where: { $0.id == device.id }) else { return }
        network?.devices.remove(at: index)
        deviceIssue = nil
        Task {
            do { try await client.removeDevice(device.id) }
            catch {
                network?.devices.insert(device, at: min(index, network?.devices.count ?? 0))
                deviceIssue = "Couldn't remove \(device.title): \(error.localizedDescription)"
            }
        }
    }
}

#if os(macOS)
private struct SettingsKey: FocusedValueKey {
    typealias Value = Binding<Bool>
}

private extension FocusedValues {
    var settingsPanel: Binding<Bool>? {
        get { self[SettingsKey.self] }
        set { self[SettingsKey.self] = newValue }
    }
}

private struct SettingsCommands: Commands {
    @FocusedValue(\.settingsPanel) private var settings

    var body: some Commands {
        CommandGroup(replacing: .appSettings) {
            Button("Settings…") { settings?.wrappedValue = true }
                .keyboardShortcut(",", modifiers: .command)
                .disabled(settings == nil)
        }
    }
}
#endif
