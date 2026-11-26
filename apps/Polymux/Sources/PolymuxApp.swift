import CoreImage
import CryptoKit
import SwiftUI
import AvatarKit
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
    #if os(macOS)
    @State private var statusBar = StatusBar.shared
    #endif

    init() {
        _ = Notifications.shared
        #if os(macOS)
        _ = Updates.shared
        StatusBar.shared.start()
        #endif
    }
    var body: some Scene {
        WindowGroup {
            AppRoot()
                .tint(Color("AccentColor"))
        }
        #if os(macOS)
        .defaultSize(width: 1060, height: 760)
        .commands {
            CommandGroup(after: .appInfo) {
                Button("Check for Updates…") { Updates.shared.checkForUpdates() }
            }
            SettingsCommands()
        }
        #endif
        #if os(macOS)
        MenuBarExtra(isInserted: $statusBar.menu) {
            StatusMenu(bar: statusBar)
        } label: {
            StatusLabel(bar: statusBar)
        }
        .menuBarExtraStyle(.window)
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
    let plugins: PluginStore
    let hub: HubStore
    let providers: ProviderStore
    let network: NetworkStore
    /// Recent chats stay in memory, so reopening one draws its messages in the first frame.
    private var chats: [String: ChatStore] = [:]
    private var recentChats: [String] = []
    /// The message each chat was scrolled to, for chats left away from their end.
    var places: [String: String] = [:]
    private var profiles: [String: ProfileStore] = [:]
    private var drafts: [String: DraftStore] = [:]
    let route: ServerRoute?

    init(address: String) async throws {
        self.address = address
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
            ChatStore(client: api, actor: store.cacheActor, threadID: $0, sidebar: store, resumes: false)
        }
        async let chatCache: Void = opening?.ready() ?? ()
        layout = SidebarLayout(client: client, cache: sidebar.cacheActor)
        await layout.load()
        settings = SettingsStore(client: client, cache: sidebar.cacheActor)
        await settings.restore()
        avatars = AvatarStore(sidebar: sidebar)
        vault = VaultLibrary(client: client.vaultClient())
        plugins = PluginStore(client: client, cache: sidebar.cacheActor, surfaces: PluginSurface.available)
        await plugins.restore()
        hub = HubStore(client: client, cache: sidebar.cacheActor)
        await hub.restore()
        providers = ProviderStore(client: client, cache: sidebar.cacheActor)
        network = NetworkStore(client: client, cache: sidebar.cacheActor)
        async let providerCache: () = providers.restore()
        async let deviceCache: () = network.restore()
        _ = await (providerCache, deviceCache)
        await chatCache
        // The chat selected on launch is read from cache before the first frame.
        if let opening {
            chats[opening.threadID] = opening
            recentChats = [opening.threadID]
        }
    }

    func chat(_ id: String) -> ChatStore {
        if let store = chats[id] {
            if recentChats.last != id {
                recentChats.removeAll { $0 == id }
                recentChats.append(id)
            }
            return store
        }
        let store = ChatStore(client: client, actor: sidebar.cacheActor, threadID: id, sidebar: sidebar, resumes: false)
        chats[id] = store
        recentChats.append(id)
        if recentChats.count > 12 { chats[recentChats.removeFirst()] = nil }
        return store
    }

    /// Reads a chat the pointer settled on, or a neighbour of the open one, ahead of the click.
    func warm(_ id: String) {
        Task { await ready(id) }
    }

    private func ready(_ id: String) async {
        await draft(id).restore()
        await chat(id).warm()
    }

    /// Warms what's likely to be opened after cached content appears.
    func prepare() async {
        EmojiCatalogue.warm()
        Task { await prepareHub() }
        await sidebar.refresh()
        await layout.refresh()
        let placed = sidebar.chats.map(layout.applying)
        let open = UserDefaults.standard.string(forKey: "lastChat.\(address)").flatMap { sidebar.displayed($0)?.id }
            ?? placed.first?.id
        let likely = [open].compactMap { $0 } + placed.filter { $0.pinned == true }.map(\.id) + placed.prefix(6).map(\.id)
        var seen: Set<String> = []
        await withTaskGroup(of: Void.self) { group in
            for id in likely.prefix(10) where seen.insert(id).inserted {
                group.addTask { await self.ready(id) }
            }
        }
        guard let open else { return }
        let members = sidebar.displayed(open)?.participantIDs ?? []
        await withTaskGroup(of: Void.self) { group in
            for id in members where !id.hasPrefix("local-") {
                group.addTask { await self.profile(id).refresh() }
            }
            let images = chat(open).messages.suffix(30).flatMap(\.attachments).filter { $0.kind == AttachmentKind.image }
            for image in images.suffix(8) {
                group.addTask { await DecodedImages.warm(image, client: self.client) }
            }
        }
    }

    /// Hub's recent chats, faces and images, so opening it shows them at once. Nothing here marks a chat read.
    private func prepareHub() async {
        await plugins.refresh()
        guard plugins.plugin(named: HubStore.plugin)?.state == PluginState.ready else { return }
        let recent = await hub.preload()
        for chat in hub.chats.filter({ !$0.avatar.isEmpty }).sorted(by: { $0.at > $1.at }).prefix(40) {
            await DecodedImages.warm(HubAvatar.address(chat.avatar, client: client), name: HubAvatar.file(chat.name), client: client)
        }
        for chat in recent.prefix(3) {
            let messages = (hub.messages[chat.id] ?? []).suffix(30).map(\.bubble)
            let images = messages.flatMap(\.attachments).filter { $0.kind == AttachmentKind.image }
            for image in images.suffix(8) { await DecodedImages.warm(image, client: client) }
        }
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
        let route = ServerRoute(url: url, ticket: credential?.ticket, token: credential?.token,
                                also: credential?.also.flatMap(URL.init(string:)))
        let client = APIClient(baseURL: url, token: credential?.token,
                               endpoint: { try await route.resolve() }, recover: { await route.recover() })
        return (client, route)
    }
}

struct AppRoot: View {
    @AppStorage("serverAddress") private var address = ServerHost.address
    @State private var pool = SessionPool(servers: ServerStore())
    @State private var showSettings = SettingsTab.verifying != nil
    @State private var joinChoice: String?
    /// Kept here because a change of server rebuilds Settings.
    @State private var settingsTab = SettingsTab.verifying ?? .general
    @State private var pairingTask: Task<Void, Error>?
    @State private var pairingIssue: String?
    /// Set when a new install starts its own server, or on a fresh iPhone install.
    @AppStorage("onboarding.pending") private var onboarding = Onboarding.verifying || SetupDefaults.pending
    #if os(iOS)
    @State private var onboardingLink: URL?
    #endif
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
        #else
        // Phone test runs pass this so auto-lock can't interrupt them.
        .onAppear { UIApplication.shared.isIdleTimerDisabled = CommandLine.arguments.contains("--keep-awake") }
        #endif
        .neutralBackground()
        // Decodes the avatars the first frame shows, while the session is still loading.
        .task { await Task.detached(priority: .userInitiated) { preloadMascots() }.value }
        .task(id: address) {
            #if DEBUG
            guard !CommandLine.arguments.contains("--verify-approval"),
                  !CommandLine.arguments.contains("--verify-status") else { return }
            if CommandLine.arguments.contains("--verify-startup") {
                try? await Task.sleep(for: .seconds(8))
                guard !Task.isCancelled else { return }
            }
            if let second = VerifyFixture.secondServer {
                pool.servers.remember(address)
                pool.servers.remember(second)
            }
            #endif
            await ServerHost.shared.prepare()
            pool.show(address)
        }
        .onOpenURL { handleConnect($0) }
        .task(id: activeSession?.client.id) {
            guard let session = activeSession else { return }
            while !Task.isCancelled {
                await session.network.refresh()
                pool.report(session)
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
            if phase == .active { pool.resume() }
        }
        .onAppear {
            URLRouter.shared.handler = { handleConnect($0) }
        }
        .onDisappear { pairingTask?.cancel() }
        .onChange(of: showSettings) { _, shown in
            guard !shown else { return }
            settingsTab = .general
            // Closing Settings answers the join question with the default: keep separate.
            joinChoice = nil
        }
        .panel(isPresented: settingsShown, page: true, animatesResize: false) {
            ConnectionView(pool: pool, settings: activeSession?.settings,
                           client: activeSession?.client,
                           join: join,
                           joinIssue: pairingIssue,
                           joined: $joinChoice,
                           category: $settingsTab,
                           onClose: { showSettings = false })
                .id(activeSession?.client.id)
        }
        #if os(macOS)
        .task(id: localSession?.client.id) { Permissions.shared.client = localSession?.client }
        .task(id: activeSession?.client.id) { if let activeSession { StatusBar.shared.session = activeSession } }
        .onChange(of: appearance, initial: true) { Permissions.shared.appearance = $1 }
        #endif
        .panelHost()
        #if os(macOS)
        .toolbar(removing: .title)
        #endif
        .environment((activeSession ?? pool.latest)?.avatars)
        .environment(activeSession?.vault)
        .environment(activeSession?.plugins)
        .environment(activeSession?.hub)
        .environment(activeSession?.sidebar)
        .environment(activeSession?.providers)
        .environment(activeSession?.network)
        .appAppearance(appearance)
        #if os(macOS)
        .background {
            WindowFill(setup: onboarding).frame(width: 0, height: 0).accessibilityHidden(true)
            InputFocus().frame(width: 0, height: 0).accessibilityHidden(true)
        }
        #endif
    }

    private func handleConnect(_ url: URL) {
        if url.scheme == "polymux", url.host == "settings" {
            showSettings = true
            return
        }
        guard Invite(link: url) != nil else { return }
        #if os(iOS)
        if onboarding {
            onboardingLink = url
            return
        }
        #endif
        Task {
            do { try await join(url) }
            catch is CancellationError {}
            catch {
                pairingIssue = error.isOffline ? "Can’t reach this server. Check its connection and create a new link." : error.reason(or: "Couldn’t pair with this server.")
                settingsTab = .network
                showSettings = true
            }
        }
    }

    private func join(_ url: URL) async throws {
        guard let invite = Invite(link: url) else {
            throw ClientError.response(400, "Paste a valid Polymux pairing link.")
        }
        let previous = pairingTask
        previous?.cancel()
        let task = Task<Void, Error> {
            // Finish any earlier write before this invite replaces it.
            _ = await previous?.result
            try Task.checkCancellation()
            let target = try await pool.pair(invite)
            // Asked first: switching rebuilds Settings, which cancels this task's view.
            if await pool.keepsData(joined: target) { joinChoice = target }
            address = target
            pairingIssue = nil
            await pool.retireEmptyHost(joined: target)
        }
        pairingTask = task
        try await withTaskCancellationHandler {
            try await task.value
        } onCancel: {
            task.cancel()
        }
    }

    private var activeSession: AppSession? { pool.sessions[address] }
    private var error: String? { pool.issues[address] }

    /// Settings closes while setup is on screen, then returns when setup finishes.
    private var settingsShown: Binding<Bool> {
        Binding(get: { showSettings && !onboarding }, set: { showSettings = $0 })
    }

    /// This Mac's own server, which reports its permissions to the Grant window.
    private var localSession: AppSession? {
        #if DEBUG
        if CommandLine.arguments.contains("--verify-permissions") { return activeSession }
        #endif
        return pool.sessions[ServerHost.address]
    }

    private var appearance: String {
        #if DEBUG
        if VerifyFixture.enabled || CommandLine.arguments.contains("--verify-status") {
            return UserDefaults.standard.string(forKey: "AppleInterfaceStyle")?.lowercased() ?? "system"
        }
        if CommandLine.arguments.contains("--verify-approval") {
            return CommandLine.arguments.contains("--approval-dark") ? "dark" : "light"
        }
        #endif
        return activeSession?.settings.value?.appearance ?? "system"
    }

    @ViewBuilder private var content: some View {
        #if os(macOS)
        if onboarding {
            Onboarding(pool: pool, join: join) {
                UserDefaults.standard.set(true, forKey: "onboarding.completed")
                onboarding = false
            }
        } else {
            mainContent
        }
        #else
        // iPhone has no window to fill, so setup covers the whole screen.
        mainContent
            .fullScreenCover(isPresented: $onboarding) {
                Onboarding(pool: pool, join: join, incoming: $onboardingLink) {
                    UserDefaults.standard.set(true, forKey: "onboarding.completed")
                    onboarding = false
                }
                .environment(activeSession?.plugins)
                .interactiveDismissDisabled()
            }
        #endif
    }

    private var mainContent: some View {
        Group {
            if let session = activeSession {
                ShellView(session: session, pool: pool, showSettings: $showSettings)
                    .id(session.client.id)
            } else if let error, activeSession == nil {
                ContentUnavailableView {
                    Label("Can’t reach your server", systemImage: "network")
                } description: {
                    Text(error)
                } actions: {
                    Button("Connection settings") { showSettings = true }
                }
            } else {
                // Opening the saved session is local and takes milliseconds, so nothing is drawn meanwhile.
                Color.clear.frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }
}

struct ConnectionView: View {
    let pool: SessionPool
    let settings: SettingsStore?
    let client: APIClient?
    let join: (URL) async throws -> Void
    let joinIssue: String?
    @Binding var joined: String?
    @Binding var category: SettingsTab
    let onClose: () -> Void
    @Environment(NetworkStore.self) private var networkStore: NetworkStore?
    @Environment(ProviderStore.self) private var providerStore: ProviderStore?
    @Environment(PluginStore.self) private var plugins: PluginStore?
    private var network: NetworkInfo? { networkStore?.value }
    private var providers: [Provider] { providerStore?.providers ?? [] }
    @State private var networkPage = SettingsTab.verifiesPage ? UserDefaults.standard.string(forKey: "serverAddress") : nil
    @State private var joining = false
    @State private var adding = false
    @State private var providerTarget: ProviderTarget?
    @State private var pluginTarget: String?
    @State private var arrangingPlugins = false
    @State private var editingMedia: MediaKind?
    @State private var nav = PageNav()
    /// Bumped on each page opened, so a page reopened mid-transition starts fresh.
    @State private var opened = 0
    @State private var crumb: PageCrumb?
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.colorScheme) private var scheme

    init(pool: SessionPool, settings: SettingsStore?, client: APIClient?,
         join: @escaping (URL) async throws -> Void, joinIssue: String?, joined: Binding<String?>,
         category: Binding<SettingsTab>, onClose: @escaping () -> Void) {
        self.pool = pool
        self.settings = settings
        self.client = client
        self.join = join
        self.joinIssue = joinIssue
        _joined = joined
        _category = category
        self.onClose = onClose
    }

    var body: some View {
        Group {
            if compact {
                VStack(spacing: 0) {
                    header
                    if !subpage { compactTabs.padding(.bottom, 12) }
                    pane
                }
            } else {
                HStack(spacing: 0) {
                    SettingsNav(category: $category)
                        // The join question is answered before moving on.
                        .disabled(joined != nil)
                    Divider()
                    VStack(spacing: 0) {
                        header
                        pane
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(PanelSurface.color(scheme))
        .environment(\.pageNav, nav)
        .onPreferenceChange(PageCrumbs.self) { crumb = $0 }
        .preference(key: PanelHeight.self, value: PanelSize.maximumHeight)
        .preference(key: PanelWidth.self, value: PanelSize.width + SettingsNav.width)
        .onChange(of: category) { reset() }
        #if os(macOS)
        .allowsWindowActivationEvents()
        #endif
        .task(id: client?.id) { await networkStore?.refresh() }
    }

    /// One container, so returning from a sub-view doesn't refetch over optimistic edits.
    private var pane: some View {
        let page = opened
        // A page's callbacks act only while it is still the open one, so a late save can't close the next page.
        func back(_ change: @escaping () -> Void) -> () -> Void {
            { nav.pop { if opened == page { change() } } }
        }
        return ZStack {
            Group {
                if let joined {
                    JoinChoice(pool: pool, joined: joined) {
                        self.joined = nil
                        onClose()
                    }
                } else if joining {
                    AddDevice(client: client, onBack: back { joining = false }) { link in
                        try await join(link)
                        if joined == nil { onClose() }
                    }
                } else if adding {
                    AddDevice(client: client, onBack: back { adding = false })
                } else if let target = providerTarget {
                    ProviderEditor(client: client, original: target.provider, onBack: back { providerTarget = nil }) {
                        providerStore?.accept($0)
                        Task { await settings?.refresh() }
                    }
                } else if let pluginTarget {
                    PluginDetail(name: pluginTarget, onBack: back { self.pluginTarget = nil })
                } else if arrangingPlugins, let plugins {
                    PluginLayoutEditor(store: plugins, onBack: back { arrangingPlugins = false })
                } else if let kind = editingMedia, let settings {
                    MediaOrderView(kind: kind, store: settings, providers: providers, onBack: back { editingMedia = nil })
                } else if let networkPage, let server = pool.servers.devices.first(where: { $0.address == networkPage }) {
                    NetworkPage(pool: pool, server: server, onBack: back { self.networkPage = nil })
                }
            }
            .id(opened)
            if !subpage {
                settingsPane
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .scrollContentBackground(.hidden)
                    .scrollIndicators(.hidden)
                    .safeAreaInset(edge: .bottom, spacing: 0) { Color.clear.frame(height: 16) }
                    .columnFade()
                    .background(PanelSurface.color(scheme))
                    .panelDismiss(onClose: onClose)
                    .pageSlide(nav)
            }
        }
        .clipped()
    }

    private var subpage: Bool {
        joined != nil || joining || adding || providerTarget != nil || pluginTarget != nil || arrangingPlugins
            || editingMedia != nil || networkPage.map { address in pool.servers.devices.contains { $0.address == address } } == true
    }

    /// The tab, then the page inside it, like a breadcrumb; the page's own back and buttons sit beside it.
    private var header: some View {
        let page = subpage ? crumb : nil
        let enabled = page?.enabled ?? true
        let title = page?.title ?? category.title
        return HStack(spacing: 10) {
            if let back = page?.back {
                PanelNavButton(symbol: "chevron.left", label: "Back", enabled: enabled) { nav.pop { back() } }
                    .transition(.opacity.combined(with: .scale(scale: 0.8)))
            }
            HStack(spacing: 8) {
                TabIcon(tab: category)
                if page != nil {
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.semibold)).foregroundStyle(.muted)
                        .accessibilityHidden(true)
                        .transition(.opacity)
                }
                ZStack(alignment: .leading) {
                    Text(title)
                        .font(.headline).lineLimit(1)
                        .accessibilityAddTraits(.isHeader)
                        .id(title)
                        .transition(.asymmetric(insertion: .opacity.combined(with: .offset(x: nav.back ? -12 : 12)),
                                                removal: .opacity.animation(Motion.quick)))
                }
            }
            Spacer(minLength: 8)
            if let accessory = page?.accessory {
                PanelNavButton(symbol: accessory.symbol, label: accessory.label, enabled: enabled, action: accessory.action)
                    .transition(.opacity.combined(with: .scale(scale: 0.8)))
            }
            PanelNavButton(symbol: "xmark", label: "Close Settings", enabled: enabled) {
                if let close = page?.close { close() } else { onClose() }
            }
        }
        .frame(minHeight: 44)
        // The icon sits over the section icons below; Back and Close sit at the cards' edges.
        .padding(.leading, page?.back == nil ? PanelStyle.formInset + 10 : PanelStyle.formInset)
        .padding(.trailing, PanelStyle.formInset)
        .padding(.vertical, 16)
        .motion(Motion.page, value: page)
    }

    private func open(_ change: @escaping () -> Void) {
        nav.push {
            opened += 1
            change()
        }
    }

    /// Back to the tab's own page, as switching tabs does.
    private func reset() {
        nav.cancel()
        opened += 1
        joining = false
        adding = false
        providerTarget = nil
        pluginTarget = nil
        arrangingPlugins = false
        editingMedia = nil
        networkPage = nil
    }

    private var compact: Bool { sizeClass == .compact }

    /// iPhone keeps tabs on top; the sidebar column needs a regular width.
    private var compactTabs: some View {
        ScrollView(.horizontal) {
            PillPicker("Settings tab", selection: $category,
                       options: SettingsTab.allCases.map { ($0.title, $0) })
        }
        .edgeFade()
        .scrollIndicators(.hidden)
        .padding(.horizontal, 14)
    }

    @ViewBuilder private var settingsPane: some View {
        switch category {
        case .vault: VaultSettings()
        case .network:
            NetworkSettings(pool: pool, joinIssue: joinIssue, open: { id in open { networkPage = id } }, join: { open { joining = true } },
                            add: client == nil ? nil : { open { adding = true } })
        case .plugins: PluginSettings(open: { name in open { pluginTarget = name } }, arrange: { open { arrangingPlugins = true } })
        default: settingsForm
        }
    }

    private var settingsForm: some View {
        Form {
            switch category {
            case .general: generalSections
            case .voice:
                if let settings {
                    VoicePane(store: settings, providers: providers)
                }
            case .models:
                ProviderSettings(client: client, settings: settings) { provider in open { providerTarget = ProviderTarget(provider: provider) } }
                if let settings { MediaSettings(store: settings) { kind in open { editingMedia = kind } } }
            case .vault, .plugins, .network: EmptyView()
            case .updates: UpdatesSettings(network: network, settings: settings)
            case .shortcuts: ShortcutsSettings()
            }
        }
        .settingsForm()
        .id(category)
    }

    @ViewBuilder private var generalSections: some View {
        Section {
            SettingsPicker("Theme", selection: Binding(get: { settings?.value?.appearance ?? "system" },
                                                      set: { settings?.edit(SettingsPatch(appearance: $0)) }),
                           options: [("Light", "light"), ("Dark", "dark"), ("System", "system")])
            .disabled(settings?.value == nil)
            if plugins?.tabs.isEmpty == false || !pool.tabs.isEmpty {
                Toggle("Switcher badges", isOn: Binding(get: { settings?.value?.switcherBadges ?? true },
                                                        set: { settings?.edit(SettingsPatch(switcherBadges: $0)) }))
                    .accessibilityLabel("Switcher badges")
                    .disabled(settings?.value == nil)
            }
        } header: {
            SectionHeader("Appearance", symbol: "circle.lefthalf.filled")
        }
        .panelRow()
        if let issue = settings?.error {
            Section { Text(issue).font(.footnote).foregroundStyle(.muted) }
                .panelRow()
        }
        #if os(macOS)
        Section {
            Toggle("Launch at login", isOn: Binding(get: { LoginItem.shared.enabled }, set: { LoginItem.shared.set($0) }))
                .accessibilityLabel("Launch at login")
            if let issue = LoginItem.shared.issue {
                Text(issue).font(.footnote).foregroundStyle(.muted)
            }
        } header: {
            SectionHeader("Startup", symbol: "power", tint: .green)
        }
        .panelRow()
        .onAppear { LoginItem.shared.refresh() }
        Section {
            Toggle("Menu bar icon", isOn: Binding(get: { StatusBar.shared.menu }, set: { StatusBar.shared.menu = $0 }))
                .accessibilityLabel("Menu bar icon")
            if NotchPanel.screen != nil {
                Toggle("Notch activity", isOn: Binding(get: { StatusBar.shared.notch }, set: { StatusBar.shared.notch = $0 }))
                    .accessibilityLabel("Notch activity")
            }
        } header: {
            SectionHeader("Menu Bar", symbol: "menubar.rectangle", tint: .indigo)
        }
        .panelRow()
        #endif
        Section {
            Toggle("Allow notifications", isOn: Binding(get: { Notifications.shared.enabled },
                set: { enabled in
                    Task {
                        await Notifications.shared.allow(enabled)
                        #if os(macOS)
                        await Permissions.shared.refresh()
                        #endif
                    }
                }))
                .accessibilityLabel("Allow notifications")
            if let issue = Notifications.shared.issue {
                Text(issue).font(.footnote).foregroundStyle(.muted)
            }
        } header: {
            SectionHeader("Notifications", symbol: "bell.fill", tint: .red)
        }
        .panelRow()
        #if os(macOS)
        permissionSection
        Section {
            Button {
                UserDefaults.standard.set(true, forKey: "onboarding.pending")
            } label: {
                HStack {
                    Text("Run Setup Again")
                    Spacer()
                    Image(systemName: "chevron.right").foregroundStyle(.muted)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Run setup again")
        } header: {
            SectionHeader("Onboarding", symbol: "sparkles")
        }
        .panelRow()
        #endif
        Section {
            ForEach(Connections.available, id: \.self) { capability in
                Toggle(Connections.title(capability), isOn: Binding(get: { Connections.shared.enabled.contains(capability) },
                    set: { on in Task { await Connections.shared.allow(capability, on) } }))
                    .accessibilityLabel(Connections.title(capability))
            }
            if let issue = Connections.shared.issue {
                Text(issue).font(.footnote).foregroundStyle(.muted)
            }
        } header: {
            SectionHeader("Device Access", symbol: "laptopcomputer.and.iphone", tint: .blue)
        }
        .panelRow()
    }

    #if os(macOS)
    private var permissionSection: some View {
        let permissions = Permissions.shared
        return Section {
            ForEach(permissions.all) { permission in
                HStack(spacing: 8) {
                    Text(permission.title)
                    Spacer()
                    if permissions.granted.contains(permission) {
                        HStack(spacing: 4) {
                            Image(systemName: "checkmark.circle.fill").accessibilityHidden(true)
                            Text("Granted").accessibilityLabel("\(permission.title): Granted")
                        }
                    } else {
                        Button("Grant") { permissions.grant(permission) }
                            .buttonStyle(.bordered)
                            .accessibilityLabel("Grant \(permission.title)")
                    }
                }
            }
        } header: {
            SectionHeader("Permissions", symbol: "hand.raised.fill")
        }
        .panelRow()
        .task(id: permissions.client?.id) { await permissions.refresh() }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            Task { await permissions.refresh() }
        }
    }
    #endif
}

/// The provider being added (nil) or edited.
private struct ProviderTarget {
    let provider: Provider?
}

enum SettingsTab: String, CaseIterable, Identifiable {
    case general, models, voice, network, vault, plugins, updates, shortcuts

    static var allCases: [SettingsTab] {
        #if os(macOS)
        [.general, .models, .voice, .network, .vault, .plugins, .shortcuts, .updates]
        #else
        [.general, .models, .voice, .network, .vault, .plugins, .updates]
        #endif
    }

    var id: String { rawValue }

    var title: String { self == .updates ? "Versions" : rawValue.capitalized }

    /// UI checks open Settings at launch: --verify-network, or its page with --verify-network-page;
    /// --verify-voice opens the Voice tab.
    static var verifying: SettingsTab? {
        #if DEBUG
        if CommandLine.arguments.contains("--verify-voice") { return .voice }
        return CommandLine.arguments.contains("--verify-network") || verifiesPage ? .network : nil
        #else
        nil
        #endif
    }

    static var verifiesPage: Bool {
        #if DEBUG
        CommandLine.arguments.contains("--verify-network-page")
        #else
        false
        #endif
    }

    /// Plugins draws its own plug, because SF Symbols has none at this angle.
    var icon: Image {
        switch self {
        case .general: Image(systemName: "gearshape.fill")
        case .voice: Image(systemName: "waveform")
        case .network: Image(systemName: "network")
        case .models: Image(systemName: "cpu.fill")
        case .vault: Image(systemName: "lock.fill")
        case .plugins: Image("plug")
        case .updates: Image(systemName: "arrow.down.circle.fill")
        case .shortcuts: Image(systemName: "keyboard")
        }
    }

    var tint: Color {
        switch self {
        case .general, .models, .updates, .shortcuts: .gray
        case .voice: .pink
        case .network, .vault: .blue
        case .plugins: .orange
        }
    }
}

#if os(macOS)
private struct SettingsKey: FocusedValueKey {
    typealias Value = Binding<Bool>
}

private struct SearchPanelKey: FocusedValueKey {
    typealias Value = Binding<Bool>
}

extension FocusedValues {
    @Entry var findInChat: (() -> Void)?
    @Entry var toggleSidebar: (() -> Void)?

    var searchPanel: Binding<Bool>? {
        get { self[SearchPanelKey.self] }
        set { self[SearchPanelKey.self] = newValue }
    }

    fileprivate var settingsPanel: Binding<Bool>? {
        get { self[SettingsKey.self] }
        set { self[SettingsKey.self] = newValue }
    }
}

private struct SettingsCommands: Commands {
    @FocusedValue(\.settingsPanel) private var settings
    @FocusedValue(\.searchPanel) private var search
    @FocusedValue(\.findInChat) private var find
    @FocusedValue(\.toggleSidebar) private var sidebar
    @State private var shortcuts = ShortcutStore.shared

    var body: some Commands {
        CommandGroup(replacing: .sidebar) {
            Button("Toggle Sidebar") { sidebar?() }
                .keyboardShortcut(shortcuts.key(.toggleSidebar).keyEquivalent,
                                  modifiers: shortcuts.key(.toggleSidebar).eventModifiers)
                .disabled(sidebar == nil)
        }
        CommandGroup(after: .textEditing) {
            Button("Search…") { search?.wrappedValue = true }
                .keyboardShortcut(shortcuts.key(.search).keyEquivalent,
                                  modifiers: shortcuts.key(.search).eventModifiers)
                .disabled(search == nil)
            Button("Find in Chat") { find?() }
                .keyboardShortcut(shortcuts.key(.find).keyEquivalent,
                                  modifiers: shortcuts.key(.find).eventModifiers)
                .disabled(find == nil)
        }
        CommandGroup(replacing: .appSettings) {
            Button("Settings…") { settings?.wrappedValue = true }
                .keyboardShortcut(shortcuts.key(.settings).keyEquivalent,
                                  modifiers: shortcuts.key(.settings).eventModifiers)
                .disabled(settings == nil)
        }
    }
}
#endif
