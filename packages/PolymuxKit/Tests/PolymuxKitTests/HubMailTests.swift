import Foundation
import Testing
@testable import PolymuxKit

private func envelope(_ id: Int64 = 1, subject: String = "Hello", answered: Bool = false) -> HubMailEnvelope {
    HubMailEnvelope(id: id, account: "a", folder: "INBOX", from: "Priya", address: "priya@example.test", subject: subject, preview: "", at: 0,
                    seen: true, flagged: false, attachments: 0, answered: answered, draft: false, importance: HubMailEnvelopeImportance.normal)
}

private func letter(html: String = "", files: [HubMailFile] = [], text: String = "line one\nline two",
                    to: [HubMailAddress] = [], cc: [HubMailAddress] = []) -> HubMailMessage {
    HubMailMessage(envelope: envelope(), to: to, cc: cc, bcc: [], text: text, html: html, files: files, messageId: "<m@x>", references: [])
}

private func file(_ part: String, _ name: String, mime: String, cid: String = "") -> HubMailFile {
    HubMailFile(part: part, name: name, mime: mime, size: 1, contentId: cid, inline: !cid.isEmpty)
}

@Suite struct HubMailFormatTests {
    @Test func signatureMeetsTheTextOnlyOnTheWayOut() {
        #expect(HubMailFormat.body("Hello\n\n", signature: "Carl") == "Hello\n\nCarl")
        #expect(HubMailFormat.body("", signature: "Carl") == "Carl")
        #expect(HubMailFormat.body("Hello", signature: "  ") == "Hello")
    }

    @Test func htmlIsOnlyMadeWhenSomethingNeedsIt() {
        #expect(HubMailFormat.html("Hello", signature: "") == nil)
        let signed = HubMailFormat.html("a<b\nc", signature: "<b>Carl</b>")
        #expect(signed == "<div>a&lt;b<br>c</div><br><br><div data-polymux-signature=\"true\"><b>Carl</b></div>")
        #expect(HubMailFormat.html("", signature: "<b>Carl</b>") == "<div data-polymux-signature=\"true\"><b>Carl</b></div>")
    }

    @Test func filesSitWhereTheyWerePut() {
        let files = [HubMailPiece(id: "1", name: "b.pdf", contentID: "b@x", offset: 5), HubMailPiece(id: "2", name: "a<.pdf", contentID: "a@x", offset: 2)]
        let html = HubMailFormat.html("Hello there", signature: "", files: files)
        #expect(html == "<div>He<div><a href=\"cid:a@x\">a&lt;.pdf</a></div>llo<div><a href=\"cid:b@x\">b.pdf</a></div> there</div>")
    }

    @Test func editingARunOnlyMovesTheFilesAfterIt() {
        var draft = HubMailCompose(account: "a")
        draft.text = "abcdef"
        draft.files = [HubMailPiece(id: "1", name: "1", contentID: "1", offset: 2), HubMailPiece(id: "2", name: "2", contentID: "2", offset: 4)]
        #expect(HubMailFormat.segments(draft.text, files: draft.files).map(\.text) == ["ab", "cd", "ef"])
        HubMailFormat.edit(&draft, segment: 1, text: "cdXX")
        #expect(draft.text == "abcdXXef")
        #expect(draft.files.map(\.offset) == [2, 6])
        HubMailFormat.edit(&draft, segment: 0, text: "")
        #expect(draft.text == "cdXXef")
        #expect(draft.files.map(\.offset) == [0, 4])
    }

    @Test func subjectsAreOnlyPrefixedOnce() {
        #expect(HubMailFormat.prefixed("Hello", mode: .reply) == "Re: Hello")
        #expect(HubMailFormat.prefixed("RE: Hello", mode: .replyAll) == "RE: Hello")
        #expect(HubMailFormat.prefixed("Hello", mode: .forward) == "Fwd: Hello")
    }

    @Test func aReplyQuotesEveryLine() {
        let lines = HubMailFormat.quote(letter()).components(separatedBy: "\n")
        #expect(lines.count == 3)
        #expect(lines[0].hasSuffix("Priya wrote:"))
        #expect(lines[1] == "> line one")
    }

    @Test func replyAllLeavesOutTheMailboxAndRepeats() {
        let message = letter(to: [HubMailAddress(name: "", address: "ME@example.test"), HubMailAddress(name: "", address: "sam@example.test")],
                             cc: [HubMailAddress(name: "", address: "sam@example.test")])
        #expect(HubMailFormat.recipients(message, all: false, mine: "me@example.test") == ["priya@example.test"])
        #expect(HubMailFormat.recipients(message, all: true, mine: "me@example.test") == ["priya@example.test", "sam@example.test"])
    }

    @Test func conversationsNeedAReply() {
        #expect(HubMailFormat.baseSubject("Re: Fwd: FW: Hello ") == "hello")
        let alerts = [envelope(1, subject: "Disk full"), envelope(2, subject: "Disk full")]
        #expect(HubMailFormat.chains(alerts)["disk full"] == 1)
        #expect(HubMailFormat.chain(alerts, subject: "Disk full").isEmpty)
        let talk = [envelope(1, subject: "Plan"), envelope(2, subject: "Re: Plan")]
        #expect(HubMailFormat.chains(talk)["plan"] == 2)
        #expect(HubMailFormat.chain(talk, subject: "Plan").count == 2)
    }
}

@Suite struct HubMailPageTests {
    @Test func inlineImagesAndLinksResolveWhereAuthored() {
        let message = letter(html: "<p>Hi</p><img src=\"cid:logo@x\" width=\"40\"><a href=\"cid:q3@x\">q3.pdf</a><img src=\"cid:gone\"><img src=\"http://track.example/p.gif\"><img src=\"https://cdn.example/a.png\">",
                             files: [file("2", "logo.png", mime: "image/png", cid: "logo@x"), file("3", "q3.pdf", mime: "application/pdf", cid: "q3@x"), file("4", "extra.zip", mime: "application/zip")])
        let page = HubMailPage.document(message, dark: false)
        #expect(page.contains("<img src=\"hub-part://file/2\" width=\"40\">"))
        #expect(page.contains("hub-open://file/3"))
        #expect(page.contains("application/pdf"))
        #expect(!page.contains("cid:gone"))
        #expect(!page.contains("track.example"))
        #expect(page.contains("https://cdn.example/a.png"))
        // The part the body never placed follows it, and placed ones aren't repeated.
        #expect(page.contains("hub-open://file/4"))
        #expect(page.components(separatedBy: "hub-part://file/2").count == 2)
    }

    @Test func plainMailIsEscapedAndFollowsTheAppearance() {
        let page = HubMailPage.document(letter(text: "<script>x</script>"), dark: true)
        #expect(page.contains("&lt;script&gt;"))
        #expect(!page.contains("<script>"))
        #expect(page.contains("color-scheme:dark"))
    }

    @Test func designedMailKeepsItsWhitePage() {
        #expect(HubMailPage.document(letter(html: "<p>Hi</p>"), dark: true).contains("background:#fff"))
    }

    @Test func aNonImageInAnImageTagBecomesACard() {
        let page = HubMailPage.document(letter(html: "<img src=\"cid:a@x\">", files: [file("2", "a.pdf", mime: "application/pdf", cid: "a@x")]), dark: false)
        #expect(page.contains("hub-open://file/2"))
        #expect(!page.contains("<img"))
    }
}
