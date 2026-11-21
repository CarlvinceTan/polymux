import SwiftUI
import PolymuxKit
import CoreImage.CIFilterBuiltins

/// Hub's accounts as a page of its plugin settings.
struct HubAccountsPage: View {
    let back: () -> Void
    @Environment(HubStore.self) private var store: HubStore?

    var body: some View {
        if let store { HubAccounts(store: store, onBack: back) }
    }
}

/// Messaging accounts and mailboxes: lists them, links more and unlinks them.
struct HubAccounts: View {
    @Bindable var store: HubStore
    var onBack: (() -> Void)?
    @Environment(\.panelClose) private var close
    @State private var page = Page.list
    @State private var signingIn = false

    private enum Page: Hashable {
        case list, platform(String), mail, edit(String), signatures(String?), client(HubMailProvider)
    }

    var body: some View {
        Group {
            if let login = store.login {
                HubLoginPage(store: store, login: login, onClose: leave)
            } else {
                switch page {
                case .list: list
                case let .platform(id):
                    if let platform = store.status?.platforms.first(where: { $0.id == id }) {
                        HubPlatformPage(store: store, platform: platform, onClose: leave) { page = .list }
                    }
                case .mail: HubMailForm(store: store, onClose: leave) { page = .list }
                case let .edit(id):
                    HubMailForm(store: store, editing: store.mail.accounts.first { $0.id == id }, onClose: leave) { page = .list }
                case let .signatures(scope):
                    HubSignatures(store: store, onClose: leave, onBack: { page = .list }, scope: scope)
                case let .client(provider):
                    HubMailClientForm(store: store, provider: provider, onClose: leave) { saved in
                        page = .list
                        if saved { signIn(provider) }
                    }
                }
            }
        }
        .panelDismiss(onClose: leave)
        .task {
            // Keeps each mailbox's state true while this page is open.
            while !Task.isCancelled {
                await store.checkMail()
                try? await Task.sleep(for: .seconds(60))
            }
        }
    }

    private func leave() {
        store.cancelLogin()
        close()
    }

    private var list: some View {
        PanelView("Accounts", onClose: leave, onBack: onBack, width: PanelSize.width, height: PanelSize.maximumHeight) {
            Form {
                Section {
                    let platforms = store.status?.platforms ?? []
                    if platforms.isEmpty {
                        Text(store.status?.state == HubStatusState.failed ? store.status?.issue ?? "Hub isn’t available." : "Hub is starting.")
                            .foregroundStyle(.muted).frame(maxWidth: .infinity)
                    }
                    ForEach(platforms, id: \.id) { platform in row(platform) }
                } header: {
                    Text("Messaging").foregroundStyle(.primary)
                }
                .panelRow()
                mail
                if let issue = store.issue {
                    Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow()
                }
            }
            .formStyle(.grouped)
        }
    }

    private var mail: some View {
        Section {
            ForEach(store.status?.mail ?? [], id: \.id) { mailbox in
                VStack(alignment: .leading, spacing: 2) {
                    Text(mailbox.email).lineLimit(1)
                    Text(mailbox.issue ?? "Reachable").font(.caption).foregroundStyle(.muted).lineLimit(2)
                    Text(details(mailbox)).font(.caption).foregroundStyle(.muted).lineLimit(1)
                    HStack(spacing: PanelStyle.rowInset) {
                        Button("Signatures") { page = .signatures(mailbox.id) }
                            .accessibilityLabel("Signatures for \(mailbox.email)")
                        Button("Edit") { page = .edit(mailbox.id) }
                            .accessibilityLabel("Edit \(mailbox.email)")
                        Button("Remove") { Task { await store.removeMail(mailbox.id) } }
                            .accessibilityLabel("Remove \(mailbox.email)")
                    }
                    .buttonStyle(.bordered)
                    .padding(.top, 4)
                }
            }
            ForEach(store.status?.mailProviders ?? [], id: \.id) { provider in
                LabeledContent(provider.name) {
                    Button("Sign in") {
                        if provider.setup.isEmpty { signIn(provider) } else { page = .client(provider) }
                    }
                    .buttonStyle(.bordered)
                    .accessibilityLabel("Sign in with \(provider.name)")
                    .disabled(signingIn)
                }
            }
        } header: {
            HStack {
                Text("Mail").foregroundStyle(.primary)
                Spacer()
                if !(store.status?.mail ?? []).isEmpty {
                    Button("Manage signatures", systemImage: "signature") { page = .signatures(nil) }
                        .help("Manage signatures")
                        .labelStyle(.iconOnly)
                        .buttonStyle(.borderless)
                }
                Button("Add mailbox", systemImage: "plus") { page = .mail }
                    .help("Add mailbox")
                    .labelStyle(.iconOnly)
                    .buttonStyle(.borderless)
            }
        }
        .panelRow()
    }

    private func details(_ mailbox: HubMailAccount) -> String {
        [mailbox.imap, mailbox.smtp, mailbox.auth == "oauth" ? "OAuth" : "Password"].compactMap { $0 }.joined(separator: " · ")
    }

    private func signIn(_ provider: HubMailProvider) {
        signingIn = true
        Task {
            defer { signingIn = false }
            _ = await store.signIn(provider.id) { url in try await HubSignIn.run(url) }
        }
    }

    private func row(_ platform: HubPlatform) -> some View {
        let note = summary(platform)
        return Button { page = .platform(platform.id) } label: {
            HStack(spacing: PanelStyle.headingInset) {
                PlatformMark(platform: platform.id, size: 22)
                Text(platform.name).lineLimit(1).layoutPriority(1)
                Spacer(minLength: PanelStyle.rowInset)
                Text(note).foregroundStyle(.muted).lineLimit(1)
                Image(systemName: "chevron.right").foregroundStyle(.muted)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(platform.name)
        .accessibilityValue(note)
    }

    private func summary(_ platform: HubPlatform) -> String {
        if let issue = platform.issue, !issue.isEmpty { return issue }
        if platform.accounts.count > 1 { return "\(platform.accounts.count) accounts" }
        return platform.accounts.first?.name ?? ""
    }
}

/// One platform: its linked accounts, then the ways to link another.
private struct HubPlatformPage: View {
    @Bindable var store: HubStore
    let platform: HubPlatform
    let onClose: () -> Void
    let onBack: () -> Void
    @State private var values: [String: String] = [:]

    private var needsSetup: Bool { platform.accounts.isEmpty && !platform.setup.isEmpty }

    /// A bridge that names no flows still signs in through its default.
    private var flows: [HubFlow] { platform.flows.isEmpty ? [HubFlow(id: "", name: "Account")] : platform.flows }

    var body: some View {
        PanelView(platform.name, onClose: onClose, onBack: onBack, width: PanelSize.width, height: PanelSize.maximumHeight) {
            Form {
                if !platform.accounts.isEmpty { accounts }
                if needsSetup { setup } else { add }
                if let issue = store.issue {
                    Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow()
                }
            }
            .formStyle(.grouped)
        } actions: {
            if needsSetup {
                Button("Save") { Task { await store.setup(platform.id, values: values) } }
                    .keyboardShortcut(.defaultAction)
            }
        }
    }

    private var accounts: some View {
        Section {
            ForEach(platform.accounts, id: \.id) { account in
                LabeledContent {
                    Button("Disconnect") { Task { await store.disconnect(platform.id, account: account.id) } }
                        .buttonStyle(.bordered)
                        .accessibilityLabel("Disconnect \(account.name)")
                } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(account.name).lineLimit(1)
                        if let issue = account.issue, !issue.isEmpty {
                            Text(issue).font(.caption).foregroundStyle(.muted).lineLimit(2)
                        }
                    }
                }
            }
        } header: {
            Text("Accounts").foregroundStyle(.primary)
        }
        .panelRow()
    }

    private var add: some View {
        Section {
            ForEach(flows, id: \.id) { flow in
                LabeledContent(flow.name) {
                    Button("Connect") { Task { await store.connect(platform.id, flow: flow.id) } }
                        .buttonStyle(.bordered)
                        .accessibilityLabel("Connect \(flows.count > 1 ? flow.name : platform.name)")
                        .disabled(store.loginBusy)
                }
            }
        } header: {
            Text("Add account").foregroundStyle(.primary)
        }
        .panelRow()
    }

    private var setup: some View {
        Section {
            ForEach(platform.setup, id: \.id) { field in
                SettingsField(title: field.name, text: Binding(get: { values[field.id] ?? "" }, set: { values[field.id] = $0 }),
                              prompt: field.name, secure: field.secret)
            }
        } header: {
            Text("Setup").foregroundStyle(.primary)
        }
        .panelRow()
    }
}

private struct HubLoginPage: View {
    @Bindable var store: HubStore
    let login: HubLogin
    let onClose: () -> Void
    @State private var values: [String: String] = [:]

    private var title: String {
        store.status?.platforms.first { $0.id == login.platform }?.name ?? login.platform.capitalized
    }

    private var entering: Bool { login.step.kind == HubLoginStepKind.input }

    var body: some View {
        PanelView(title, onClose: onClose, onBack: { store.cancelLogin() }, width: PanelSize.width, height: PanelSize.maximumHeight) {
            if entering { form } else { display }
        } actions: {
            if entering {
                Button("Continue") { Task { await store.answer(values) } }
                    .disabled(store.loginBusy)
                    .keyboardShortcut(.defaultAction)
            }
        }
    }

    private var form: some View {
        Form {
            Section {
                ForEach(login.step.fields, id: \.id) { field in
                    SettingsField(title: field.name, text: Binding(get: { values[field.id] ?? "" }, set: { values[field.id] = $0 }),
                                  prompt: field.name, secure: field.secret)
                }
            } footer: {
                Text(login.step.instructions).foregroundStyle(.muted)
            }
            .panelRow()
            if let issue = store.issue {
                Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow()
            }
        }
        .formStyle(.grouped)
    }

    private var display: some View {
        VStack(spacing: PanelStyle.contentInset) {
            Text(login.step.instructions).multilineTextAlignment(.center).foregroundStyle(.muted)
            if login.step.kind != HubLoginStepKind.display {
                Text("This sign-in isn’t supported yet.").foregroundStyle(.muted)
            } else if login.step.display == "qr" {
                QRCode(text: login.step.code).frame(width: 220, height: 220)
            } else {
                Text(login.step.code).font(.title.monospaced()).textSelection(.enabled)
            }
            if let issue = store.issue { Text(issue).font(.footnote).foregroundStyle(.muted) }
        }
        .padding(PanelStyle.contentInset)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

private struct QRCode: View {
    let text: String

    var body: some View {
        if let image { Image(decorative: image, scale: 1).interpolation(.none).resizable().accessibilityLabel("Sign-in code") }
    }

    private var image: CGImage? {
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(text.utf8)
        guard let output = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: 8, y: 8)) else { return nil }
        return CIContext().createCGImage(output, from: output.extent)
    }
}

private struct HubMailForm: View {
    @Bindable var store: HubStore
    var editing: HubMailAccount?
    let onClose: () -> Void
    let done: () -> Void
    @State private var preset = "custom"
    @State private var name = ""
    @State private var email = ""
    @State private var password = ""
    @State private var imapHost = ""
    @State private var imapPort = "993"
    @State private var smtpHost = ""
    @State private var smtpPort = "587"
    @State private var busy = false

    private var presets: [HubMailPreset] { store.status?.mailPresets ?? [] }

    /// A provider without its own choice is set up as Other, so its hint follows the servers.
    private var hint: String? {
        let host = presets.first { !$0.imapHost.isEmpty && ($0.imapHost == imapHost || $0.smtpHost == smtpHost) }
        return (host ?? presets.first { $0.id == preset })?.hint
    }

    private func choose(_ id: String) {
        preset = id
        guard let chosen = presets.first(where: { $0.id == id }) else { return }
        imapHost = chosen.imapHost
        imapPort = String(chosen.imapPort)
        smtpHost = chosen.smtpHost
        smtpPort = String(chosen.smtpPort)
    }

    private static func split(_ address: String?) -> (host: String, port: String) {
        guard let address, let colon = address.lastIndex(of: ":") else { return (address ?? "", "") }
        return (String(address[..<colon]), String(address[address.index(after: colon)...]))
    }

    private func begin() {
        guard let editing else { return }
        name = editing.name
        email = editing.email
        (imapHost, imapPort) = Self.split(editing.imap)
        (smtpHost, smtpPort) = Self.split(editing.smtp)
    }

    var body: some View {
        PanelView(editing == nil ? "Add mailbox" : "Edit mailbox", enabled: !busy, onClose: onClose, onBack: done, width: PanelSize.width, height: PanelSize.maximumHeight) {
            Form {
                Section {
                    if editing == nil {
                        Picker("Provider", selection: Binding(get: { preset }, set: choose)) {
                            ForEach(presets.filter { !$0.hidden }, id: \.id) { Text($0.name).tag($0.id) }
                        }
                    }
                    SettingsField(title: "Name", text: $name, prompt: "Your name")
                    SettingsField(title: "Email", text: $email, prompt: "name@example.com")
                    SettingsField(title: "Password", text: $password, prompt: editing == nil ? "App password" : "Keep current", secure: true)
                } header: {
                    Text("Account").foregroundStyle(.primary)
                } footer: {
                    if let hint { Text(hint).foregroundStyle(.muted) }
                }
                .panelRow()
                Section {
                    SettingsField(title: "IMAP server", text: $imapHost, prompt: "Automatic")
                    SettingsField(title: "IMAP port", text: $imapPort, prompt: "993")
                    SettingsField(title: "SMTP server", text: $smtpHost, prompt: "Automatic")
                    SettingsField(title: "SMTP port", text: $smtpPort, prompt: "587")
                } header: {
                    Text("Servers").foregroundStyle(.primary)
                }
                .panelRow()
                if let issue = store.issue {
                    Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow()
                }
            }
            .formStyle(.grouped)
        } actions: {
            Button(editing == nil ? "Add" : "Save") {
                busy = true
                let setup = HubMailSetup(name: name, email: email, password: password, imapHost: imapHost,
                                         imapPort: Int64(imapPort) ?? 993, smtpHost: smtpHost, smtpPort: Int64(smtpPort) ?? 587)
                Task {
                    defer { busy = false }
                    let saved = if let editing { await store.editMail(editing.id, setup) } else { await store.addMail(setup) }
                    if saved { done() }
                }
            }
            .disabled(email.isEmpty || (password.isEmpty && editing == nil) || busy)
            .keyboardShortcut(.defaultAction)
        }
        .onAppear(perform: begin)
    }
}

private struct HubMailClientForm: View {
    @Bindable var store: HubStore
    let provider: HubMailProvider
    let onClose: () -> Void
    let done: (Bool) -> Void
    @State private var values: [String: String] = [:]

    var body: some View {
        PanelView(provider.name, onClose: onClose, onBack: { done(false) }, width: PanelSize.width, height: PanelSize.maximumHeight) {
            Form {
                Section {
                    ForEach(provider.setup, id: \.id) { field in
                        SettingsField(title: field.name, text: Binding(get: { values[field.id] ?? "" }, set: { values[field.id] = $0 }),
                                      prompt: field.name, secure: field.secret)
                    }
                }
                .panelRow()
            }
            .formStyle(.grouped)
        } actions: {
            Button("Save") {
                let value = HubMailClient(clientId: values["clientId"] ?? "", clientSecret: values["clientSecret"] ?? "")
                Task { if await store.setMailClient(provider.id, client: value) { done(true) } }
            }
            .disabled((values["clientId"] ?? "").isEmpty)
            .keyboardShortcut(.defaultAction)
        }
    }
}

/// Picks chats: the shared body of linking and broadcast lists.
private struct ChatPicker: View {
    let chats: [HubChat]
    @Binding var chosen: Set<String>
    var body: some View {
        List(chats, id: \.id) { chat in
            Toggle(isOn: Binding(get: { chosen.contains(chat.id) }, set: { on in
                if on { chosen.insert(chat.id) } else { chosen.remove(chat.id) }
            })) {
                HStack(spacing: 8) {
                    PlatformMark(platform: chat.platform)
                    Text(chat.name).lineLimit(1)
                }
            }
            .accessibilityLabel(chat.name)
        }
        .listStyle(.plain).scrollContentBackground(.hidden)
    }
}

struct HubMerge: View {
    @Bindable var store: HubStore
    @Environment(\.panelClose) private var close
    @State private var name = ""
    @State private var chosen: Set<String> = []

    private var direct: [HubChat] { store.chats.filter { !$0.group && !$0.space } }

    var body: some View {
        PanelView("Link chats", onClose: { close() }, width: PanelSize.width, height: PanelSize.maximumHeight) {
            VStack(spacing: PanelStyle.rowInset) {
                Form { SettingsField(title: "Name", text: $name, prompt: "Contact name") }.formStyle(.grouped).frame(height: 76)
                ChatPicker(chats: direct, chosen: $chosen)
                HStack {
                    Spacer()
                    Button("Link") {
                        let members = direct.filter { chosen.contains($0.id) }
                            .map { HubLinkMember(platform: $0.platform, chat: $0.id, name: $0.name) }
                        Task { await store.merge(name: name, members: members); close() }
                    }
                    .buttonStyle(.borderedProminent).disabled(chosen.count < 2)
                }
                .padding(PanelStyle.contentInset)
            }
        }
        .panelDismiss(onClose: { close() })
    }
}

struct HubBroadcastEditor: View {
    @Bindable var store: HubStore
    @Environment(\.panelClose) private var close
    @State private var name = ""
    @State private var chosen: Set<String> = []

    private var direct: [HubChat] { store.chats.filter { !$0.group && !$0.space } }

    var body: some View {
        PanelView("New list", onClose: { close() }, width: PanelSize.width, height: PanelSize.maximumHeight) {
            VStack(spacing: PanelStyle.rowInset) {
                Form { SettingsField(title: "Name", text: $name, prompt: "List name") }.formStyle(.grouped).frame(height: 76)
                ChatPicker(chats: direct, chosen: $chosen)
                HStack {
                    Spacer()
                    Button("Create") {
                        let members = direct.filter { chosen.contains($0.id) }
                            .map { HubLinkMember(platform: $0.platform, chat: $0.id, name: $0.name) }
                        Task { await store.saveBroadcast(name: name, recipients: members); close() }
                    }
                    .buttonStyle(.borderedProminent).disabled(chosen.isEmpty || name.isEmpty)
                }
                .padding(PanelStyle.contentInset)
            }
        }
        .panelDismiss(onClose: { close() })
    }
}
