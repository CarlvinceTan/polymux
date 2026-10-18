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
                .frame(minWidth: 780, minHeight: 500)
                #else
                .frame(minWidth: 360, minHeight: 500)
                #endif
        }
        #if os(macOS)
        .defaultSize(width: 1060, height: 760)
        .commands {
            CommandGroup(replacing: .sidebar) {}
        }
        #endif
    }
}

@MainActor
final class AppSession {
    let client: APIClient
    let cache: LocalCache
    let sidebar: SidebarStore
    let settings: SettingsStore
    let avatars: AvatarStore
    /// Carries the connection when the server is only reachable from another network.
    let tunnel: Tunnel?

    init(address: String) async throws {
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
        settings = SettingsStore(client: client)
        avatars = AvatarStore(sidebar: sidebar)
    }

    private static func connect(_ address: String, url: URL) async throws -> (APIClient, Tunnel?) {
        let ticket = try? String(contentsOf: ClientPaths.ticket(address), encoding: .utf8)
        let (base, tunnel) = try await Tunnel.route(url, ticket: ticket)
        guard tunnel != nil else { return (APIClient(baseURL: url), nil) }
        // The tunnel's local address is not the one the credential was saved under.
        let token = try? String(contentsOf: ClientPaths.token(address), encoding: .utf8)
        return (APIClient(baseURL: base, token: token), tunnel)
    }
}

struct AppRoot: View {
    @AppStorage("serverAddress") private var address = "http://127.0.0.1:7337"
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
        #endif
        .neutralBackground()
        .task(id: address) {
            #if DEBUG
            guard !CommandLine.arguments.contains("--verify-approval") else { return }
            #endif
            while !Task.isCancelled {
                do {
                    let opened = try await AppSession(address: address)
                    session = opened
                    error = nil
                    Task { await announce(opened.client) }
                    #if DEBUG
                    if !VerifyFixture.local { Notifications.shared.connect(opened.client) }
                    #else
                    Notifications.shared.connect(opened.client)
                    #endif
                    while !Task.isCancelled {
                        await opened.settings.refresh()
                        if !opened.sidebar.isConnected { break }
                        do { try await Task.sleep(for: .seconds(3)) }
                        catch { break }
                    }
                } catch {
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
            ConnectionView(address: $address, settings: session?.settings, client: session?.client,
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
        address = target
    }

    /// Names this device on the server so the Devices list can tell it apart.
    private func announce(_ client: APIClient) async {
        #if os(macOS)
        let name = await Task.detached { Host.current().localizedName ?? "Mac" }.value
        #else
        let name = UIDevice.current.name
        #endif
        try? await client.renameDevice(name)
    }

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
            if let session {
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
    let settings: SettingsStore?
    let client: APIClient?
    let onClose: () -> Void
    @State private var draft = ""
    @FocusState private var serverFocused: Bool
    @State private var network: NetworkInfo? = nil
    @State private var networkIssue: String?
    @State private var removeIssue: String?
    @State private var refreshing = false
    @State private var adding = false

    var body: some View {
        PanelView("Settings", onClose: close) {
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
                    LabeledContent(network?.server.name ?? "Server", value: network?.server.platform ?? "")
                    TextField("Server address", text: $draft)
                        .labelsHidden()
                        .accessibilityIdentifier("settings.server")
                        .lineLimit(1)
                        .multilineTextAlignment(.leading)
                        .autocorrectionDisabled()
                        .focused($serverFocused)
                        .onSubmit { save() }
                        #if os(iOS)
                        .textInputAutocapitalization(.never)
                        .keyboardType(.URL)
                        #endif
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
                    if let removeIssue {
                        Text(removeIssue).font(.footnote).foregroundStyle(.red)
                    } else if let networkIssue {
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
            .defaultFocus($serverFocused, false)
            .accessibilityIdentifier("settings.form")
        }
        .panelDismiss(onClose: close)
        #if os(macOS)
        .background { SettingsFocus().frame(width: 0, height: 0).accessibilityHidden(true) }
        #endif
        .onAppear { draft = address; serverFocused = false }
        .task(id: draft) {
            guard ServerAddress.parse(draft) != nil else { return }
            do { try await Task.sleep(for: .milliseconds(600)) }
            catch { return }
            save()
        }
        .onDisappear { save() }
        .task(id: client?.baseURL) { await loadNetwork() }
        .onChange(of: address) { _, _ in Task { await loadNetwork() } }
        .panel(isPresented: $adding) {
            AddDevice(client: client) { link in
                URLRouter.shared.handler?(link)
                adding = false
                onClose()
            }
        }
    }

    private static let rowHeight: CGFloat = 40
    private static let visibleRows = 4

    /// Client devices in a window that shows four rows and scrolls past them, so the panel keeps its height.
    private var devices: some View {
        let rows = (network?.devices ?? []).sorted { $0.id == network?.current && $1.id != network?.current }
        let height = CGFloat(min(max(rows.count, 1), Self.visibleRows)) * Self.rowHeight
        return ScrollView {
            LazyVStack(spacing: 0) {
                ForEach(rows) { device in
                    HStack {
                        Text(device.name).lineLimit(1)
                        Spacer(minLength: 8)
                        if device.id == network?.current { Text("This device").foregroundStyle(.secondary) }
                    }
                    .frame(height: Self.rowHeight)
                    .contentShape(Rectangle())
                    .contextMenu {
                        if device.id != network?.current {
                            Button("Remove", systemImage: "trash", role: .destructive) { remove(device) }
                        }
                    }
                }
            }
        }
        .scrollIndicators(.hidden)
        .frame(height: height)
        .overlay { if network == nil && networkIssue == nil { ProgressView().accessibilityIdentifier("settings.network-loading") } }
    }

    private func loadNetwork() async {
        refreshing = true
        defer { refreshing = false }
        guard let url = ServerAddress.parse(address) else { return }
        if network == nil, let cached = Self.networks[address] { network = cached }
        do {
            let api = client ?? APIClient(baseURL: url)
            let fresh = try await api.network()
            Self.networks[address] = fresh
            network = fresh
            networkIssue = nil
        } catch {
            // Keep what is already shown; only say why it could not refresh.
            networkIssue = error.localizedDescription
        }
    }

    private static var networks: [String: NetworkInfo] = [:]

    private func remove(_ device: NetworkInfo.Device) {
        guard let client, let index = network?.devices.firstIndex(where: { $0.id == device.id }) else { return }
        network?.devices.remove(at: index)
        removeIssue = nil
        Task {
            do { try await client.removeDevice(device.id) }
            catch {
                network?.devices.insert(device, at: min(index, network?.devices.count ?? 0))
                removeIssue = "Couldn't remove \(device.name): \(error.localizedDescription)"
            }
        }
    }

    private func save() {
        guard let url = ServerAddress.parse(draft), url.absoluteString != address else { return }
        address = url.absoluteString
    }

    private func close() {
        save()
        onClose()
    }
}

#if os(macOS)
import AppKit

private struct SettingsFocus: NSViewRepresentable {
    func makeNSView(context: Context) -> SettingsAnchor { SettingsAnchor() }
    func updateNSView(_ view: SettingsAnchor, context: Context) {}
}

private final class SettingsAnchor: NSView {
    override var acceptsFirstResponder: Bool { true }
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard let window else { return }
        // Native form focus settles after the panel is inserted, even when SwiftUI focus starts false.
        DispatchQueue.main.async { [weak self, weak window] in
            DispatchQueue.main.async { [weak self, weak window] in
                guard let self, let window, self.window === window, window.isKeyWindow else { return }
                if window.firstResponder is NSTextField || (window.firstResponder as? NSTextView)?.isFieldEditor == true {
                    window.makeFirstResponder(self)
                }
            }
        }
    }
}
#endif
