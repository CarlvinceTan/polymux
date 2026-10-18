import XCTest
@testable import PolymuxKit

final class DictationTests: XCTestCase {
    func testPartialsReplaceAtCursor() throws {
        var draft = DictationDraft()
        let first = try XCTUnwrap(draft.update("hi", start: 0, final: false, text: "before after",
                                               selection: NSRange(location: 7, length: 0)))
        XCTAssertEqual(first.text, "before hiafter")
        let final = try XCTUnwrap(draft.update("hello ", start: 0, final: true, text: first.text,
                                               selection: NSRange(location: first.cursor, length: 0)))
        XCTAssertEqual(final.text, "before hello after")
        XCTAssertNil(draft.partial)
        XCTAssertNil(draft.update("hello ", start: 0, final: true, text: final.text,
                                  selection: NSRange(location: final.cursor, length: 0)))
        XCTAssertEqual(draft.update("again ", start: 1, final: true, text: final.text,
                                    selection: NSRange(location: final.cursor, length: 0))?.text,
                       "before hello again after")
    }

    func testSelectionAndUnicode() {
        var draft = DictationDraft()
        let text = "👋 old end"
        let range = NSRange(text.range(of: "old")!, in: text)
        XCTAssertEqual(draft.update("new", start: 0, final: false, text: text, selection: range)?.text, "👋 new end")
    }

    func testTypedEditsPreserved() {
        var draft = DictationDraft()
        let first = draft.update("hello", start: 0, final: false, text: "end", selection: NSRange(location: 0, length: 0))!
        draft.edit(from: first.text, to: "X" + first.text)
        XCTAssertEqual(draft.update("hi", start: 0, final: false, text: "X" + first.text,
                                    selection: NSRange(location: 0, length: 0))?.text, "Xhiend")
        draft.edit(from: "Xhiend", to: "Xtypedend")
        XCTAssertNil(draft.update("revision", start: 0, final: true, text: "Xtypedend",
                                  selection: NSRange(location: 0, length: 0)))
    }

    func testStopKeepsPartialAndResetsSession() {
        var draft = DictationDraft()
        let result = draft.update("spoken", start: 0, final: false, text: "typed ", selection: NSRange(location: 6, length: 0))!
        draft.stop()
        XCTAssertEqual(result.text, "typed spoken")
        XCTAssertNil(draft.partial)
        XCTAssertEqual(draft.update(" more", start: 0, final: true, text: result.text,
                                    selection: NSRange(location: result.cursor, length: 0))?.text, "typed spoken more")
    }

    func testCommittedSegmentsIgnoreLateRevisions() throws {
        var draft = DictationDraft()
        let first = try XCTUnwrap(draft.update("one ", start: 0, final: true, text: "typed ",
                                               selection: NSRange(location: 6, length: 0)))
        let second = try XCTUnwrap(draft.update("two", start: 1, final: false, text: first.text,
                                                selection: NSRange(location: first.cursor, length: 0)))
        XCTAssertNil(draft.update("late one ", start: 0, final: true, text: second.text,
                                  selection: NSRange(location: second.cursor, length: 0)))
        XCTAssertEqual(draft.update("two!", start: 1, final: true, text: second.text,
                                    selection: NSRange(location: second.cursor, length: 0))?.text, "typed one two!")
    }

    func testEmptyFinalRetractsPartialWithoutClearingDraft() throws {
        var draft = DictationDraft()
        let partial = try XCTUnwrap(draft.update("wrong", start: 0, final: false, text: "typed end",
                                                 selection: NSRange(location: 6, length: 0)))
        let final = try XCTUnwrap(draft.update("", start: 0, final: true, text: partial.text,
                                               selection: NSRange(location: partial.cursor, length: 0)))
        XCTAssertEqual(final.text, "typed end")
        XCTAssertEqual(final.cursor, 6)
        XCTAssertNil(draft.partial)
        XCTAssertNil(draft.update("late", start: 0, final: true, text: final.text,
                                  selection: NSRange(location: final.cursor, length: 0)))
    }
}
