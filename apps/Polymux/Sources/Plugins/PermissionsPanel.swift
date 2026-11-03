import SwiftUI
import PolymuxKit

/// One rule of a chat. The plugin's service enforces it; the app only edits the list.
struct PermissionRule: Codable, Equatable, Identifiable, Sendable {
    var id = UUID().uuidString
    var effect: String
    var kind: String
    var pattern: String

    var title: String { pattern.isEmpty ? "Everything" : pattern }

    var summary: String {
        let action = ["read": "reading", "write": "writing", "command": "commands", "tool": "tool"][kind] ?? kind
        return "\(effect == "allow" ? "Allow" : "Deny") \(action)"
    }
}

/// A chat's rules. Exposed means commands can still reach what its path rules cover.
struct PermissionRules: Codable, Equatable, Sendable {
    var rules: [PermissionRule] = []
    var exposed = false
}

/// A chat's permission rules, in the side panel.
struct PermissionsPanel: View {
    let plugin: Plugin
    let thread: PolymuxKit.Thread
    let session: AppSession
    @State private var shown = PermissionRules()
    @State private var saved = PermissionRules()
    @State private var saving: Task<Void, Never>?
    @State private var failed = false
    @State private var effect = "deny"
    @State private var kind = "read"
    @State private var pattern = ""

    private var path: String { "chats/\(thread.id)/rules" }
    private var cacheKey: String { "permissions.\(thread.id)" }
    private var field: String { ["read": "Path", "write": "Path", "command": "Command"][kind] ?? "Tool" }
    private var prompt: String { ["read": "All, or ~/.ssh/**", "write": "All, or src/**", "command": "All, or git *"][kind] ?? "message_agent" }
    private var entry: String { pattern.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        Form {
            Section {
                if shown.rules.isEmpty {
                    Text("No rules yet.").foregroundStyle(.muted).frame(maxWidth: .infinity)
                }
                ForEach(shown.rules) { row($0) }
            } header: {
                Text("Rules").foregroundStyle(.primary)
            } footer: {
                if failed {
                    Text("That change wasn't saved.").foregroundStyle(.muted)
                } else if shown.exposed {
                    Text("Commands can still reach these files.").foregroundStyle(.muted)
                }
            }
            .panelRow()
            Section {
                SettingsPicker("Rule", selection: $effect, options: [("Deny", "deny"), ("Allow", "allow")])
                SettingsPicker("Action", selection: $kind, options: [("Reading", "read"), ("Writing", "write"), ("Commands", "command"), ("Tool", "tool")])
                SettingsField(title: field, text: $pattern, prompt: prompt)
                Button("Add") { add() }
                    .buttonStyle(.borderless)
                    .frame(maxWidth: .infinity)
                    .disabled(kind == "tool" && entry.isEmpty)
                    .accessibilityLabel("Add rule")
            } header: {
                Text("New rule").foregroundStyle(.primary)
            }
            .panelRow()
        }
        .formStyle(.grouped)
        .scrollContentBackground(.hidden)
        .task(id: thread.id) { await load() }
    }

    private func row(_ rule: PermissionRule) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(rule.title).lineLimit(1).truncationMode(.middle)
                Text(rule.summary).font(.caption).foregroundStyle(.muted).lineLimit(1)
            }
            .accessibilityElement(children: .combine)
            Spacer(minLength: 8)
            Button("Remove", systemImage: "minus.circle") { save(shown.rules.filter { $0.id != rule.id }) }
                .labelStyle(.iconOnly)
                .buttonStyle(.borderless)
                .help("Remove rule")
                .accessibilityLabel("Remove \(rule.summary), \(rule.title)")
        }
    }

    private func load() async {
        let cache = session.sidebar.cacheActor
        if let cached: PermissionRules = await cache.snapshot(cacheKey) { shown = cached; saved = cached }
        guard let fresh: PermissionRules = try? await session.client.pluginGet(plugin.name, path) else { return }
        shown = fresh
        saved = fresh
        await cache.saveSnapshot(cacheKey, fresh)
    }

    private func add() {
        save(shown.rules + [PermissionRule(effect: effect, kind: kind, pattern: entry)])
        pattern = ""
    }

    /// Shows the change at once. Saves run in order, since each one replaces the whole list.
    private func save(_ rules: [PermissionRule]) {
        shown.rules = rules
        failed = false
        let earlier = saving
        saving = Task {
            await earlier?.value
            do {
                saved = try await session.client.pluginWrite(plugin.name, path, method: "PUT", body: PermissionRules(rules: rules))
                if shown.rules == rules { shown = saved }
                await session.sidebar.cacheActor.saveSnapshot(cacheKey, saved)
            } catch {
                shown = saved
                failed = true
            }
        }
    }
}
