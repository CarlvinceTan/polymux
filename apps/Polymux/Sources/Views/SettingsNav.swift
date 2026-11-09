import SwiftUI

/// The settings modal's left column: search above the tabs, like System Settings.
struct SettingsNav: View {
    @Binding var category: SettingsTab
    @State private var query = ""
    @State private var chosen: String?
    @State private var hovered: String?

    /// Matches the macOS Settings sidebar width.
    static let width: CGFloat = 200

    private var text: String { query.trimmingCharacters(in: .whitespaces) }
    private var results: [SettingsEntry] { SettingsEntry.all.filter { $0.matches(text) } }

    var body: some View {
        VStack(spacing: PanelStyle.rowInset) {
            search
            if text.isEmpty {
                VStack(spacing: 2) {
                    ForEach(SettingsTab.allCases) { tab in
                        row(SettingsEntry(tab: tab), selected: category == tab) { category = tab }
                    }
                }
                Spacer(minLength: 0)
            } else if results.isEmpty {
                Text("No results.").foregroundStyle(.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView {
                    VStack(spacing: 2) {
                        ForEach(results) { entry in
                            row(entry, selected: chosen == entry.id) {
                                chosen = entry.id
                                category = entry.tab
                            }
                        }
                    }
                }
                .scrollIndicators(.never)
            }
        }
        .padding(.horizontal, 10)
        // The search field lines up with the pane's first section title.
        .padding(.top, 4)
        .padding(.bottom, 12)
        .frame(width: Self.width)
        .onChange(of: text.isEmpty) { _, empty in if empty { chosen = nil } }
    }

    private var search: some View {
        HStack(spacing: PanelStyle.rowInset) {
            PanelSearch("Search settings", text: $query)
            if !query.isEmpty {
                Button { query = "" } label: { Image(systemName: "xmark.circle.fill") }
                    .buttonStyle(.plain).foregroundStyle(.muted)
                    .accessibilityLabel("Clear settings search").help("Clear search")
            }
        }
        .padding(.horizontal, PanelStyle.headingInset)
        .frame(height: 36)
        .glassEffect(.regular, in: Capsule())
    }

    /// A tab, or a found section with its tab beneath, as System Settings lists results.
    private func row(_ entry: SettingsEntry, selected: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 9) {
                TabIcon(tab: entry.tab)
                VStack(alignment: .leading, spacing: 1) {
                    Text(entry.title).lineLimit(1)
                    if !entry.isTab {
                        Text(entry.tab.title).font(.subheadline).foregroundStyle(.muted).lineLimit(1)
                    }
                }
            }
            .font(.body)
            .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
            .padding(.horizontal, 10)
            .padding(.vertical, 3)
            .background {
                RoundedRectangle(cornerRadius: PanelStyle.rowRadius)
                    .fill(.primary.opacity(selected ? PanelStyle.selectedTint : hovered == entry.id ? PanelStyle.hoverTint : 0))
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .pointerHover { inside in
            if inside { hovered = entry.id } else if hovered == entry.id { hovered = nil }
        }
        .accessibilityLabel(entry.isTab ? entry.title : "\(entry.title), \(entry.tab.title)")
        .accessibilityAddTraits(selected ? [.isSelected] : [])
    }
}

/// A tab's glyph on its coloured tile, like System Settings.
struct TabIcon: View {
    let tab: SettingsTab

    var body: some View {
        tab.icon.resizable().scaledToFit()
            .fontWeight(.medium)
            .frame(width: 14, height: 14)
            .foregroundStyle(.white)
            .frame(width: 22, height: 22)
            .background(tab.tint.gradient, in: .rect(cornerRadius: 6, style: .continuous))
            .accessibilityHidden(true)
    }
}

/// A pane section's glyph on a grey tile, like System Settings' page rows.
struct SectionTile: View {
    let symbol: String

    var body: some View {
        Image(systemName: symbol)
            .resizable().scaledToFit()
            .fontWeight(.medium)
            .frame(width: 14, height: 14)
            .foregroundStyle(.white)
            .frame(width: 22, height: 22)
            .background(Color.gray.gradient, in: .rect(cornerRadius: 6, style: .continuous))
            .accessibilityHidden(true)
    }
}

/// A pane section's title with its tile; the trailing view holds the section's button.
struct SectionHeader<Trailing: View>: View {
    let title: String
    let symbol: String
    var trailing: Trailing

    init(_ title: String, symbol: String, @ViewBuilder trailing: () -> Trailing) {
        self.title = title
        self.symbol = symbol
        self.trailing = trailing()
    }

    init(_ title: String, symbol: String) where Trailing == EmptyView {
        self.init(title, symbol: symbol) { EmptyView() }
    }

    var body: some View {
        HStack(spacing: 8) {
            SectionTile(symbol: symbol)
            Text(title).foregroundStyle(Color.primary)
            Spacer(minLength: 8)
            trailing
        }
    }
}

/// A tab or one of its sections, found by its title or the rows inside it.
struct SettingsEntry: Identifiable {
    let tab: SettingsTab
    /// Nil for the tab itself.
    let section: String?
    let terms: [String]

    init(tab: SettingsTab, section: String? = nil, terms: [String] = []) {
        self.tab = tab
        self.section = section
        self.terms = terms
    }

    var isTab: Bool { section == nil }
    var title: String { section ?? tab.title }
    var id: String { section.map { "\(tab.rawValue).\($0)" } ?? tab.rawValue }

    func matches(_ query: String) -> Bool {
        ([title] + terms).contains { $0.localizedStandardContains(query) }
    }

    /// Every entry in tab order: the tab, then its sections.
    @MainActor static var all: [SettingsEntry] {
        SettingsTab.allCases.flatMap { [SettingsEntry(tab: $0, terms: terms($0))] + sections($0) }
    }

    /// Tabs without sections are found by what they hold.
    private static func terms(_ tab: SettingsTab) -> [String] {
        switch tab {
        case .vault: VaultCategory.allCases.filter { $0 != .all }.map(\.title) + ["Import"]
        default: []
        }
    }

    /// Each tab's section titles and row labels, as its pane shows them.
    @MainActor private static func sections(_ tab: SettingsTab) -> [SettingsEntry] {
        var list: [SettingsEntry] = []
        func add(_ title: String, _ terms: [String] = []) {
            list.append(SettingsEntry(tab: tab, section: title, terms: terms))
        }
        switch tab {
        case .general:
            add("Appearance", ["Theme", "Light", "Dark", "Switcher badges"])
            add("Notifications", ["Allow notifications"])
            #if os(macOS)
            add("Permissions", ["System permissions"])
            #endif
            add("Connections", Connections.available.map(Connections.title))
        case .network:
            add("Networks", ["Join network"])
            if ServerHost.shared.available { add("This Mac", ["Run a server here"]) }
        case .models:
            add("Default model", ["Model", "Reasoning"])
            add("Providers")
            add("Generation", ["Image", "Video"])
            add("Voice", ["Provider", "Model", "Fallbacks", "Language"])
        case .updates:
            #if os(macOS)
            add("Updates", ["Check automatically"])
            #endif
            add("Version", ["Server channel", "Nightly", "Stable"])
        case .vault: break
        }
        return list
    }
}
