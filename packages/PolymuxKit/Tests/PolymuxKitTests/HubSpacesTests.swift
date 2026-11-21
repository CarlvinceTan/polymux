import Foundation
import Testing
@testable import PolymuxKit

private func chat(_ id: String, name: String? = nil, space: Bool = false, parents: [String] = [], at: Int64 = 0, unread: Int64 = 0, preview: String = "") -> HubChat {
    HubChat(id: id, platform: "slack", name: name ?? id, avatar: "", group: true, space: space, parents: parents, preview: preview, at: at, unread: unread)
}

private func message(_ id: String, text: String = "", kind: String = HubMessageKind.text, sender: String = "@a:x", name: String = "Ana",
                     attachment: HubAttachment? = nil) -> HubMessage {
    HubMessage(id: id, chat: "!c", sender: sender, senderName: name, mine: false, text: text, at: 1, kind: kind, attachment: attachment)
}

@Suite struct HubSpacesTests {
    private let chats = [chat("acme", name: "Acme", space: true), chat("general", parents: ["acme"], at: 30, unread: 2),
                         chat("random", parents: ["acme"], at: 10, unread: 3), chat("loose", at: 20),
                         chat("filed", parents: ["hidden-envelope"], at: 5)]

    @Test func spaceFoldsItsChatsIntoOneRow() {
        let rows = HubSpaces.root(chats)
        #expect(rows.map(\.id) == ["acme", "loose", "filed"])
        #expect(rows[0].unread == 5 && rows[0].at == 30 && rows[0].preview == "general, random")
    }

    @Test func searchForAChildKeepsItsSpaceReachable() {
        #expect(HubSpaces.root(chats, query: "random").map(\.id) == ["acme"])
        #expect(HubSpaces.root(chats, query: "nothing").isEmpty)
    }

    @Test func childrenStayBehindTheirSpace() {
        #expect(HubSpaces.children(of: "acme", in: chats).map(\.id) == ["general", "random"])
        #expect(HubSpaces.children(of: "acme", in: chats, query: "gen").map(\.id) == ["general"])
    }
}

@Suite struct HubSearchTests {
    private let messages = [
        message("$1", text: "Deck is at https://www.example.com/deck, see (https://a.test/b)."),
        message("$2", kind: HubMessageKind.image, attachment: HubAttachment(uri: "mxc://x/1", name: "chart.png", mime: "image/png", size: 1)),
        message("$3", kind: HubMessageKind.file, attachment: HubAttachment(uri: "mxc://x/2", name: "q3.pdf", mime: "application/pdf", size: 1)),
        message("$4", text: "system", kind: HubMessageKind.notice),
        message("$5", text: "lunch?", name: "Priya"),
    ]

    @Test func filtersSplitWhatAChatShared() {
        #expect(HubSearch.results(messages, query: "", filter: .media).map(\.message.id) == ["$2"])
        #expect(HubSearch.results(messages, query: "", filter: .files).map(\.title) == ["q3.pdf"])
        #expect(HubSearch.results(messages, query: "", filter: .links).map(\.title) == ["example.com", "a.test"])
        #expect(HubSearch.results(messages, query: "", filter: .messages).map(\.message.id) == ["$5", "$1"])
    }

    @Test func queryReachesSendersAndFileTypes() {
        #expect(HubSearch.results(messages, query: "priya", filter: .all).map(\.message.id) == ["$5"])
        #expect(HubSearch.results(messages, query: "application/pdf", filter: .all).map(\.message.id) == ["$3"])
        #expect(HubSearch.results(messages, query: "system", filter: .all).isEmpty)
    }

    @Test func linksLoseSentencePunctuation() {
        #expect(HubSearch.links(in: "see https://a.test/x_(y), and (http://b.test).") == ["https://a.test/x_(y)", "http://b.test"])
    }
}

@Suite struct HubMentionTests {
    private let members = [HubMember(id: "@wa_1:x", name: "José Núñez", avatar: ""), HubMember(id: "@wa_2:x", name: "José Núñez", avatar: ""),
                           HubMember(id: "@wa_3:x", name: "Sam", avatar: ""), HubMember(id: "@wa_+65123:x", name: "🙂", avatar: "")]

    @Test func tokenIsTheHandleAtTheCaret() {
        #expect(HubMentionText.token(in: "hi @sa", cursor: 6) == HubToken(start: 3, end: 6, query: "sa"))
        #expect(HubMentionText.token(in: "@", cursor: 1) == HubToken(start: 0, end: 1, query: ""))
        #expect(HubMentionText.token(in: "(@sa", cursor: 4)?.query == "sa")
        #expect(HubMentionText.token(in: "mail a@b.test", cursor: 13) == nil)
        #expect(HubMentionText.token(in: "@sam and more", cursor: 13) == nil)
        #expect(HubMentionText.token(in: "@sam and more", cursor: 4)?.query == "sam")
    }

    @Test func handlesAreReadableAndUnique() {
        #expect(HubMentionText.handle(members[0]) == "@jose_nunez")
        #expect(HubMentionText.handle(HubMember(id: "@a:x", name: "J. R. Smith", avatar: "")) == "@j_r_smith")
        #expect(HubMentionText.handle(HubMember(id: "@a:x", name: "mary-jane", avatar: "")) == "@mary-jane")
        #expect(HubMentionText.handle(members[3]) == "@65123")
        let found = HubMentionText.suggestions(HubToken(start: 0, end: 1, query: "jo"), members: members, group: true)
        #expect(found.map(\.value) == ["@jose_nunez", "@jose_nunez_2"])
    }

    @Test func everyoneIsOnlyOfferedInGroups() {
        let token = HubToken(start: 0, end: 1, query: "")
        #expect(HubMentionText.suggestions(token, members: members, group: true).first?.value == "@everyone")
        #expect(HubMentionText.suggestions(token, members: members, group: false).first?.value != "@everyone")
    }

    @Test func replacingKeepsTheRestOfTheDraft() {
        let middle = HubMentionText.replace("hi @sa, ok", HubToken(start: 3, end: 6, query: "sa"), with: "@sam")
        #expect(middle.text == "hi @sam, ok" && middle.caret == 7)
        let end = HubMentionText.replace("@sa", HubToken(start: 0, end: 3, query: "sa"), with: "@sam")
        #expect(end.text == "@sam " && end.caret == 5)
    }

    @Test func mentionsComeFromTheFinalText() {
        let sent = HubMentionText.mentions(in: "@sam and @jose_nunez_2 and sam@sam.test", members: members, group: false)
        #expect(sent?.users.map(\.id) == ["@wa_2:x", "@wa_3:x"])
        #expect(HubMentionText.mentions(in: "@samantha", members: members, group: true) == nil)
        #expect(HubMentionText.mentions(in: "@everyone", members: members, group: false) == nil)
        #expect(HubMentionText.mentions(in: "@everyone", members: members, group: true)?.everyone == true)
    }

    @Test func peopleWhoWroteCanBeMentionedWhenTheListFails() {
        let merged = HubMentionText.merge([], [message("$1", sender: "@wa_9:x", name: "Zed"), message("$2", sender: "@wa_9:x", name: "Zed")])
        #expect(merged.map(\.id) == ["@wa_9:x"])
    }
}

@Suite struct HubReactionTests {
    private func person(_ name: String) -> HubMember { HubMember(id: "@" + name, name: name, avatar: "") }

    @Test func namesReadAsASentence() {
        #expect(HubReactions.names([person("Jonah")]) == "Jonah")
        #expect(HubReactions.names([person("Sam"), person("Noor")]) == "Sam and Noor")
        #expect(HubReactions.names(["You", "Maya", "Leo"].map(person)) == "You, Maya and Leo")
        #expect(HubReactions.names(["You", "Maya", "Leo", "Zed"].map(person)) == "You, Maya, Leo and 1 other")
        #expect(HubReactions.names(["You", "Maya", "Leo", "Zed", "Ana"].map(person)) == "You, Maya, Leo and 2 others")
    }

    @Test func tappingAddsAndRemovesYou() {
        let others = [HubReaction(key: "👍", count: 1, mine: false, people: [person("Sam")])]
        let added = HubReactions.toggled(others, key: "👍")
        #expect(added[0].mine && added[0].count == 2 && added[0].people.map(\.name) == ["You", "Sam"])
        #expect(HubReactions.toggled(added, key: "👍") == others)
        let fresh = HubReactions.toggled(others, key: "🔥")
        #expect(fresh.count == 2 && fresh[1].people.map(\.name) == ["You"])
        #expect(HubReactions.toggled(fresh, key: "🔥") == others)
    }
}
