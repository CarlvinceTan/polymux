import SwiftUI
import PolymuxKit

extension EnvironmentValues {
    @Entry var reactionPicker = ReactionAction()
}

struct ReactionAction {
    var action: () -> Void = {}

    @MainActor func callAsFunction() { action() }
}

extension View {
    func reactionPicker(react: @escaping (String) -> Void) -> some View {
        modifier(ReactionPicker(react: react))
    }
}

private struct ReactionPicker: ViewModifier {
    @Environment(\.colorScheme) private var scheme
    @AppStorage("recentReactions") private var recent = ""
    @State private var showing = false
    let react: (String) -> Void

    func body(content: Content) -> some View {
        content
            .environment(\.reactionPicker, ReactionAction { showing = true })
            .background(alignment: .topTrailing) {
                Color.clear.frame(width: 1, height: 1)
                    .popover(isPresented: $showing) {
                        EmojiPicker { emoji in
                            showing = false
                            recent = Reaction.suggestions(recent: [emoji] + recent.components(separatedBy: ",")).joined(separator: ",")
                            react(emoji)
                        }
                        .appAppearance(scheme == .dark ? "dark" : "light")
                    }
            }
    }
}

struct EmojiPicker: View {
    @State private var groups: [EmojiGroup] = []
    @State private var search = ""
    @State private var category: String?
    /// What picking one does, for its accessible name.
    var action = "React"
    let select: (String) -> Void

    private var matches: [EmojiGroup] {
        groups.compactMap { group in
            if search.isEmpty, let category, category != group.name { return nil }
            let emoji = group.emoji.filter {
                search.isEmpty ? !$0.tone : $0.name.localizedStandardContains(search) || $0.emoji == search
            }
            return emoji.isEmpty ? nil : EmojiGroup(name: group.name, icon: group.icon, emoji: emoji)
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            TextField("Search Emoji", text: $search)
                .accessibilityLabel("Search Emoji")
                .textFieldStyle(.roundedBorder)
                .padding(10)
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 8) {
                    ForEach(matches, id: \.name) { group in
                        Text(group.name).font(.caption).foregroundStyle(.muted)
                        LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 0), count: 6), spacing: 0) {
                            ForEach(group.emoji, id: \.emoji) { entry in
                                Button { select(entry.emoji) } label: {
                                    Text(entry.emoji).font(.system(size: 26))
                                        .frame(maxWidth: .infinity).frame(height: 40)
                                        .contentShape(Rectangle())
                                }
                                .buttonStyle(.borderless)
                                .help(entry.name)
                                .accessibilityLabel("\(action) \(entry.emoji)")
                            }
                        }
                    }
                }
                .padding(.horizontal, 10)
            }
            .id(category)
            .overlay {
                if !groups.isEmpty && matches.isEmpty { Text("No emoji.").foregroundStyle(.muted) }
            }
            Divider()
            HStack(spacing: 0) {
                ForEach(groups, id: \.name) { group in
                    Button(group.name, systemImage: group.icon) {
                        search = ""
                        category = group.name
                    }
                    .labelStyle(.iconOnly)
                    .buttonStyle(.borderless)
                    .foregroundStyle(category == group.name && search.isEmpty ? Color.primary : Color.muted)
                    .accessibilityValue(category == group.name && search.isEmpty ? "Selected" : "")
                    .frame(maxWidth: .infinity)
                    .help(group.name)
                }
            }
            .padding(10)
        }
        .frame(width: 346)
        .frame(idealHeight: 470, maxHeight: 470)
        .task { groups = await EmojiCatalogue.groups.value }
    }
}

private struct EmojiGroup: Decodable, Sendable {
    let name: String
    let icon: String
    let emoji: [EmojiEntry]
}

private struct EmojiEntry: Decodable, Sendable {
    let emoji: String
    let name: String
    let tone: Bool
}

enum EmojiCatalogue {
    /// Decoded ahead of the first reaction, so the picker opens filled.
    static func warm() { _ = groups }

    fileprivate static let groups = Task.detached(priority: .userInitiated) { () -> [EmojiGroup] in
        guard let url = Bundle.main.url(forResource: "Emoji", withExtension: "json"),
              let data = try? Data(contentsOf: url) else { return [] }
        return (try? JSONDecoder().decode([EmojiGroup].self, from: data)) ?? []
    }
}
