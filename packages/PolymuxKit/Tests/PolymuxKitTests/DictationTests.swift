import Foundation
import Testing
@testable import PolymuxKit

private func cursor(_ location: Int) -> NSRange { NSRange(location: location, length: 0) }

@Suite struct DictationTests {
    @Test func partialsReplaceAtCursor() throws {
        var draft = DictationDraft()
        let firstResult = draft.update("hi", start: 0, final: false, text: "before after", selection: cursor(7))
        let first = try #require(firstResult)
        #expect(first.text == "before hiafter")
        let finalResult = draft.update("hello ", start: 0, final: true, text: first.text, selection: cursor(first.cursor))
        let final = try #require(finalResult)
        #expect(final.text == "before hello after")
        #expect(draft.partial == nil)
        let repeated = draft.update("hello ", start: 0, final: true, text: final.text, selection: cursor(final.cursor))
        #expect(repeated == nil)
        let next = draft.update("again ", start: 1, final: true, text: final.text, selection: cursor(final.cursor))
        #expect(next?.text == "before hello again after")
    }

    @Test func selectionAndUnicode() {
        var draft = DictationDraft()
        let text = "👋 old end"
        let range = NSRange(text.range(of: "old")!, in: text)
        let result = draft.update("new", start: 0, final: false, text: text, selection: range)
        #expect(result?.text == "👋 new end")
    }

    @Test func typedEditsPreserved() {
        var draft = DictationDraft()
        let first = draft.update("hello", start: 0, final: false, text: "end", selection: cursor(0))!
        draft.edit(from: first.text, to: "X" + first.text)
        let revised = draft.update("hi", start: 0, final: false, text: "X" + first.text, selection: cursor(0))
        #expect(revised?.text == "Xhiend")
        draft.edit(from: "Xhiend", to: "Xtypedend")
        let late = draft.update("revision", start: 0, final: true, text: "Xtypedend", selection: cursor(0))
        #expect(late == nil)
    }

    @Test func stopKeepsPartialAndResetsSession() {
        var draft = DictationDraft()
        let result = draft.update("spoken", start: 0, final: false, text: "typed ", selection: cursor(6))!
        draft.stop()
        #expect(result.text == "typed spoken")
        #expect(draft.partial == nil)
        let more = draft.update(" more", start: 0, final: true, text: result.text, selection: cursor(result.cursor))
        #expect(more?.text == "typed spoken more")
    }

    @Test func committedSegmentsIgnoreLateRevisions() throws {
        var draft = DictationDraft()
        let firstResult = draft.update("one ", start: 0, final: true, text: "typed ", selection: cursor(6))
        let first = try #require(firstResult)
        let secondResult = draft.update("two", start: 1, final: false, text: first.text, selection: cursor(first.cursor))
        let second = try #require(secondResult)
        let late = draft.update("late one ", start: 0, final: true, text: second.text, selection: cursor(second.cursor))
        #expect(late == nil)
        let final = draft.update("two!", start: 1, final: true, text: second.text, selection: cursor(second.cursor))
        #expect(final?.text == "typed one two!")
    }

    @Test func emptyFinalRetractsPartialWithoutClearingDraft() throws {
        var draft = DictationDraft()
        let partialResult = draft.update("wrong", start: 0, final: false, text: "typed end", selection: cursor(6))
        let partial = try #require(partialResult)
        let finalResult = draft.update("", start: 0, final: true, text: partial.text, selection: cursor(partial.cursor))
        let final = try #require(finalResult)
        #expect(final.text == "typed end")
        #expect(final.cursor == 6)
        #expect(draft.partial == nil)
        let late = draft.update("late", start: 0, final: true, text: final.text, selection: cursor(final.cursor))
        #expect(late == nil)
    }
}
