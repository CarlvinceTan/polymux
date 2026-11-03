import SwiftUI
import PolymuxKit

/// One network: everything centres on the row, and detail steps back.
private struct NetworkRow: View {
    let server: ServerDevice
    let store: NetworkStore?
    var action: (() -> Void)? = nil

    private var local: Bool { server.address == ServerHost.address }
    private var place: String { local ? "This Mac" : store?.value?.server.name ?? server.name }

    private var state: StatusDot.State {
        guard let store, store.hasChecked else { return .checking }
        return store.isConnected ? .online : .offline
    }

    private var count: String? {
        store?.value.map { $0.devices.count == 1 ? "1 device" : "\($0.devices.count) devices" }
    }

    private var content: some View {
        HStack(spacing: PanelStyle.headingInset) {
            Image(systemName: local ? "laptopcomputer" : "server.rack")
                .font(.title3)
                .foregroundStyle(.muted)
                .frame(width: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(server.title).lineLimit(1)
                HStack(spacing: 6) {
                    StatusDot(state: state)
                    Text(place).font(.subheadline).foregroundStyle(.muted).lineLimit(1)
                }
            }
            .layoutPriority(1)
            Spacer(minLength: PanelStyle.rowInset)
            if let count { Text(count).foregroundStyle(.muted).lineLimit(1) }
            if action != nil { Image(systemName: "chevron.right").foregroundStyle(.muted) }
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
    }

    var body: some View {
        if let action {
            Button(action: action) { content }
                .buttonStyle(.plain)
                .accessibilityLabel(server.title)
                .accessibilityValue([place, count].compactMap { $0 }.joined(separator: ", "))
        } else {
            content
        }
    }
}

/// Settings' Network tab: the networks this device is on, and this Mac's own server.
struct NetworkSettings: View {
    let pool: SessionPool
    let joinIssue: String?
    let open: (String) -> Void
    let join: () -> Void
    @State private var deletingCopy = false

    private var networks: [ServerDevice] { pool.servers.devices.filter { $0.denied != true } }

    var body: some View {
        Form {
            Section {
                if networks.isEmpty {
                    Text("No networks yet.").foregroundStyle(.muted).frame(maxWidth: .infinity)
                }
                ForEach(networks) { server in
                    NetworkRow(server: server, store: pool.sessions[server.address]?.network) { open(server.address) }
                }
            } header: {
                HStack {
                    Text("Networks").foregroundStyle(Color.primary)
                    Spacer()
                    Button("Join network", systemImage: "plus", action: join)
                }
                .labelStyle(.iconOnly)
                .buttonStyle(.borderless)
            } footer: {
                if let joinIssue { Text(joinIssue).font(.footnote).foregroundStyle(.primary) }
            }
            .panelRow()
            if ServerHost.shared.available { thisMac }
        }
        .settingsForm()
    }

    private var thisMac: some View {
        Section {
            Toggle("Run a server here", isOn: Binding(get: { ServerHost.shared.running },
                                                      set: { on in Task { await pool.host(on) } }))
                .accessibilityLabel("Run a server here")
                .disabled(ServerHost.shared.changing)
            if ServerHost.shared.leftover {
                LabeledContent("Old server copy") {
                    Button("Delete", role: .destructive) { deletingCopy = true }
                        .buttonStyle(.bordered)
                        .buttonBorderShape(.roundedRectangle)
                        .accessibilityLabel("Delete old server copy")
                }
                .confirmationDialog("Delete the old copy of this server?", isPresented: $deletingCopy) {
                    Button("Delete", role: .destructive) { Task { await ServerHost.shared.forgetCopy() } }
                } message: {
                    Text("The server keeps running where it moved to. This copy can’t be recovered.")
                }
            }
        } header: {
            Text("This Mac").foregroundStyle(Color.primary)
        } footer: {
            if let issue = ServerHost.shared.issue { Text(issue).font(.footnote).foregroundStyle(.muted) }
        }
        .panelRow()
    }
}

private struct ActionRow: View {
    let title: String
    let detail: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: PanelStyle.rowInset) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).lineLimit(1)
                    Text(detail).font(.subheadline).foregroundStyle(.muted).lineLimit(1)
                }
                .layoutPriority(1)
                Spacer(minLength: PanelStyle.rowInset)
                Image(systemName: "chevron.right").foregroundStyle(.muted)
            }
            .padding(.vertical, 2)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityValue(detail)
    }
}

/// One network's page: its name, server and devices, and moving, merging or leaving it.
struct NetworkPage: View {
    let pool: SessionPool
    let server: ServerDevice
    let onBack: () -> Void
    @Environment(\.panelClose) private var close
    @State private var name: String
    @State private var managing: NetworkInfo.Device?
    @State private var adding = false
    @State private var choosing: MoveServer.Kind?
    @State private var transfer: Transfer?
    @State private var leaving = false

    private struct Transfer {
        let kind: MoveServer.Kind
        let target: ServerDevice
    }

    init(pool: SessionPool, server: ServerDevice, onBack: @escaping () -> Void) {
        self.pool = pool
        self.server = server
        self.onBack = onBack
        _name = State(initialValue: server.title)
    }

    private var session: AppSession? { pool.sessions[server.address] }
    private var store: NetworkStore? { session?.network }
    private var network: NetworkInfo? { store?.value }
    private var unreachable: Bool { store?.isConnected != true }
    private var title: String { name.isEmpty ? server.title : name }
    private var others: [ServerDevice] { pool.servers.known.filter { $0.address != server.address } }

    var body: some View {
        // One container, so returning from a sub-view doesn't refetch over optimistic edits.
        ZStack {
            if let managing {
                DeviceView(device: managing, renames: !restricted(managing),
                           status: restricted(managing) ? .unknown : status(managing), server: managing.id == network?.host,
                           onBack: { self.managing = nil }, save: { rename(managing, to: $0) }) { manage(managing) }
            } else if adding {
                AddDevice(client: session?.client, onBack: { adding = false })
            } else if let transfer, let id = transfer.target.serverID {
                MoveServer(kind: transfer.kind, sourceName: title, source: session?.client, network: store,
                           target: NetworkInfo.Peer(id: id, name: transfer.target.title), finished: session == nil) {
                    self.transfer = nil
                    if session == nil { onBack() }
                }
            } else if let choosing {
                choice(choosing)
            } else {
                page
            }
        }
    }

    private var page: some View {
        PanelView(title, onClose: { close() }, onBack: back) {
            Form {
                Section {
                    SettingsField(title: "Name", text: $name, prompt: server.title)
                        .accessibilityIdentifier("network.name")
                        .onSubmit(save)
                    LabeledContent("Server") {
                        Text(network?.server.name ?? server.name).lineLimit(1).truncationMode(.middle)
                    }
                }
                .panelRow()
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
                    if let issue = store?.actionIssue ?? store?.issue {
                        Text(issue).font(.footnote).foregroundStyle(.muted)
                    }
                }
                .panelRow()
                Section {
                    ActionRow(title: "Move server", detail: others.isEmpty ? "Join another network first" : "Run \(title) from another device") {
                        start(.move)
                    }
                    ActionRow(title: "Merge network", detail: others.isEmpty ? "Join another network first" : "Bring its chats into another network") {
                        start(.merge)
                    }
                } footer: {
                    if let issue = pool.linkIssue ?? network?.moveIssue.flatMap({ $0.isEmpty ? nil : $0 }) {
                        Text(issue).font(.footnote).foregroundStyle(.muted)
                    }
                }
                .disabled(others.isEmpty)
                .panelRow()
                if server.address != ServerHost.address {
                    Section {
                        Button("Leave \(title)", role: .destructive) { leaving = true }
                            .buttonStyle(.borderless)
                            .foregroundStyle(.red)
                            .frame(maxWidth: .infinity)
                    }
                    .panelRow()
                }
            }
            .formStyle(.grouped)
        }
        .panelDismiss(onClose: { close() })
        .alert("Leave \(title)?", isPresented: $leaving) {
            Button("Leave", role: .destructive) {
                pool.remove(server.address)
                onBack()
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("You’ll need a new pairing link to join it again.")
        }
    }

    private func choice(_ kind: MoveServer.Kind) -> some View {
        PanelView(kind == .move ? "Move server" : "Merge network", onClose: { close() }, onBack: { choosing = nil }) {
            Form {
                Section {
                    ForEach(others) { target in
                        NetworkRow(server: target, store: pool.sessions[target.address]?.network) { begin(kind, target) }
                    }
                } header: {
                    Text(kind == .move ? "Move to" : "Merge into").foregroundStyle(Color.primary)
                } footer: {
                    if let issue = pool.linkIssue { Text(issue).font(.footnote).foregroundStyle(.muted) }
                }
                .panelRow()
            }
            .formStyle(.grouped)
        }
        .panelDismiss(onClose: { close() })
    }

    /// Devices on this network, this one first.
    @ViewBuilder private var devices: some View {
        if network == nil && store?.issue == nil && store?.hasChecked != true {
            ProgressView().frame(maxWidth: .infinity).accessibilityLabel("Loading devices")
        } else if network == nil && unreachable {
            Text("Offline").foregroundStyle(.muted).frame(maxWidth: .infinity)
        }
        ForEach((network?.devices ?? []).sorted { $0.id == network?.current && $1.id != network?.current }) { device in
            Button { managing = device } label: {
                HStack(spacing: 10) {
                    StatusDot(state: status(device))
                    VStack(alignment: .leading) {
                        HStack(alignment: .firstTextBaseline, spacing: 6) {
                            Text(device.title).lineLimit(1).layoutPriority(1)
                            if device.id == network?.current {
                                Text("This device").font(.subheadline).foregroundStyle(.muted).lineLimit(1)
                            }
                        }
                        if device.title != device.name {
                            Text(device.name).font(.subheadline).foregroundStyle(.muted).lineLimit(1)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    Image(systemName: "chevron.right").foregroundStyle(.muted)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityLabel(device.title)
            .accessibilityValue(details(device))
            .appMenu {
                Button("Rename", systemImage: "pencil") { managing = device }
                if removable(device) {
                    Button("Remove", systemImage: "trash", role: .destructive) { remove(device) }
                }
            }
        }
        ForEach(network?.restricted ?? []) { device in
            Button { managing = device } label: {
                LabeledContent(device.title) {
                    Image(systemName: "chevron.right").foregroundStyle(.muted)
                }
                .foregroundStyle(.muted)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(device.title)
        }
    }

    /// What the device page offers besides its name.
    @ViewBuilder private func manage(_ device: NetworkInfo.Device) -> some View {
        if restricted(device) {
            LabeledContent("Access") {
                Button("Allow") {
                    managing = nil
                    Task { await store?.allow(device) }
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.roundedRectangle)
                .accessibilityLabel("Allow \(device.title)")
            }
        } else if removable(device) {
            Button("Remove", role: .destructive) {
                managing = nil
                remove(device)
            }
            .buttonStyle(.borderless)
            .frame(maxWidth: .infinity)
            .accessibilityLabel("Remove \(device.title)")
        }
    }

    private func back() {
        save()
        onBack()
    }

    private func save() {
        let value = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty, value != server.title else { return }
        pool.servers.remember(server.address, network: value)
        Task { await store?.name(value) }
    }

    private func start(_ kind: MoveServer.Kind) {
        if others.count == 1, let only = others.first { begin(kind, only) } else { choosing = kind }
    }

    private func begin(_ kind: MoveServer.Kind, _ target: ServerDevice) {
        Task {
            guard await pool.link(server.address, to: target.address) else { return }
            choosing = nil
            transfer = Transfer(kind: kind, target: target)
        }
    }

    private func restricted(_ device: NetworkInfo.Device) -> Bool {
        network?.restricted?.contains { $0.id == device.id } == true
    }

    private func removable(_ device: NetworkInfo.Device) -> Bool {
        device.id != network?.current && device.id != network?.host
    }

    /// What a device row shows besides its name, for VoiceOver.
    private func details(_ device: NetworkInfo.Device) -> String {
        [device.id == network?.host ? "Server" : nil, status(device).title, device.id == network?.current ? "This device" : nil,
         device.title != device.name ? device.name : nil]
            .compactMap { $0 }.joined(separator: ", ")
    }

    private func status(_ device: NetworkInfo.Device) -> StatusDot.State {
        if device.paired == false { return .pairing }
        // The list is from the cache when the server can't be reached, so presence is unknown.
        if unreachable || store?.issue != nil { return .unknown }
        return device.id == network?.current || network?.online?.contains(device.id) == true ? .online : .offline
    }

    private func rename(_ device: NetworkInfo.Device, to name: String) {
        let label = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard label != (device.label ?? "") else { return }
        Task { await store?.rename(device, label: label) }
    }

    private func remove(_ device: NetworkInfo.Device) {
        guard removable(device) else { return }
        Task { await store?.remove(device) }
    }
}

/// Asked after joining a network when this Mac's own server holds data.
struct JoinChoice: View {
    let pool: SessionPool
    let joined: String
    let done: () -> Void
    @Environment(\.panelClose) private var close
    @State private var merging = false
    @State private var started = false

    private var target: ServerDevice? { pool.servers.devices.first { $0.address == joined } }
    private var local: ServerDevice? { pool.servers.devices.first { $0.address == ServerHost.address } }

    var body: some View {
        if started, let target, let local, let id = target.serverID {
            let session = pool.sessions[local.address]
            MoveServer(kind: .merge, sourceName: local.title, source: session?.client, network: session?.network,
                       target: NetworkInfo.Peer(id: id, name: target.title), finished: session == nil, onBack: done)
        } else {
            page
        }
    }

    private var page: some View {
        PanelView("Joined \(target?.title ?? "network")", onClose: { close() }) {
            Form {
                if let target {
                    Section { NetworkRow(server: target, store: pool.sessions[target.address]?.network) }
                        .panelRow()
                }
                if let target, let local {
                    Section {
                        option("Keep separate", detail: "This Mac stays on \(local.title) and on \(target.title).", chosen: !merging) { merging = false }
                        option("Merge into \(target.title)", detail: "\(local.title)’s chats move to \(target.title) and this Mac stops being a server. This can’t be undone.",
                               chosen: merging) { merging = true }
                    } header: {
                        Text("This Mac already has \(local.title)").foregroundStyle(Color.primary)
                    } footer: {
                        if let issue = pool.linkIssue { Text(issue).font(.footnote).foregroundStyle(.muted) }
                    }
                    .panelRow()
                }
            }
            .formStyle(.grouped)
        } actions: {
            Button("Done", action: finish)
                .disabled(merging && target?.serverID == nil)
        }
        .panelDismiss(onClose: { close() })
    }

    private func finish() {
        guard merging else { return done() }
        Task {
            if await pool.link(ServerHost.address, to: joined) { started = true }
        }
    }

    private func option(_ title: String, detail: String, chosen: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(alignment: .top, spacing: PanelStyle.headingInset) {
                Image(systemName: chosen ? "checkmark.circle.fill" : "circle")
                    .font(.title3)
                    .foregroundStyle(chosen ? Color.accentColor : Color.muted)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                    Text(detail).font(.subheadline).foregroundStyle(.muted)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.vertical, 2)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityValue(chosen ? "Selected" : "")
        .accessibilityHint(detail)
    }
}
