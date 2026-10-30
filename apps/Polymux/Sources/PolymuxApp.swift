import CoreImage
import CryptoKit
import SwiftUI
import PolymuxKit
#if os(macOS)
import AppKit
import PolymuxUpdates
#else
import UIKit
#endif

@main
struct PolymuxApp: App {
    #if os(macOS)
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    #else
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    #endif

    init() {
        _ = Notifications.shared
        #if os(macOS)
        _ = Updates.shared
        #endif
    }
    var body: some Scene {
        WindowGroup {
            AppRoot()
                .tint(Color("AccentColor"))
                #if os(macOS)
                .frame(minWidth: PaneWidth.window, minHeight: 720)
                #endif
        }
        #if os(macOS)
        .defaultSize(width: 1060, height: 760)
        .commands {
            CommandGroup(replacing: .sidebar) {}
            CommandGroup(after: .appInfo) {
                Button("Check for Updates…") { Updates.shared.checkForUpdates() }
            }
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
    let vault: VaultLibrary
    let providers: ProviderStore
    let network: NetworkStore
    /// The chat selected on launch, read from cache before the first frame.
    private(set) var initialChat: ChatStore?
    private var profiles: [String: ProfileStore] = [:]
    private var drafts: [String: DraftStore] = [:]
    let route: ServerRoute?

    init(address: String) async throws {
        self.address = address
        initialChat = nil
        guard let url = ServerAddress.resolve(address, defaults: .standard) else {
            throw URLError(.badURL)
        }
        #if DEBUG
        if VerifyFixture.local {
            client = APIClient(baseURL: url, session: VerifyFixture.session(), token: "")
            route = nil
        } else {
            (client, route) = await Self.connect(address, url: url)
        }
        #else
        (client, route) = await Self.connect(address, url: url)
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
        let api = client
        let store = sidebar
        let selected = UserDefaults.standard.string(forKey: "lastChat.\(address)")
            .flatMap { id in store.displayed(id)?.id } ?? store.threads.first?.id
        let opening = selected.map {
            ChatStore(client: api, actor: store.cacheActor, threadID: $0, sidebar: store)
        }
        async let chatCache: Void = opening?.ready() ?? ()
        layout = SidebarLayout(client: client, cache: sidebar.cacheActor)
        await layout.load()
        settings = SettingsStore(client: client, cache: sidebar.cacheActor)
        await settings.restore()
        avatars = AvatarStore(sidebar: sidebar)
        vault = VaultLibrary(client: client.vaultClient())
        providers = ProviderStore(client: client, cache: sidebar.cacheActor)
        network = NetworkStore(client: client, cache: sidebar.cacheActor)
        async let providerCache: () = providers.restore()
        async let deviceCache: () = network.restore()
        _ = await (providerCache, deviceCache)
        await chatCache
        initialChat = opening
    }

    func profile(_ agentID: String) -> ProfileStore {
        if let store = profiles[agentID] { return store }
        let store = ProfileStore(agentID: agentID, client: client, cache: sidebar.cacheActor)
        profiles[agentID] = store
        return store
    }

    func draft(_ threadID: String) -> DraftStore {
        if let store = drafts[threadID] { return store }
        let store = DraftStore(client: client, cache: sidebar.cacheActor, threadID: threadID)
        drafts[threadID] = store
        return store
    }

    private static func connect(_ address: String, url: URL) async -> (APIClient, ServerRoute) {
        let credential = await Task.detached { try? ServerCredential.load(address) }.value
        let route = ServerRoute(url: url, ticket: credential?.ticket, token: credential?.token)
        let client = APIClient(baseURL: url, token: credential?.token,
                               endpoint: { try await route.resolve() }, recover: { await route.recover() })
        return (client, route)
    }
}

struct AppRoot: View {
    @AppStorage("serverAddress") private var address = "http://127.0.0.1:7337"
    @State private var servers = ServerStore()
    @State private var session: AppSession?
    @State private var error: String?
    @State private var showSettings = false
    /// Changes on every pairing link, so re-pairing the current server reconnects with the new credential.
    @State private var pairing = 0
    @State private var pairingTask: Task<Void, Error>?
    @State private var pairingIssue: String?
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        Group {
            #if DEBUG
            if CommandLine.arguments.contains("--verify-approval") {
                ApprovalFixture()
            } else if CommandLine.arguments.contains("--verify-status") {
                StatusFixture()
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
        .task(id: "\(address)#\(pairing)") {
            #if DEBUG
            guard !CommandLine.arguments.contains("--verify-approval"),
                  !CommandLine.arguments.contains("--verify-status") else { return }
            if CommandLine.arguments.contains("--verify-startup") {
                try? await Task.sleep(for: .seconds(8))
                guard !Task.isCancelled else { return }
            }
            #endif
            servers.remember(address)
            let target = address
            var current: AppSession?
            defer {
                let retired = current?.route
                Task { await retired?.stop() }
            }
            while !Task.isCancelled {
                do {
                    let opened = try await AppSession(address: target)
                    current = opened
                    try Task.checkCancellation()
                    session = opened
                    error = nil
                    Task { await announce(opened) }
                    #if DEBUG
                    if !VerifyFixture.local {
                        Notifications.shared.connect(opened.client, address: target)
                        Connections.shared.connect(opened.client, address: target)
                    }
                    #else
                    Notifications.shared.connect(opened.client, address: target)
                    Connections.shared.connect(opened.client, address: target)
                    #endif
                    while !Task.isCancelled {
                        await opened.settings.refresh()
                        if !opened.settings.isConnected { await opened.route?.recover() }
                        do { try await Task.sleep(for: .seconds(3)) }
                        catch { break }
                    }
                } catch {
                    guard !Task.isCancelled else { return }
                    session = nil
                    self.error = error.isOffline ? "Polymux keeps trying to reconnect." : "Check the server in Connection settings."
                }
                if Task.isCancelled { break }
                try? await Task.sleep(for: .seconds(2))
            }
        }
        .onOpenURL { handleConnect($0) }
        .task(id: activeSession?.client.id) {
            guard let session = activeSession else { return }
            while !Task.isCancelled {
                await session.network.refresh()
                if let network = session.network.value { servers.remember(session.address, name: network.server.name) }
                do { try await Task.sleep(for: .seconds(5)) } catch { return }
            }
        }
        .task(id: activeSession?.client.id) {
            guard let store = activeSession?.providers else { return }
            while !Task.isCancelled {
                await store.refreshProviders()
                do { try await Task.sleep(for: .seconds(5)) } catch { return }
            }
        }
        .task(id: activeSession?.client.id) {
            guard let store = activeSession?.providers else { return }
            while !Task.isCancelled {
                await store.refreshOptions()
                do { try await Task.sleep(for: .seconds(60)) } catch { return }
            }
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active, let route = session?.route { Task { await route.resume() } }
        }
        .onAppear {
            URLRouter.shared.handler = { handleConnect($0) }
        }
        .onDisappear { pairingTask?.cancel() }
        .panel(isPresented: $showSettings) {
            ConnectionView(address: $address, servers: servers, settings: activeSession?.settings,
                            client: activeSession?.client,
                            join: join,
                            joinIssue: pairingIssue,
                           onClose: { showSettings = false })
                .id(activeSession?.client.id)
        }
        .panelHost()
        #if os(macOS)
        .toolbar(removing: .title)
        #endif
        .environment(session?.avatars)
        .environment(activeSession?.vault)
        .environment(activeSession?.providers)
        .environment(activeSession?.network)
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
        Task {
            do { try await join(url) }
            catch is CancellationError {}
            catch {
                pairingIssue = error.isOffline ? "Can’t reach this server. Check its connection and create a new link." : error.reason(or: "Couldn’t pair with this server.")
                showSettings = true
            }
        }
    }

    private func join(_ url: URL) async throws {
        guard let invite = Invite(link: url), let parsed = ServerAddress.parse(invite.url) else {
            throw ClientError.response(400, "Paste a valid Polymux pairing link.")
        }
        let previous = pairingTask
        previous?.cancel()
        let task = Task<Void, Error> {
            // Finish any earlier write before this invite replaces it.
            _ = await previous?.result
            try Task.checkCancellation()
            let token = try await Pairing.redeem(invite)
            let known = await Task.detached {
                try? ServerCredential.load(parsed.absoluteString)?.ticket
            }.value
            let kept = await Tunnel.address(for: parsed, ticket: invite.ticket, token: token, knownTicket: known)
            try Task.checkCancellation()
            let target = kept.absoluteString
            do {
                try await ServerCredential(token: token, ticket: invite.ticket).save(target)
            } catch is CancellationError {
                throw CancellationError()
            } catch {
                throw ClientError.response(0, "Couldn’t save this connection on this device.")
            }
            // Publishing is the commit point; even cancellation must reconnect once it succeeds.
            // A tunnel-only server's name arrives once connected.
            servers.remember(target, name: kept == parsed ? nil : "Server")
            address = target
            pairing += 1
            pairingIssue = nil
            await Pairing.finish(invite)
        }
        pairingTask = task
        try await withTaskCancellationHandler {
            try await task.value
        } onCancel: {
            task.cancel()
        }
    }

    /// Names this device on the server so the Devices list can tell it apart.
    private func announce(_ session: AppSession) async {
        #if os(macOS)
        let name = await Task.detached { Host.current().localizedName ?? "Mac" }.value
        #else
        let name = UIDevice.current.name
        #endif
        try? await session.client.renameDevice(name)
        await session.network.refresh()
        if let network = session.network.value { servers.remember(session.address, name: network.server.name) }
    }

    private var activeSession: AppSession? { session?.address == address ? session : nil }

    private var appearance: String {
        #if DEBUG
        if VerifyFixture.enabled || CommandLine.arguments.contains("--verify-status") {
            return UserDefaults.standard.string(forKey: "AppleInterfaceStyle")?.lowercased() ?? "system"
        }
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
                    .id(session.client.id)
            } else if let error {
                ContentUnavailableView {
                    Label("Can’t reach your server", systemImage: "network")
                } description: {
                    Text(error)
                } actions: {
                    Button("Connection settings") { showSettings = true }
                }
            } else {
                Image("Logo")
                    .renderingMode(.template)
                    .resizable()
                    .scaledToFit()
                    .frame(width: 96, height: 96)
                    .foregroundStyle(.primary)
                    .accessibilityLabel("Polymux")
                    .accessibilityValue("Connecting")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }
}

struct ConnectionView: View {
    @Binding var address: String
    let servers: ServerStore
    let settings: SettingsStore?
    let client: APIClient?
    let join: (URL) async throws -> Void
    let joinIssue: String?
    let onClose: () -> Void
    @Environment(NetworkStore.self) private var networkStore: NetworkStore?
    @Environment(ProviderStore.self) private var providerStore: ProviderStore?
    private var network: NetworkInfo? { networkStore?.value }
    private var networkIssue: String? { networkStore?.issue }
    private var deviceIssue: String? { networkStore?.actionIssue }
    private var unreachable: Bool { networkStore?.isConnected != true }
    private var providers: [Provider] { providerStore?.providers ?? [] }
    @State private var renaming: NetworkInfo.Device?
    @State private var adding = false
    @State private var providerTarget: ProviderTarget?
    @State private var category = SettingsTab.general
    @Environment(\.horizontalSizeClass) private var sizeClass

    /// The settings modal's left tab column, matching the macOS Settings shape.
    static let navWidth: CGFloat = 200

    init(address: Binding<String>, servers: ServerStore, settings: SettingsStore?, client: APIClient?,
         join: @escaping (URL) async throws -> Void, joinIssue: String?,
         onClose: @escaping () -> Void) {
        _address = address
        self.servers = servers
        self.settings = settings
        self.client = client
        self.join = join
        self.joinIssue = joinIssue
        self.onClose = onClose
    }

    var body: some View {
        // One container, so returning from a sub-view doesn't refetch over optimistic edits.
        ZStack {
            if adding {
                AddDevice(client: client, onBack: { adding = false }) { link in
                    try await join(link)
                    onClose()
                }
            } else if let target = providerTarget {
                ProviderEditor(client: client, original: target.provider, onBack: { providerTarget = nil }) {
                    providerStore?.accept($0)
                    Task { await settings?.refresh() }
                }
            } else if let renaming {
                DeviceView(device: renaming, onBack: { self.renaming = nil }) { rename(renaming, to: $0) }
            } else {
                settingsPanel
            }
        }
        .task(id: client?.id) { await networkStore?.refresh() }
    }

    private var settingsPanel: some View {
        PanelView(category.title, onClose: onClose, header: compact ? AnyView(compactTabs) : nil,
                  scrollUnder: false, width: PanelSize.width + Self.navWidth, height: PanelSize.maximumHeight,
                  closeLabel: "Close Settings") {
            HStack(spacing: 0) {
                if !compact {
                    settingsNav
                    Divider()
                }
                settingsPane
            }
        }
        .panelDismiss(onClose: onClose)
    }

    private var compact: Bool { sizeClass == .compact }

    /// iPhone keeps tabs on top; the sidebar column needs a regular width.
    private var compactTabs: some View {
        ScrollView(.horizontal) {
            PillPicker("Settings tab", selection: $category,
                       options: SettingsTab.allCases.map { ($0.title, $0) })
        }
        .scrollIndicators(.hidden)
        .padding(.horizontal, 14)
    }

    private var settingsNav: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(SettingsTab.allCases) { tab in
                navRow(tab)
            }
            Spacer(minLength: 12)
        }
        .padding(.horizontal, 10)
        // The first tab lines up with the pane's first section title.
        .padding(.top, 4)
        .frame(width: Self.navWidth)
    }

    private func navRow(_ tab: SettingsTab) -> some View {
        Button {
            category = tab
        } label: {
            HStack(spacing: 9) {
                Image(systemName: tab.symbol).frame(width: 20)
                Text(tab.title)
            }
            .font(.body)
            .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
            .padding(.horizontal, 10)
            .background {
                if category == tab {
                    RoundedRectangle(cornerRadius: 7).fill(Color.accentColor.opacity(0.16))
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(tab.title)
        .accessibilityAddTraits(category == tab ? [.isSelected] : [])
    }

    @ViewBuilder private var settingsPane: some View {
        if category == .vault {
            VaultSettings()
        } else {
            settingsForm
        }
    }

    private var settingsForm: some View {
        Form {
            switch category {
            case .general: generalSections
            case .models:
                ProviderSettings(client: client, settings: settings) { providerTarget = ProviderTarget(provider: $0) }
                if let settings { VoiceSettings(store: settings, providers: providers) }
            case .vault: EmptyView()
            case .updates: UpdatesSettings(server: network?.server)
            }
        }
        .formStyle(.grouped)
        #if os(macOS)
        // Grouped forms keep an internal inset on macOS.
        .padding(.top, -PanelStyle.formInset)
        #else
        .contentMargins(.top, 0, for: .scrollContent)
        #endif
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .id(category)
    }

    @ViewBuilder private var generalSections: some View {
        Section("Appearance") {
            SettingsPicker("Theme", selection: Binding(get: { settings?.value?.appearance ?? "system" },
                                                      set: { settings?.edit(SettingsPatch(appearance: $0)) }),
                           options: [("Light", "light"), ("Dark", "dark"), ("System", "system")])
            .disabled(settings?.value == nil)
        }
        .panelRow()
        if let issue = settings?.error {
            Section { Text(issue).font(.footnote).foregroundStyle(.muted) }
                .panelRow()
        }
        Section {
            devices
        } header: {
            HStack {
                Text("Devices").foregroundStyle(Color.primary)
                Spacer()
                Button("Add device", systemImage: "plus") { adding = true }
            }
            .labelStyle(.iconOnly)
            .buttonStyle(.borderless)
        } footer: {
            if let issue = deviceIssue ?? networkIssue {
                Text(issue).font(.footnote).foregroundStyle(.muted)
            }
            if let joinIssue { Text(joinIssue).font(.footnote).foregroundStyle(.primary) }
        }
        .panelRow()
        Section("Notifications") {
            Toggle("Allow notifications", isOn: Binding(get: { Notifications.shared.enabled },
                set: { enabled in Task { await Notifications.shared.allow(enabled) } }))
                .accessibilityLabel("Allow notifications")
            if let issue = Notifications.shared.issue {
                Text(issue).font(.footnote).foregroundStyle(.muted)
            }
        }
        .panelRow()
        Section {
            ForEach(Connections.available, id: \.self) { capability in
                Toggle(Connections.title(capability), isOn: Binding(get: { Connections.shared.enabled.contains(capability) },
                    set: { on in Task { await Connections.shared.allow(capability, on) } }))
                    .accessibilityLabel(Connections.title(capability))
                if capability == "location" {
                    Text("Lets bots on this server request this device’s current location and address.")
                        .font(.footnote).foregroundStyle(.muted)
                }
            }
            if let issue = Connections.shared.issue {
                Text(issue).font(.footnote).foregroundStyle(.muted)
            }
        } header: {
            Text("Connections").foregroundStyle(Color.primary)
        }
        .panelRow()
    }

    /// Client devices, this one first.
    @ViewBuilder private var devices: some View {
        if network == nil && networkIssue == nil && networkStore?.hasChecked != true {
            ProgressView().frame(maxWidth: .infinity).accessibilityLabel("Loading devices")
        } else if network == nil && unreachable {
            Text("Offline").foregroundStyle(.muted).frame(maxWidth: .infinity)
        }
        // Switching only matters once this app knows more than one server.
        if servers.devices.count > 1 {
            SettingsPicker("Server", selection: $address, options: servers.devices.map { ($0.name, $0.address) })
            .pickerStyle(.menu)
            .accessibilityIdentifier("settings.server")
        }
        ForEach((network?.devices ?? []).sorted { $0.id == network?.current && $1.id != network?.current }) { device in
            HStack {
                Button { renaming = device } label: {
                    LabeledContent {
                        if device.id == network?.current { Text("This device") }
                    } label: {
                        HStack(spacing: 10) {
                            StatusDot(state: status(device))
                            VStack(alignment: .leading) {
                                HStack(spacing: 6) {
                                    Text(device.title).lineLimit(1)
                                    if device.id == network?.host {
                                        Image(systemName: "server.rack").foregroundStyle(.muted).offset(y: 1).accessibilityLabel("Server")
                                    }
                                }
                                if device.title != device.name { Text(device.name).lineLimit(1) }
                            }
                        }
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityLabel(device.title)
                .accessibilityValue(details(device))
                .appMenu {
                    Button("Rename", systemImage: "pencil") { renaming = device }
                    if removable(device) {
                        Button("Remove", systemImage: "trash", role: .destructive) { remove(device) }
                    }
                }
                if removable(device) {
                    Button("Remove", role: .destructive) { remove(device) }
                        .buttonStyle(.bordered)
                        .buttonBorderShape(.roundedRectangle)
                        .accessibilityLabel("Remove \(device.title)")
                        .help("Remove device")
                }
            }
        }
    }

    private func removable(_ device: NetworkInfo.Device) -> Bool {
        device.id != network?.current && device.id != network?.host
    }

    /// What a device row shows besides its name, for VoiceOver.
    private func details(_ device: NetworkInfo.Device) -> String {
        let state: String? = switch status(device) {
        case .online: "Online"
        case .offline: "Offline"
        case .pairing: "Pairing"
        case .checking: "Checking"
        case .unknown: nil
        }
        return [device.id == network?.host ? "Server" : nil, state, device.id == network?.current ? "This device" : nil,
                device.title != device.name ? device.name : nil]
            .compactMap { $0 }.joined(separator: ", ")
    }

    private func status(_ device: NetworkInfo.Device) -> StatusDot.State {
        if device.paired == false { return .pairing }
        // The list is from the cache when the server can't be reached, so presence is unknown.
        if unreachable || networkIssue != nil { return .unknown }
        return device.id == network?.current || network?.online?.contains(device.id) == true ? .online : .offline
    }

    private func rename(_ device: NetworkInfo.Device, to name: String) {
        let label = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard label != (device.label ?? "") else { return }
        Task { await networkStore?.rename(device, label: label) }
    }

    private func remove(_ device: NetworkInfo.Device) {
        guard removable(device) else { return }
        Task { await networkStore?.remove(device) }
    }
}

/// The provider being added (nil) or edited.
private struct ProviderTarget {
    let provider: Provider?
}

private enum SettingsTab: String, CaseIterable, Identifiable {
    case general, models, vault, updates

    var id: String { rawValue }

    var title: String { rawValue.capitalized }

    var symbol: String {
        switch self {
        case .general: "gearshape"
        case .models: "cpu"
        case .vault: "lock"
        case .updates: "arrow.down.circle"
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
