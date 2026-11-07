import SwiftUI
import PolymuxKit

/// Overlapping avatars for a few people.
private struct HubFaces: View {
    let people: [HubMember]
    let client: APIClient
    var size: CGFloat = 18

    var body: some View {
        HStack(spacing: -size * 0.35) {
            ForEach(people.prefix(4), id: \.id) { person in
                HubAvatar(name: person.name, uri: person.avatar, client: client, size: size)
                    .background(Color(white: 0.86), in: HubAvatar.shape(size))
                    .padding(1)
                    .background(Color(white: 0.5), in: HubAvatar.shape(size + 2))
            }
        }
    }
}

/// Who's behind a reaction or a read mark, on dark so it stands off any bubble.
private struct HubPeopleTip: View {
    let people: [HubMember]
    let text: String
    let client: APIClient

    var body: some View {
        HStack(spacing: 6) {
            HubFaces(people: people, client: client)
            Text(text).font(.caption).foregroundStyle(.white).lineLimit(1)
        }
        .padding(.leading, 4).padding(.trailing, 10).padding(.vertical, 4)
        .background(Color(white: 0.18), in: Capsule())
        .fixedSize()
        .allowsHitTesting(false)
        .accessibilityHidden(true)
        .transition(.opacity.combined(with: .scale(scale: 0.92, anchor: .bottom)))
    }
}

/// A short spray of the emoji just added.
private struct HubBurst: View {
    let emoji: String
    @State private var out = false

    var body: some View {
        ZStack {
            ForEach(0..<5, id: \.self) { index in
                let angle = Double(index) / 4 * .pi
                Text(emoji).font(.caption2)
                    .offset(x: out ? cos(angle) * 24 : 0, y: out ? -sin(angle) * 22 - 14 : 0)
                    .opacity(out ? 0 : 1)
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
        .onAppear { withAnimation(.easeOut(duration: 0.55)) { out = true } }
    }
}

/// A message's reactions: each pill says who on hover or long press, and a tap toggles yours.
struct HubReactionBar: View {
    let reactions: [HubReaction]
    let outgoing: Bool
    let client: APIClient
    let react: (String) -> Void
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var tip: String?
    @State private var burst: String?
    @State private var adding = false

    private static let height: CGFloat = 26
    private static let quick = ["👍", "❤️", "😂", "🎉", "😮", "🔥", "👀", "🙏"]

    var body: some View {
        HStack(spacing: 6) {
            ForEach(reactions, id: \.key) { reaction in
                pill(reaction).transition(.scale(scale: 0.7).combined(with: .opacity))
            }
            addButton
        }
        .animation(reduceMotion ? nil : .spring(duration: 0.28), value: reactions)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.12), value: tip)
        .task(id: burst) {
            guard burst != nil else { return }
            try? await Task.sleep(for: .milliseconds(600))
            burst = nil
        }
    }

    private func pill(_ reaction: HubReaction) -> some View {
        Button {
            if !reaction.mine, !reduceMotion { burst = reaction.key }
            react(reaction.key)
        } label: {
            HStack(spacing: 4) {
                Text(reaction.key)
                Text(String(reaction.count)).monospacedDigit().contentTransition(.numericText())
            }
            .font(.caption.weight(.medium))
            .foregroundStyle(reaction.mine ? Color("OnAccent") : Color.primary.opacity(0.7))
            .padding(.horizontal, 9)
            .frame(height: Self.height)
            .background(reaction.mine ? AnyShapeStyle(Color("Unread")) : AnyShapeStyle(.primary.opacity(0.07)), in: Capsule())
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .onHover { inside in
            if inside { tip = reaction.key } else if tip == reaction.key { tip = nil }
        }
        .simultaneousGesture(LongPressGesture(minimumDuration: 0.35).onEnded { _ in tip = reaction.key })
        .overlay(alignment: outgoing ? .bottomTrailing : .bottomLeading) {
            if tip == reaction.key, !reaction.people.isEmpty {
                HubPeopleTip(people: reaction.people, text: HubReactions.names(reaction.people), client: client)
                    .padding(.bottom, Self.height + 6)
            }
        }
        .overlay {
            if burst == reaction.key { HubBurst(emoji: reaction.key) }
        }
        .accessibilityLabel("Reaction \(reaction.key), \(reaction.count), \(HubReactions.names(reaction.people))")
        .accessibilityValue(reaction.mine ? "Yours" : "")
    }

    private var addButton: some View {
        Button { adding = true } label: {
            Image(systemName: "face.smiling").font(.caption).foregroundStyle(.muted)
                .frame(width: Self.height, height: Self.height)
                .background(.primary.opacity(0.07), in: Circle())
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Add reaction")
        .popover(isPresented: $adding) {
            HStack(spacing: 2) {
                ForEach(Self.quick, id: \.self) { emoji in
                    HubQuickEmoji(emoji: emoji, used: reactions.contains { $0.key == emoji && $0.mine }) {
                        adding = false
                        if !reduceMotion { burst = emoji }
                        react(emoji)
                    }
                }
            }
            .padding(6)
            .presentationCompactAdaptation(.popover)
        }
    }
}

/// One choice in the quick bar. A dot marks a reaction you've already made.
private struct HubQuickEmoji: View {
    let emoji: String
    let used: Bool
    let action: () -> Void
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            VStack(spacing: 2) {
                Text(emoji).font(.title3).scaleEffect(hovering && !reduceMotion ? 1.25 : 1, anchor: .bottom)
                Circle().fill(used ? Color("Unread") : .clear).frame(width: 4, height: 4)
            }
            .frame(width: 36)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .animation(reduceMotion ? nil : .spring(duration: 0.2), value: hovering)
        .accessibilityLabel("React \(emoji)")
        .accessibilityValue(used ? "Yours" : "")
    }
}

/// Who has reached this message, for each person's newest one.
struct HubReaders: View {
    let people: [HubMember]
    let outgoing: Bool
    let client: APIClient
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var showing = false

    private var text: String { "Read by \(HubReactions.names(people))" }

    var body: some View {
        Button { showing.toggle() } label: {
            HubFaces(people: people, client: client, size: 15).contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { showing = $0 }
        // Beside the faces, since the reactions sit directly above them.
        .overlay(alignment: outgoing ? .trailing : .leading) {
            if showing {
                HubPeopleTip(people: people, text: text, client: client)
                    .padding(outgoing ? .trailing : .leading, CGFloat(min(people.count, 4)) * 12 + 14)
            }
        }
        .animation(reduceMotion ? nil : .easeOut(duration: 0.12), value: showing)
        .accessibilityLabel(text)
    }
}
