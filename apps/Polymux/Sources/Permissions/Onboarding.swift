#if os(macOS)
import SwiftUI
import PolymuxKit

/// The first launch's setup. It fills the window: a step list on the left, and the current step on the right.
struct Onboarding: View {
    /// UI checks open it at launch with --verify-onboarding.
    static var verifying: Bool {
        #if DEBUG
        CommandLine.arguments.contains("--verify-onboarding")
        #else
        false
        #endif
    }

    let pool: SessionPool
    let join: (URL) async throws -> Void
    let finish: () -> Void

    @State private var step = Step.welcome
    @State private var host = Host.local
    @State private var link = ""
    @State private var joining = false
    @State private var joined = false
    @State private var joinIssue: String?
    @AppStorage("serverAddress") private var address = ServerHost.address
    @Environment(PluginStore.self) private var plugins: PluginStore?
    @Environment(\.colorScheme) private var scheme
    private let permissions = Permissions.shared

    private enum Step: Int, CaseIterable, Hashable {
        case welcome, host, permissions, plugins

        var title: String {
            switch self {
            case .welcome: "Welcome"
            case .host: "Where Bots Run"
            case .permissions: "Permissions"
            case .plugins: "Plugins"
            }
        }

        var heading: String {
            switch self {
            case .welcome: "Get Started with Polymux"
            case .host: "Choose Where Bots Run"
            case .permissions: "System Permissions"
            case .plugins: "Official Plugins"
            }
        }

        var symbol: String {
            switch self {
            case .welcome: "sparkles"
            case .host: "server.rack"
            case .permissions: "hand.raised.fill"
            case .plugins: "puzzlepiece.extension.fill"
            }
        }

        var tint: Color {
            switch self {
            case .welcome: .blue
            case .host: .blue
            case .permissions: .blue
            case .plugins: .orange
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
        }
        .background(PanelSurface.color(scheme))
        .ignoresSafeArea(edges: .top)
        .tint(.blue)
        .task { await permissions.refresh() }
        .onAppear {
            if !ServerHost.shared.available {
                host = .remote
            } else if address != ServerHost.address {
                host = .remote
                joined = true
            }
        }
    }

    private var content: some View {
        VStack(spacing: 18) {
            pageHeader
            pageBody
            if step == .welcome { welcomeActions } else { navigationActions }
        }
        .frame(maxWidth: 700)
        .padding(.horizontal, 40)
        .padding(.vertical, 20)
        .frame(maxWidth: .infinity)
    }

    private var pageHeader: some View {
        VStack(spacing: 14) {
            if step == .welcome {
                AppIcon().frame(width: 160, height: 160)
            } else {
                hero(step.symbol, tint: step.tint)
            }
            Text(step.heading)
                .font(step == .welcome ? .largeTitle.bold() : .title.bold())
                .multilineTextAlignment(.center)
            Text(detail)
                .font(.body)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
    }

    private var detail: String {
        switch step {
        case .welcome:
            "Choose where bots run, grant them access, and add what you need."
        case .host:
            "Run the server on this device, or connect to one that's already running."
        case .permissions:
            "Bots inherit what Polymux Server is granted."
        case .plugins:
            "Add a chat hub, permission rules, and remote access."
        }
    }

    @ViewBuilder private var pageBody: some View {
        switch step {
        case .welcome: EmptyView()
        case .host: hostPage
        case .permissions: permissionsPage
        case .plugins: pluginsPage
        }
    }

    private var welcomeActions: some View {
        VStack(spacing: 12) {
            Button("Continue", action: advance)
                .buttonStyle(OnboardingPrimaryButton())
                .keyboardShortcut(.defaultAction)
            Button("Skip for now", action: finish)
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
        }
        .padding(.top, 4)
    }

    private var navigationActions: some View {
        HStack(spacing: 12) {
            Button("Back", action: back)
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
            Spacer(minLength: 0)
            Button(step == .plugins ? "Done" : "Continue", action: advance)
                .buttonStyle(OnboardingPrimaryButton())
                .keyboardShortcut(.defaultAction)
                .disabled(joining)
        }
        .padding(.top, 4)
    }

    private var hostPage: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(spacing: 0) {
                if ServerHost.shared.available {
                    hostOption(.local, title: "On this device",
                               detail: ServerHost.shared.running ? "This device is running the server." : "Starts a server and its bots here.",
                               symbol: "laptopcomputer", tint: .blue)
                    Divider().padding(.horizontal, 12)
                }
                hostOption(.remote, title: "On another device",
                           detail: joined ? "Connected." : "Connect to a server that's already running.",
                           symbol: "server.rack", tint: .purple)
            }
            .background(PanelSurface.card(scheme), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(.separator))
            if host == .remote {
                pairing
                    .padding(.top, 16)
            }
        }
    }

    private func hostOption(_ choice: Host, title: String, detail: String, symbol: String, tint: Color) -> some View {
        Button {
            host = choice
        } label: {
            HStack(spacing: 13) {
                Image(systemName: symbol)
                    .font(.system(size: 17, weight: .light))
                    .foregroundStyle(tint)
                    .frame(width: 34, height: 34)
                    .background(.quinary, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                VStack(alignment: .leading, spacing: 3) {
                    Text(title).font(.headline)
                    Text(detail).font(.subheadline).foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Image(systemName: host == choice ? "checkmark.circle.fill" : "circle")
                    .font(.title3)
                    .foregroundStyle(host == choice ? Color.blue : Color.secondary)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 12)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityValue(host == choice ? "Selected" : "")
        .accessibilityHint(detail)
    }

    private var pairing: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                TextField("Pairing link", text: $link, prompt: Text("Paste a pairing link"))
                    .labelsHidden()
                    .lineLimit(1)
                    .autocorrectionDisabled()
                    .disabled(joining)
                    .onSubmit(connect)
                Button("Connect", action: connect)
                    .tint(.blue)
                    .disabled(joining || link.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
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
                Text("On the other device, open Settings › Network › Add device.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
    }

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
            } catch is CancellationError {
            } catch {
                joinIssue = error.isOffline ? "Can’t reach this server. Check its connection and try the link again."
                                            : error.reason(or: "Couldn’t pair with this server.")
            }
        }
    }

    private var permissionsPage: some View {
        VStack(spacing: 0) {
            ForEach(Array(permissions.all.enumerated()), id: \.element) { index, permission in
                if index > 0 { Divider().padding(.horizontal, 12) }
                permissionRow(permission)
            }
        }
        .background(PanelSurface.card(scheme), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(.separator))
        .task {
            while !Task.isCancelled {
                await permissions.refresh()
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    private func permissionRow(_ permission: Permission) -> some View {
        HStack(spacing: 13) {
            Image(systemName: permission.symbol)
                .font(.system(size: 15, weight: .light))
                .foregroundStyle(permission.tint)
                .frame(width: 34, height: 34)
                .background(.quinary, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            VStack(alignment: .leading, spacing: 3) {
                Text(permission.title).font(.headline)
                Text(permission.detail).font(.subheadline).foregroundStyle(.secondary)
                    .lineLimit(2, reservesSpace: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
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
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
    }

    private var pluginsPage: some View {
        Group {
            if let plugins {
                OnboardingPlugins(store: plugins)
            } else {
                Text("Connect to a server to add plugins.")
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 40)
            }
        }
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
        if let next = Step(rawValue: step.rawValue + 1) {
            step = next
        } else {
            finish()
        }
    }

    private func back() {
        if let previous = Step(rawValue: step.rawValue - 1) {
            step = previous
        }
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
    @State private var confirming: MarketPlugin?
    @Environment(\.colorScheme) private var scheme

    private var plugins: [MarketPlugin] {
        (store.market ?? []).filter { $0.author == "Polymux" }
    }

    private let columns = [GridItem(.adaptive(minimum: 200, maximum: 240), spacing: 14, alignment: .top)]

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
        .alert("Install \(confirming?.title ?? "plugin")?", isPresented: Binding(
            get: { confirming != nil }, set: { if !$0 { confirming = nil } }
        ), presenting: confirming) { item in
            Button("Install") { store.install(item) }
            Button("Cancel", role: .cancel) {}
        } message: { item in
            Text("Version \(item.version) from \(item.source). It runs on your server.")
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
            Button(version == nil ? "Install" : "Update") { confirming = item }
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

#endif
