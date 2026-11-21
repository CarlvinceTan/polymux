import SwiftUI
import PolymuxKit

/// The first launch's setup. It fills the window on a Mac and the whole screen on iPhone.
struct Onboarding: View {
    /// UI checks open it at launch with --verify-onboarding.
    static var verifying: Bool {
        #if DEBUG
        CommandLine.arguments.contains("--verify-onboarding")
        #else
        false
        #endif
    }

    /// UI checks can open a later step with --onboarding-step=models.
    private static var firstStep: Step {
        #if DEBUG
        let value = CommandLine.arguments.first { $0.hasPrefix("--onboarding-step=") }?.split(separator: "=").last
        if let step = Step.allCases.first(where: { "\($0)" == value.map(String.init) }) { return step }
        #endif
        #if os(macOS)
        return .welcome
        #else
        return .host
        #endif
    }

    let pool: SessionPool
    let join: (URL) async throws -> Void
    var incoming: Binding<URL?>? = nil
    let finish: () -> Void

    @State private var step = Onboarding.firstStep
    @State private var host = Host.local
    @State private var link = ""
    @State private var joining = false
    @State private var joined = false
    @State private var joinIssue: String?
    /// Connecting a provider inside the model step; nil shows the step itself.
    @State private var providerPage: SetupProviderPage?
    @State private var choosingModel = false
    #if os(iOS)
    @State private var scanning = false
    /// Set once pairing shows the server has no provider, so the model step follows.
    @State private var needsModel = false
    #endif
    @AppStorage("serverAddress") private var address = ServerHost.address
    @Environment(\.colorScheme) private var scheme
    #if os(iOS)
    @Environment(\.dynamicTypeSize) private var typeSize
    #endif
    #if os(macOS)
    private let permissions = Permissions.shared
    #endif

    private var session: AppSession? { pool.sessions[address] }
    private var providers: [Provider] { session?.providers.providers ?? [] }
    private var groups: [ProviderGroup] { ProviderGroup.collect(providers, names: session?.providers.names ?? [:]) }

    /// A Mac sets up its own server; an iPhone only pairs, and adds a model when the server has none.
    private var steps: [Step] {
        #if os(macOS)
        Step.allCases.filter { step in
            guard host == .remote, joined else { return true }
            // A server that's already running has its own permissions, and usually a model.
            return step != .permissions && (step != .models || providers.isEmpty)
        }
        #else
        needsModel || step == .models ? [.host, .models] : [.host]
        #endif
    }

    private enum Step: Int, CaseIterable, Hashable {
        case welcome, host, models, permissions, plugins, ready

        var heading: String {
            switch self {
            case .welcome: "Get Started with Polymux"
            case .host:
                #if os(macOS)
                "Choose Where Bots Run"
                #else
                "Connect to Your Server"
                #endif
            case .models: "Connect a Model"
            case .permissions: "System Permissions"
            case .plugins: "Official Plugins"
            case .ready: "You’re All Set"
            }
        }

        var symbol: String {
            switch self {
            case .welcome: "sparkles"
            case .host: "server.rack"
            case .models: "cpu.fill"
            case .permissions: "hand.raised.fill"
            case .plugins: "puzzlepiece.extension.fill"
            case .ready: "checkmark"
            }
        }

        var tint: Color {
            switch self {
            case .models: .purple
            case .plugins: .orange
            case .ready: .green
            default: .blue
            }
        }
    }

    private enum Host {
        case local, remote
    }

    var body: some View {
        VStack(spacing: 0) {
            GeometryReader { proxy in
                ScrollView {
                    content
                        .frame(minHeight: proxy.size.height, alignment: .center)
                }
                .scrollIndicators(.hidden)
            }
            // The next step stays in reach however long the page is.
            Group {
                if step == .welcome { welcomeActions } else { navigationActions }
            }
            .frame(maxWidth: 700)
            .padding(.horizontal, horizontalInset)
            .padding(.vertical, 16)
            .frame(maxWidth: .infinity)
        }
        .background(PanelSurface.color(scheme).ignoresSafeArea())
        .tint(.blue)
        .environment(session?.providers)
        .onChange(of: step) { providerPage = nil }
        #if os(iOS)
        .dropdownHost()
        #endif
        #if os(macOS)
        .task { await permissions.refresh() }
        #endif
        .onAppear {
            if !ServerHost.shared.available {
                host = .remote
            } else if address != ServerHost.address {
                host = .remote
                joined = true
            }
        }
        #if os(iOS)
        .onChange(of: incoming?.wrappedValue, initial: true) { _, url in
            guard let url else { return }
            incoming?.wrappedValue = nil
            acceptLink(url)
        }
        .fullScreenCover(isPresented: $scanning) {
            QRScanner { url in
                scanning = false
                acceptLink(url)
            }
        }
        #endif
    }

    private var horizontalInset: CGFloat {
        #if os(macOS)
        40
        #else
        24
        #endif
    }

    private var content: some View {
        VStack(spacing: 18) {
            pageHeader
            pageBody
        }
        .frame(maxWidth: 700)
        .padding(.horizontal, horizontalInset)
        .padding(.vertical, 20)
        .frame(maxWidth: .infinity)
    }

    private var pageHeader: some View {
        VStack(spacing: 14) {
            if step == .welcome {
                SetupIsland().frame(maxHeight: 340)
            } else if showsIcon {
                AppIcon().frame(width: iconSize, height: iconSize)
            } else {
                hero(step.symbol, tint: step.tint)
            }
            Text(step.heading)
                .font(showsIcon ? .largeTitle.bold() : .title.bold())
                .multilineTextAlignment(.center)
            Text(detail)
                .font(.body)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity)
    }

    /// The first page carries the app icon: Welcome on a Mac, pairing on iPhone.
    private var showsIcon: Bool { step == steps.first && step != .models }

    private var iconSize: CGFloat {
        #if os(macOS)
        160
        #else
        120
        #endif
    }

    private var detail: String {
        switch step {
        case .welcome:
            "Choose where bots run, connect a model, and add what you need."
        case .host:
            #if os(macOS)
            "Run the server on this device, or connect to one that's already running."
            #else
            "Bots run on your server. Pair this iPhone with it to chat from anywhere."
            #endif
        case .models:
            "Bots need a model to reply. Sign in to a provider or add an API key."
        case .permissions:
            "Bots inherit what Polymux Server is granted."
        case .plugins:
            "Add a chat hub, permission rules, and remote access."
        case .ready:
            #if os(macOS)
            "Start a chat with a bot, or pair your iPhone to chat from anywhere."
            #else
            "Start a chat with a bot."
            #endif
        }
    }

    @ViewBuilder private var pageBody: some View {
        switch step {
        case .welcome: EmptyView()
        case .host: hostPage
        case .models: modelsPage
        case .permissions:
            #if os(macOS)
            permissionsPage
            #endif
        case .plugins: pluginsPage
        case .ready: readyPage
        }
    }

    private var welcomeActions: some View {
        HStack(spacing: 12) {
            Button("Skip for now", action: finish)
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
            Spacer(minLength: 0)
            Button("Continue", action: advance)
                .buttonStyle(OnboardingPrimaryButton())
                .keyboardShortcut(.defaultAction)
        }
    }

    private var navigationActions: some View {
        HStack(spacing: 12) {
            if step == steps.first {
                Button("Skip for now", action: finish)
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
            } else {
                Button("Back", action: back)
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            if steps.count > 1, let index = steps.firstIndex(of: step) {
                SetupProgress(count: steps.count, index: index)
                Spacer(minLength: 0)
            }
            if step == .models && providers.isEmpty && providerPage == nil {
                Button("Later", action: advance)
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                    .accessibilityLabel("Set up a model later")
            }
            // Connecting a provider has its own actions on the page.
            if !needsPairing && !(step == .models && providerPage != nil) {
                Button(primaryTitle, action: advance)
                    .buttonStyle(OnboardingPrimaryButton())
                    .keyboardShortcut(.defaultAction)
                    .disabled(joining || (step == .models && providers.isEmpty))
            }
        }
    }

    private var primaryTitle: String {
        if step == .ready { return "Start" }
        return step == steps.last ? "Done" : "Continue"
    }

    private var needsPairing: Bool {
        #if os(iOS)
        step == .host && !joined
        #else
        false
        #endif
    }

    private var hostPage: some View {
        #if os(macOS)
        hostChoice
        #else
        // iPhone is only ever a client, so there is nothing to choose.
        pairing
        #endif
    }

    private var hostChoice: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 14) {
                if ServerHost.shared.available {
                    hostOption(.local, title: "On this Mac",
                               detail: ServerHost.shared.running ? "This Mac is running the server." : "Starts a server and its bots here.",
                               tint: .blue) { SetupMock.local(.blue) }
                }
                hostOption(.remote, title: "On another device",
                           detail: joined ? "Connected." : "Connect to a server that's already running.",
                           tint: .purple) { SetupMock.remote(.purple) }
            }
            if host == .remote {
                pairing
                    .padding(.top, 16)
            }
        }
    }

    private func hostOption<Mock: View>(_ choice: Host, title: String, detail: String, tint: Color,
                                        @ViewBuilder mock: @escaping () -> Mock) -> some View {
        let selected = host == choice
        return Button {
            host = choice
        } label: {
            VStack(alignment: .leading, spacing: 0) {
                SetupBanner(tint: tint, width: 150, height: 72, mock: mock)
                    .frame(height: 104)
                HStack(alignment: .top, spacing: 10) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(title).font(.headline)
                        Text(detail).font(.subheadline).foregroundStyle(.secondary)
                            .lineLimit(2, reservesSpace: true)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                        .font(.title3)
                        .foregroundStyle(selected ? tint : Color.secondary)
                }
                .padding(12)
            }
            .background(PanelSurface.card(scheme), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(selected ? AnyShapeStyle(tint) : AnyShapeStyle(.separator), lineWidth: selected ? 2 : 1))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityValue(selected ? "Selected" : "")
        .accessibilityHint(detail)
    }

    private var pairing: some View {
        VStack(alignment: .leading, spacing: 8) {
            #if os(iOS)
            if !joined {
                phonePairing
            }
            #else
            linkEntry
            #endif
            if joining {
                ProgressView().controlSize(.small)
            } else if joined {
                Label("Connected", systemImage: "checkmark.circle.fill")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            if let joinIssue {
                Text(joinIssue).font(.footnote).foregroundStyle(.secondary)
            } else if !joined {
                Text("On the device running your server, open Settings › Network › Add device.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var linkEntry: some View {
        #if os(iOS)
        let layout = typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
            : AnyLayout(HStackLayout(spacing: 8))
        #else
        let layout = HStackLayout(spacing: 8)
        #endif
        return layout {
            TextField("Pairing link", text: $link, prompt: Text("Paste a pairing link"))
                .labelsHidden()
                .lineLimit(1)
                .autocorrectionDisabled()
                #if os(iOS)
                .textInputAutocapitalization(.never)
                .keyboardType(.URL)
                .textFieldStyle(.roundedBorder)
                .controlSize(.large)
                #endif
                .disabled(joining)
                .onSubmit(connect)
            Button("Connect", action: connect)
                .tint(.blue)
                #if os(iOS)
                .buttonStyle(.bordered)
                .controlSize(.large)
                #endif
                .disabled(joining || link.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
    }

    #if os(iOS)
    private var phonePairing: some View {
        VStack(alignment: .leading, spacing: 12) {
            if QRScanner.available || Self.verifying {
                Button {
                    Keyboard.close()
                    scanning = true
                } label: {
                    Label("Scan QR code", systemImage: "qrcode.viewfinder")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .disabled(joining)
                HStack(spacing: 12) {
                    Rectangle().fill(.separator).frame(height: 1)
                    Text("or paste a link").font(.subheadline).foregroundStyle(.secondary)
                        .layoutPriority(1)
                    Rectangle().fill(.separator).frame(height: 1)
                }
            }
            linkEntry
        }
    }

    private func acceptLink(_ url: URL) {
        guard !joining else { return }
        step = .host
        link = url.absoluteString
        connect()
    }

    /// A paired iPhone is ready at once, unless its server still needs a model.
    private func settle() async {
        for _ in 0..<40 where session == nil {
            try? await Task.sleep(for: .milliseconds(250))
        }
        guard let store = session?.providers else { return finish() }
        await store.refreshProviders(force: true)
        if store.providers?.isEmpty == true {
            needsModel = true
            step = .models
        } else {
            finish()
        }
    }
    #endif

    private func connect() {
        guard !joining else { return }
        guard let url = URL(string: link.trimmingCharacters(in: .whitespacesAndNewlines)), Invite(link: url) != nil else {
            joinIssue = "Paste a valid Polymux pairing link."
            return
        }
        joinIssue = nil
        joining = true
        Task {
            defer { joining = false }
            do {
                try await join(url)
                joined = true
                link = ""
                #if os(iOS)
                await settle()
                #else
                for _ in 0..<40 where session == nil { try? await Task.sleep(for: .milliseconds(250)) }
                await session?.providers.refreshProviders(force: true)
                #endif
            } catch is CancellationError {
            } catch {
                joinIssue = error.isOffline ? "Can’t reach this server. Check its connection and try the link again."
                                            : error.reason(or: "Couldn’t pair with this server.")
            }
        }
    }

    @ViewBuilder private var modelsPage: some View {
        if let page = providerPage, let client = session?.client {
            switch page {
            case .choose:
                SetupProviderPicker(options: session?.providers.options, choose: { providerPage = .signIn($0) },
                                    local: { providerPage = .local })
                    .task { await session?.providers.refreshOptions() }
            case .signIn(let option):
                SetupSignIn(client: client, option: option, connected: connected).id(option)
            case .local:
                SetupLocal(client: client, connected: connected)
            }
        } else {
            modelsOverview
        }
    }

    /// A provider finished connecting: list it and return to the step.
    private func connected(_ provider: Provider) {
        session?.providers.merge([provider])
        Task { await session?.settings.refresh() }
        providerPage = nil
    }

    private var modelsOverview: some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(spacing: 0) {
                ForEach(groups) { group in
                    OnboardingRow(title: group.name, detail: group.summary, symbol: "cloud.fill", tint: .blue) {
                        Image(systemName: "checkmark.circle.fill").font(.title3).foregroundStyle(.green)
                            .accessibilityLabel("Connected")
                    }
                    Divider().padding(.horizontal, 12)
                }
                Button { providerPage = .choose } label: {
                    OnboardingRow(title: groups.isEmpty ? "Add a provider" : "Add another provider",
                                  detail: "OpenAI, Anthropic, Gemini, Ollama and more.",
                                  symbol: "plus", tint: .purple) {
                        Image(systemName: "chevron.right").foregroundStyle(.secondary)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(session == nil)
                .accessibilityLabel(groups.isEmpty ? "Add a provider" : "Add another provider")
            }
            .onboardingCard(scheme)
            if session == nil {
                ProgressView().controlSize(.small).frame(maxWidth: .infinity)
                    .accessibilityLabel("Starting server")
            } else if !providers.isEmpty {
                defaultModel
            }
            if let issue = session?.providers.issue {
                Text(issue).font(.footnote).foregroundStyle(.secondary)
            }
        }
        .task(id: session?.client.id) {
            guard let store = session?.providers else { return }
            await store.refresh()
            // A sign-in finishes on the server, so pick it up without a manual refresh.
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(2))
                await store.refreshProviders()
            }
        }
    }

    private var defaultModel: some View {
        Button { choosingModel = true } label: {
            OnboardingRow(title: "Default model", detail: "New bots start on this model.",
                          symbol: "sparkles", tint: .purple) {
                HStack(spacing: 6) {
                    if model.model.isEmpty {
                        Text("Server default").foregroundStyle(.secondary)
                    } else {
                        ModelLabel(name: model)
                    }
                    Image(systemName: "chevron.up.chevron.down").foregroundStyle(.secondary)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Default model")
        .accessibilityValue(model.model.isEmpty ? "Server default" : model.title)
        .onboardingCard(scheme)
        .dropdown(isPresented: $choosingModel, width: .fixed(DropdownStyle.modelWidth)) {
            ModelPicker(providers: providers, provider: session?.settings.value?.defaultProvider ?? "",
                        model: session?.settings.value?.defaultModel ?? "", clearable: false) { provider, model in
                session?.settings.edit(SettingsPatch(defaultProvider: provider, defaultModel: model))
                choosingModel = false
            }
            .environment(session?.providers)
            .preferredColorScheme(scheme)
        }
        .disabled(session?.settings.value == nil)
    }

    /// The default model as Settings names it.
    private var model: ModelName {
        guard let value = session?.settings.value, !value.defaultModel.isEmpty, !value.defaultProvider.isEmpty else {
            return ModelName(model: "")
        }
        let name = groups.first { $0.connections.contains { $0.id == value.defaultProvider } }?.name ?? value.defaultProvider
        return ModelName(model: value.defaultModel, provider: name)
    }

    #if os(macOS)
    private var permissionsPage: some View {
        VStack(spacing: 0) {
            ForEach(Array(permissions.all.enumerated()), id: \.element) { index, permission in
                if index > 0 { Divider().padding(.horizontal, 12) }
                permissionRow(permission)
            }
        }
        .onboardingCard(scheme)
        .task {
            while !Task.isCancelled {
                await permissions.refresh()
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    private func permissionRow(_ permission: Permission) -> some View {
        OnboardingRow(title: permission.title, detail: permission.detail, symbol: permission.symbol,
                      tint: permission.tint, reservesLines: true) {
            if permissions.granted.contains(permission) {
                Label("Granted", systemImage: "checkmark.circle.fill")
                    .foregroundStyle(.green)
            } else {
                Button("Grant") { permissions.grant(permission) }
                    .buttonStyle(.bordered)
                    .buttonBorderShape(.capsule)
                    .tint(.blue)
                    .accessibilityLabel("Grant \(permission.title)")
            }
        }
    }
    #endif

    private var pluginsPage: some View {
        Group {
            if let plugins = session?.plugins {
                OnboardingPlugins(store: plugins)
            } else {
                Text("Connect to a server to add plugins.")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 40)
            }
        }
    }

    private var readyPage: some View {
        VStack(spacing: 14) {
            VStack(spacing: 0) {
                OnboardingRow(title: "Server", detail: serverName, symbol: "server.rack", tint: .blue) {
                    status(session != nil)
                }
                Divider().padding(.horizontal, 12)
                Button { step = .models } label: {
                    OnboardingRow(title: "Model", detail: modelSummary, symbol: "cpu.fill", tint: .purple) {
                        status(!providers.isEmpty)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Model")
                .accessibilityValue(modelSummary)
            }
            .onboardingCard(scheme)
            #if os(macOS)
            VStack(spacing: 14) {
                VStack(spacing: 4) {
                    Label("Pair your iPhone", systemImage: "iphone")
                        .font(.headline)
                    Text("Open Polymux on your iPhone and scan this code, or send it the link.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
                InviteCode(client: session?.client, size: 132, beside: true)
            }
            .frame(maxWidth: .infinity)
            .padding(16)
            .onboardingCard(scheme)
            #endif
        }
    }

    private var serverName: String {
        if address == ServerHost.address { return "This Mac" }
        return session?.network.value?.server.name ?? "Connected"
    }

    private var modelSummary: String {
        if providers.isEmpty { return "Not set. Add one in Settings › Models." }
        return model.model.isEmpty ? "Server default" : model.title
    }

    private func status(_ ready: Bool) -> some View {
        Image(systemName: ready ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
            .font(.title3)
            .foregroundStyle(ready ? Color.green : Color.orange)
            .accessibilityLabel(ready ? "Ready" : "Not set")
    }

    private func hero(_ symbol: String, tint: Color) -> some View {
        Image(systemName: symbol)
            .font(.system(size: 30, weight: .semibold))
            .foregroundStyle(.white)
            .frame(width: 56, height: 56)
            .background(tint, in: RoundedRectangle(cornerRadius: 13, style: .continuous))
            .accessibilityHidden(true)
    }

    private func advance() {
        if step == .host, host == .local {
            #if DEBUG
            if !VerifyFixture.enabled {
                Task {
                    if !ServerHost.shared.running { await pool.host(true) }
                }
            }
            #else
            Task {
                if !ServerHost.shared.running { await pool.host(true) }
            }
            #endif
        }
        if let index = steps.firstIndex(of: step), index + 1 < steps.count {
            step = steps[index + 1]
        } else {
            finish()
        }
    }

    private func back() {
        if step == .models, let page = providerPage {
            providerPage = page == .choose ? nil : .choose
            return
        }
        if let index = steps.firstIndex(of: step), index > 0 {
            step = steps[index - 1]
        }
    }
}

/// One line of a setup card: an icon, a title and detail, and its control.
struct OnboardingRow<Trailing: View>: View {
    let title: String
    let detail: String
    let symbol: String
    let tint: Color
    var reservesLines = false
    @ViewBuilder let trailing: () -> Trailing

    var body: some View {
        HStack(spacing: 13) {
            Image(systemName: symbol)
                .font(.system(size: 15, weight: .light))
                .foregroundStyle(tint)
                .frame(width: 34, height: 34)
                .background(.quinary, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(.headline)
                Text(detail).font(.subheadline).foregroundStyle(.secondary)
                    .lineLimit(2, reservesSpace: reservesLines)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            trailing()
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
    }
}

extension View {
    func onboardingCard(_ scheme: ColorScheme) -> some View {
        background(PanelSurface.card(scheme), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(.separator))
    }
}

/// The primary action in setup, drawn blue so it reads as the clear next step.
private struct OnboardingPrimaryButton: ButtonStyle {
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(enabled ? Color.white : Color.secondary)
            .padding(.horizontal, 24)
            .padding(.vertical, 10)
            .background(enabled ? Color.blue.opacity(configuration.isPressed ? 0.82 : 1) : Color.primary.opacity(0.1),
                        in: Capsule())
    }
}

/// The official plugins, shown as a visual grid.
private struct OnboardingPlugins: View {
    let store: PluginStore
    @Environment(\.colorScheme) private var scheme

    private var plugins: [MarketPlugin] {
        (store.market ?? []).filter { $0.author == "Polymux" }
    }

    #if os(macOS)
    private let columns = [GridItem(.adaptive(minimum: 200, maximum: 240), spacing: 14, alignment: .top)]
    #else
    private let columns = [GridItem(.flexible(), alignment: .top)]
    #endif

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if plugins.isEmpty {
                if let issue = store.marketIssue {
                    Text(issue).foregroundStyle(.secondary)
                } else {
                    ProgressView().controlSize(.small)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 40)
                }
            } else {
                LazyVGrid(columns: columns, alignment: .leading, spacing: 14) {
                    ForEach(plugins, id: \.name) { item in
                        card(item)
                    }
                }
            }
            if let issue = store.issue {
                Text(issue).font(.footnote).foregroundStyle(.secondary)
            }
        }
        .task {
            await store.loadMarket()
            await store.refresh()
            while !Task.isCancelled {
                await store.refresh()
                try? await Task.sleep(for: .seconds(2))
            }
        }
    }

    private func card(_ item: MarketPlugin) -> some View {
        let plugin = store.plugin(named: item.name)
        return VStack(alignment: .leading, spacing: 0) {
            PluginBanner(name: item.name, symbol: item.symbol)
                .frame(height: 104)
            VStack(alignment: .leading, spacing: 7) {
                Text(item.title).font(.headline).lineLimit(1)
                Text(item.description)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(3, reservesSpace: true)
                HStack(alignment: .center, spacing: 8) {
                    Text("Official · \(item.version)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    action(item, plugin: plugin)
                }
            }
            .padding(12)
        }
        .background(PanelSurface.card(scheme), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(.separator))
    }

    private func installed(_ item: MarketPlugin) -> String? {
        store.plugin(named: item.name)?.version ?? item.installed
    }

    @ViewBuilder private func action(_ item: MarketPlugin, plugin: Plugin?) -> some View {
        let version = installed(item)
        if plugin?.state == PluginState.installing {
            ProgressView().controlSize(.small).accessibilityLabel("Installing \(item.title)")
        } else if !item.surface.isEmpty && !store.surfaces.contains(item.surface) {
            Text("Needs update").font(.subheadline).foregroundStyle(.secondary)
        } else if !item.compatible {
            Text("Unavailable").font(.subheadline).foregroundStyle(.secondary)
        } else if version == item.version {
            Label("Installed", systemImage: "checkmark.circle.fill")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        } else {
            Button(version == nil ? "Install" : "Update") { store.install(item) }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
                .accessibilityLabel("\(version == nil ? "Install" : "Update") \(item.title)")
        }
    }
}

/// A small mock of the plugin's screen, so its job reads before the description does.
private struct PluginBanner: View {
    let name: String
    let symbol: String
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        ZStack {
            tint.opacity(scheme == .dark ? 0.22 : 0.12)
            mock
                .frame(width: 180, height: 86)
                .background(PanelSurface.color(scheme), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(.separator))
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityHidden(true)
    }

    @ViewBuilder private var mock: some View {
        switch name {
        case "hub": hubMock
        case "permissions": permissionsMock
        case "tailscale": tailscaleMock
        default: genericMock
        }
    }

    private var hubMock: some View {
        HStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 7) {
                ForEach(0..<4) { index in
                    Capsule()
                        .fill(tint.opacity(0.18))
                        .frame(width: index == 0 ? 42 : 32, height: 6)
                }
                Spacer(minLength: 0)
            }
            .padding(10)
            .frame(width: 64, alignment: .topLeading)
            Rectangle()
                .fill(.primary.opacity(0.08))
                .frame(width: 1)
            VStack(alignment: .leading, spacing: 6) {
                bubble(width: 62, alignment: .leading)
                bubble(width: 82, alignment: .trailing)
                bubble(width: 50, alignment: .leading)
                Spacer(minLength: 0)
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .topLeading)
        }
    }

    private func bubble(width: CGFloat, alignment: Alignment) -> some View {
        Capsule()
            .fill(tint.opacity(0.26))
            .frame(width: width, height: 14)
            .frame(maxWidth: .infinity, alignment: alignment)
    }

    private var permissionsMock: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Image(systemName: "lock.shield.fill")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(tint)
                Capsule().fill(tint.opacity(0.24)).frame(width: 62, height: 6)
                Spacer(minLength: 0)
            }
            ForEach(0..<3) { index in
                HStack(spacing: 6) {
                    Circle().fill(tint.opacity(0.3)).frame(width: 5, height: 5)
                    Capsule().fill(.secondary.opacity(0.16))
                        .frame(width: index == 0 ? 76 : 54, height: 5)
                    Spacer(minLength: 0)
                    Capsule().fill(tint.opacity(0.22)).frame(width: 24, height: 10)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(12)
    }

    private var tailscaleMock: some View {
        ZStack {
            Image(systemName: "globe")
                .font(.system(size: 66, weight: .ultraLight))
                .foregroundStyle(tint.opacity(0.14))
            HStack(spacing: 0) {
                node
                Rectangle().fill(tint.opacity(0.35)).frame(height: 1)
                node
                Rectangle().fill(tint.opacity(0.35)).frame(height: 1)
                node
            }
            .padding(.horizontal, 28)
        }
    }

    private var node: some View {
        Circle()
            .fill(tint)
            .frame(width: 10, height: 10)
            .overlay(Circle().strokeBorder(tint.opacity(0.25), lineWidth: 6))
    }

    private var genericMock: some View {
        Image(plugin: symbol.isEmpty ? PluginBrand.symbol(name: name) ?? "puzzlepiece.extension.fill" : symbol)
            .font(.system(size: 42, weight: .semibold))
            .foregroundStyle(tint)
    }

    private var tint: Color {
        switch name {
        case "hub": .purple
        case "permissions": .green
        case "tailscale": .blue
        default: .accentColor
        }
    }
}
