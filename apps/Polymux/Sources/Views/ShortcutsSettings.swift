import SwiftUI

enum AppShortcutAction: String, CaseIterable, Identifiable {
    case toggleSidebar, search, find, settings

    var id: String { rawValue }

    var title: String {
        switch self {
        case .toggleSidebar: "Toggle Sidebar"
        case .search: "Search"
        case .find: "Find in Chat"
        case .settings: "Settings"
        }
    }

    var defaultKey: ShortcutKey {
        switch self {
        case .toggleSidebar: ShortcutKey(character: "s", modifiers: [.control, .command])
        case .search: ShortcutKey(character: "k", modifiers: [.command])
        case .find: ShortcutKey(character: "f", modifiers: [.command])
        case .settings: ShortcutKey(character: ",", modifiers: [.command])
        }
    }
}

struct ShortcutKey: Codable, Hashable, Sendable {
    var character: String
    var modifiers: Int

    init(character: String, modifiers: EventModifiers) {
        self.character = character
        self.modifiers = modifiers.rawValue
    }

    var keyEquivalent: KeyEquivalent {
        KeyEquivalent(character.first ?? " ")
    }

    var eventModifiers: EventModifiers { EventModifiers(rawValue: modifiers) }

    var display: String {
        var out = ""
        let modifiers = eventModifiers
        if modifiers.contains(.control) { out += "⌃" }
        if modifiers.contains(.option) { out += "⌥" }
        if modifiers.contains(.shift) { out += "⇧" }
        if modifiers.contains(.command) { out += "⌘" }
        out += character.uppercased()
        return out
    }
}

/// The app's own commands, kept per Mac so each keyboard feels the same.
@MainActor @Observable
final class ShortcutStore {
    static let shared = ShortcutStore()
    private let defaults: UserDefaults
    private var keys: [String: ShortcutKey]

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        keys = (try? JSONDecoder().decode([String: ShortcutKey].self,
                                          from: defaults.data(forKey: Key.shortcuts) ?? Data())) ?? [:]
    }

    func key(_ action: AppShortcutAction) -> ShortcutKey {
        keys[action.rawValue] ?? action.defaultKey
    }

    func set(_ action: AppShortcutAction, _ key: ShortcutKey) {
        var next = keys
        next[action.rawValue] = key
        keys = next
        save()
    }

    func reset(_ action: AppShortcutAction) {
        var next = keys
        next.removeValue(forKey: action.rawValue)
        keys = next
        save()
    }

    func resetAll() {
        keys = [:]
        defaults.removeObject(forKey: Key.shortcuts)
    }

    func isDefault(_ action: AppShortcutAction) -> Bool {
        key(action) == action.defaultKey
    }

    func isTaken(_ key: ShortcutKey, except action: AppShortcutAction) -> Bool {
        AppShortcutAction.allCases.contains { $0 != action && self.key($0) == key }
    }

    private func save() {
        guard let data = try? JSONEncoder().encode(keys) else { return }
        defaults.set(data, forKey: Key.shortcuts)
    }

    private enum Key {
        static let shortcuts = "appShortcuts"
    }
}

struct ShortcutsSettings: View {
    @State private var store = ShortcutStore.shared

    var body: some View {
        #if os(macOS)
        Section {
            ForEach(AppShortcutAction.allCases) { action in
                ShortcutRow(action: action, store: store)
            }
        } header: {
            SectionHeader("Shortcuts", symbol: "keyboard") {
                Button("Reset", systemImage: "arrow.counterclockwise") { store.resetAll() }
                    .disabled(AppShortcutAction.allCases.allSatisfy { store.isDefault($0) })
                    .accessibilityLabel("Reset shortcuts")
            }
        }
        .panelRow()
        #else
        EmptyView()
        #endif
    }
}

#if os(macOS)
import AppKit

private struct ShortcutRow: View {
    let action: AppShortcutAction
    let store: ShortcutStore
    @State private var recording = false
    @State private var monitors: [Any] = []

    var body: some View {
        HStack(spacing: PanelStyle.rowInset) {
            Text(action.title)
            Spacer(minLength: 12)
            Button { recording ? cancel() : begin() } label: {
                Text(recording ? "Press keys" : store.key(action).display)
                    .font(.body.monospaced())
                    .foregroundStyle(recording ? Color.accentColor : Color.secondary)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(action.title) shortcut")
            .accessibilityValue(recording ? "Recording" : store.key(action).display)
            .accessibilityHint("Press a key combination")
            .help("Press a key combination")
        }
        .onDisappear { cancel() }
    }

    private func begin() {
        guard !recording else { return }
        recording = true
        if let monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown, handler: { event in
            if event.keyCode == 53 {
                cancel()
                return nil
            }
            if event.keyCode == 51 || event.keyCode == 117 {
                store.reset(action)
                cancel()
                return nil
            }
            guard let characters = event.charactersIgnoringModifiers, characters.count == 1,
                  let character = characters.first, !character.isWhitespace, !character.isNewline else {
                return nil
            }
            let modifiers = Self.modifiers(event.modifierFlags)
            guard modifiers.contains(.command) || modifiers.contains(.control) || modifiers.contains(.option) else {
                NSSound.beep()
                return nil
            }
            let shortcut = ShortcutKey(character: String(character).lowercased(), modifiers: modifiers)
            guard !store.isTaken(shortcut, except: action) else {
                NSSound.beep()
                return nil
            }
            store.set(action, shortcut)
            cancel()
            return nil
        }) {
            monitors.append(monitor)
        }
        if let monitor = NSEvent.addLocalMonitorForEvents(matching: .leftMouseDown, handler: { _ in
            cancel()
            return nil
        }) {
            monitors.append(monitor)
        }
    }

    private func cancel() {
        for monitor in monitors { NSEvent.removeMonitor(monitor) }
        monitors.removeAll()
        recording = false
    }

    private static func modifiers(_ flags: NSEvent.ModifierFlags) -> EventModifiers {
        var out: EventModifiers = []
        if flags.contains(.control) { out.insert(.control) }
        if flags.contains(.option) { out.insert(.option) }
        if flags.contains(.shift) { out.insert(.shift) }
        if flags.contains(.command) { out.insert(.command) }
        return out
    }
}
#endif
