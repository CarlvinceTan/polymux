import Testing
@testable import PolymuxKit

struct ReactionTests {
    @Test(arguments: ["👍", "❤️", "‼️", "🦁", "👍🏽", "👩🏽‍💻", "👨‍👩‍👧‍👦", "🇦🇺", "1️⃣"])
    func emojiAccepted(emoji: String) {
        #expect(Reaction.isEmoji(emoji))
    }

    @Test(arguments: ["", "hello", "a", "1", "$", "👍👍", "👍 ", "👍\n", "👍\u{0301}", "👍" + String(repeating: "\u{FE0F}", count: 50)])
    func textRejected(text: String) {
        #expect(!Reaction.isEmoji(text))
    }

    @Test func recentChoices() {
        #expect(Reaction.suggestions(recent: ["👩🏽‍💻", "👍", "👩🏽‍💻", "🦁", "not emoji", "🎉"] + Reaction.defaultRecents) == ["👩🏽‍💻", "🦁", "🎉", "😍", "😒"])
    }
}
