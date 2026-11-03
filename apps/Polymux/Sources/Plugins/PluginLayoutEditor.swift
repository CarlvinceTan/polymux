import SwiftUI
import PolymuxKit

/// Arranges the switcher and the shared Scoped buttons. The call button stays fixed.
struct PluginLayoutEditor: View {
    let store: PluginStore
    let onBack: () -> Void
    @Environment(\.panelClose) private var close
    @State private var switcher: [String]
    @State private var buttons: [ButtonRow]
    @State private var previewTab = PluginTab.chats

    private struct ButtonRow: Identifiable, Hashable {
        let id: String
        /// Shares glass with the row above.
        var merged: Bool
    }

    init(store: PluginStore, onBack: @escaping () -> Void) {
        self.store = store
        self.onBack = onBack
        let layout = store.list?.layout ?? PluginLayout()
        _switcher = State(initialValue: layout.switcher)
        _buttons = State(initialValue: layout.buttons.flatMap { group in
            group.enumerated().map { ButtonRow(id: $0.element, merged: $0.offset > 0) }
        })
    }

    private var hasGlobal: Bool { switcher.count > 1 }

    var body: some View {
        PanelView("Arrange", onClose: { close() }, onBack: onBack, width: PanelSize.width, height: PanelSize.maximumHeight,
                  closeLabel: "Close Settings") {
            Form {
                if hasGlobal { switcherSection }
                if !buttons.isEmpty { buttonSection }
            }
            .formStyle(.grouped)
            #if os(iOS)
            .environment(\.editMode, .constant(.active))
            #endif
            .accessibilityIdentifier("plugin.layout")
        }
        .panelDismiss(onClose: { close() })
        .onChange(of: switcher) { _, _ in save() }
        .onChange(of: buttons) { _, _ in save() }
    }

    private var switcherSection: some View {
        Section {
            PluginSwitcher(tabs: switcherTabs, selection: $previewTab)
                .frame(maxWidth: 320).frame(maxWidth: .infinity)
                .allowsHitTesting(false)
                .moveDisabled(true)
            ForEach(switcher, id: \.self) { name in
                HStack {
                    Label(title(name), systemImage: symbol(name)).labelStyle(PluginLabel()).lineLimit(1)
                    Spacer(minLength: 8)
                    arrows(title(name), first: name == switcher.first, last: name == switcher.last) { shift(name, by: $0) }
                }
            }
            .onMove { switcher.move(fromOffsets: $0, toOffset: $1) }
        } header: {
            Text("Switcher").foregroundStyle(.primary)
        }
        .panelRow()
    }

    private var buttonSection: some View {
        Section {
            preview
                .moveDisabled(true)
            ForEach($buttons) { $row in
                HStack {
                    Label(title(row.id), systemImage: symbol(row.id)).labelStyle(PluginLabel()).lineLimit(1)
                    Spacer(minLength: 8)
                    if row.id != buttons.first?.id {
                        Text("Merge").foregroundStyle(.muted).accessibilityHidden(true)
                        Toggle("Merge", isOn: $row.merged)
                            .labelsHidden()
                            .toggleStyle(.switch)
                            .controlSize(.mini)
                            .accessibilityLabel("Merge \(title(row.id))")
                    }
                    arrows(title(row.id), first: row.id == buttons.first?.id, last: row.id == buttons.last?.id) { shift(row: row.id, by: $0) }
                }
            }
            .onMove { buttons.move(fromOffsets: $0, toOffset: $1) }
        } header: {
            Text("Buttons").foregroundStyle(.primary)
        }
        .panelRow()
    }

    private var preview: some View {
        HStack(spacing: 8) {
            Spacer(minLength: 0)
            PluginButtons(groups: previewGroups) { _ in }
            PanelNavButton(symbol: "phone", label: "Speech conversation") {}
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    private func arrows(_ name: String, first: Bool, last: Bool, move: @escaping (Int) -> Void) -> some View {
        HStack(spacing: 2) {
            Button("Move up", systemImage: "arrow.up") { move(-1) }
                .accessibilityLabel("Move \(name) up").help("Move up").disabled(first)
            Button("Move down", systemImage: "arrow.down") { move(1) }
                .accessibilityLabel("Move \(name) down").help("Move down").disabled(last)
        }
        .labelStyle(.iconOnly).buttonStyle(.borderless)
    }

    private func shift(_ name: String, by offset: Int) {
        guard let index = switcher.firstIndex(of: name), switcher.indices.contains(index + offset) else { return }
        switcher.swapAt(index, index + offset)
    }

    private func shift(row id: String, by offset: Int) {
        guard let index = buttons.firstIndex(where: { $0.id == id }), buttons.indices.contains(index + offset) else { return }
        buttons.swapAt(index, index + offset)
    }

    private var switcherTabs: [PluginTab] {
        switcher.map { PluginTab(id: $0, plugin: store.plugin(named: $0)) }
    }

    private var groups: [[String]] {
        var out: [[String]] = []
        for (index, row) in buttons.enumerated() {
            if row.merged, index > 0, !out.isEmpty { out[out.count - 1].append(row.id) } else { out.append([row.id]) }
        }
        return out
    }

    private var previewGroups: [[Plugin]] {
        groups.map { group in group.compactMap(store.plugin(named:)) }.filter { !$0.isEmpty }
    }

    private func title(_ name: String) -> String {
        name == PluginTab.chats ? "Chats" : store.plugin(named: name)?.title ?? name
    }

    private func symbol(_ name: String) -> String {
        PluginSwitcher.symbol(PluginTab(id: name, plugin: store.plugin(named: name)))
    }

    private func save() {
        store.save(layout: PluginLayout(switcher: switcher, buttons: groups))
    }
}
