import SwiftUI
import PolymuxKit

struct ReactionMenu: View {
    @Environment(\.reactionPicker) private var more
    @Environment(\.colorScheme) private var scheme
    @AppStorage("recentReactions") private var recent = ""
    let reactions: [Reaction]
    let react: (String) -> Void

    /// Emoji recents macOS shares across Messages, stickers and the emoji picker.
    private static var systemRecents: [String] {
        #if os(macOS)
        let order = UserDefaults(suiteName: "com.apple.EmojiPreferences")?.stringArray(forKey: "com.apple.stickers.recency.order") ?? []
        return order.compactMap { $0.hasPrefix("emoji:") ? String($0.dropFirst(6)) : nil }
        #else
        return []
        #endif
    }

    var body: some View {
        ControlGroup("Reactions") {
            ForEach(Reaction.emojis, id: \.self) { emoji in
                choice(emoji)
            }
        }
        .controlGroupStyle(.palette)
        .labelsHidden()
        ControlGroup("Recent reactions") {
            let picks = recent.components(separatedBy: ",")
            ForEach(Reaction.suggestions(recent: picks + Self.systemRecents + Reaction.defaultRecents), id: \.self) { emoji in
                choice(emoji)
            }
            Button { more() } label: {
                Label { Text("More reactions") } icon: { ReactionIcons.symbol("face.smiling.inverse", scheme: scheme) }
            }
            .accessibilityLabel("More reactions")
        }
        .controlGroupStyle(.palette)
        .labelsHidden()
        Divider()
    }

    private func choice(_ emoji: String) -> some View {
        Toggle(isOn: Binding(
            get: { reactions.contains { $0.emoji == emoji && $0.agentID == nil } },
            set: { _ in
                recent = Reaction.suggestions(recent: [emoji] + recent.components(separatedBy: ",")).joined(separator: ",")
                react(emoji)
            }
        )) {
            Label { Text("React \(emoji)") } icon: { ReactionIcons.image(emoji) }
        }
        .accessibilityLabel("React \(emoji)")
        .menuActionDismissBehavior(.enabled)
    }
}

@MainActor
private enum ReactionIcons {
    private static var cache: [String: Image] = [:]

    static func image(_ emoji: String) -> Image {
        render(emoji, Text(emoji).font(.system(size: 24)))
    }

    /// Same 32pt frame as the emoji, so palette rows keep equal item widths.
    /// Menus don't tint custom images, so it's drawn in the scheme's primary colour.
    static func symbol(_ name: String, scheme: ColorScheme) -> Image {
        render("\(name) \(scheme)", Image(systemName: name).font(.system(size: 20))
            .foregroundStyle(.primary).environment(\.colorScheme, scheme))
    }

    private static func render(_ key: String, _ content: some View) -> Image {
        if let image = cache[key] { return image }
        let renderer = ImageRenderer(content: content.frame(width: 32, height: 32))
        renderer.scale = 2
        #if os(macOS)
        let image = Image(nsImage: renderer.nsImage ?? NSImage()).renderingMode(.original)
        #else
        let image = Image(uiImage: renderer.uiImage ?? UIImage()).renderingMode(.original)
        #endif
        cache[key] = image
        return image
    }
}

struct ReactionsView: View {
    let reactions: [Reaction]
    let agents: [Agent]
    let react: (String) -> Void

    var body: some View {
        ViewThatFits(in: .horizontal) {
            row
            ScrollView(.horizontal) { row }.scrollIndicators(.hidden)
        }
    }

    private var row: some View {
        HStack(spacing: 6) {
            ForEach(Array(Set(reactions.map(\.emoji))).sorted(), id: \.self) { emoji in
                let matching = reactions.filter { $0.emoji == emoji }
                let authors = matching.map { reaction in
                    reaction.agentID.map { id in agents.first { $0.id == id }?.name ?? "Bot" } ?? "You"
                }.joined(separator: ", ")
                Toggle("\(emoji) \(matching.count)", isOn: Binding(
                    get: { matching.contains { $0.agentID == nil } },
                    set: { _ in react(emoji) }
                ))
                .toggleStyle(.button)
                .controlSize(.small)
                .font(.caption)
                .help(authors)
                .accessibilityLabel("Reaction \(emoji), \(matching.count), \(authors)")
            }
        }
    }
}
