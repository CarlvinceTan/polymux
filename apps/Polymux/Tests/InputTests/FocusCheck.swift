import AppKit
import SwiftUI

private final class TestWindow: NSWindow {
    var delivered: NSEvent?
    var onEvent: ((NSEvent) -> Void)?

    // Hidden windows do not dispatch clicks to controls normally.
    override func sendEvent(_ event: NSEvent) {
        delivered = event
        onEvent?(event)
    }
}

private final class TestDelegate: NSObject, NSTextFieldDelegate {
    var ended = 0
    func controlTextDidEndEditing(_ notification: Notification) { ended += 1 }
}

private final class VetoEditor: NSTextView {
    var canResign = false
    override func resignFirstResponder() -> Bool { canResign }
}

@MainActor private final class Fixture {
    let window = TestWindow(contentRect: NSRect(x: 0, y: 0, width: 480, height: 360),
                            styleMask: .borderless, backing: .buffered, defer: false)
    let root = NSView(frame: NSRect(x: 0, y: 0, width: 480, height: 360))
    let anchor = InputAnchor(frame: .zero)

    init() {
        window.isReleasedWhenClosed = false
        window.contentView = root
        root.addSubview(anchor)
    }

    func close() {
        anchor.detach()
        window.close()
    }

    @discardableResult func click(_ point: NSPoint, right: Bool = false) -> NSEvent {
        let event = NSEvent.mouseEvent(with: right ? .rightMouseDown : .leftMouseDown,
            location: point, modifierFlags: [], timestamp: 0,
            windowNumber: window.windowNumber, context: nil, eventNumber: 0,
            clickCount: 1, pressure: 1)!
        NSApplication.shared.sendEvent(event)
        return event
    }

    func add(_ field: NSTextField, y: CGFloat = 60) {
        field.frame = NSRect(x: 200, y: y, width: 180, height: 28)
        field.isEditable = true
        field.isSelectable = true
        root.addSubview(field)
    }

    func focus(_ field: NSTextField) -> NSTextView {
        field.selectText(nil)
        guard let editor = field.currentEditor() as? NSTextView,
              window.firstResponder === editor else {
            fatalError("Fixture could not focus its native field editor")
        }
        return editor
    }
}

@main private struct FocusCheck {
    @MainActor private static var count = 0

    @MainActor static func main() {
        NSApplication.shared.setActivationPolicy(.prohibited)
        fields()
        editors()
        accessories()
        windows()
        search()
        print("Input focus: \(count) checks pass")
    }

    @MainActor private static func check(_ condition: @autoclosure () -> Bool, _ message: String) {
        guard condition() else { print("FAIL: \(message)"); exit(1) }
        count += 1
    }

    @MainActor private static func fields() {
        let fixture = Fixture()
        defer { fixture.close() }
        let field = NSTextField()
        let delegate = TestDelegate()
        field.delegate = delegate
        fixture.add(field)
        field.stringValue = "Draft"
        let editor = fixture.focus(field)
        fixture.click(NSPoint(x: 280, y: 74))
        check(fixture.window.firstResponder === editor, "Clicking inside a text field must keep focus")

        let padding = NSPoint(x: field.frame.minX + 1, y: field.frame.midY)
        check(!editor.visibleRect.contains(editor.convert(padding, from: nil)), "Fixture must exercise field padding outside the editor")
        fixture.click(padding)
        check(fixture.window.firstResponder === editor, "Field padding belongs to the input, not the background")
        let event = fixture.click(NSPoint(x: 40, y: 74))
        check(fixture.window.firstResponder === fixture.anchor, "Clicking a field label must clear focus")
        check(fixture.window.delivered === event, "The original click must still reach its target window")
        check(delegate.ended == 1, "Native editing-ended notifications must still fire")
        check(field.stringValue == "Draft", "Click-away must preserve the draft")

        _ = fixture.focus(field)
        fixture.click(NSPoint(x: 420, y: 160), right: true)
        check(fixture.window.firstResponder === fixture.anchor, "Right-clicking outside must clear focus")

        let secure = NSSecureTextField()
        fixture.add(secure, y: 110)
        secure.stringValue = "test-key"
        let secureEditor = fixture.focus(secure)
        fixture.click(NSPoint(x: 280, y: 124))
        check(fixture.window.firstResponder === secureEditor, "Secure inputs must keep focus when clicked inside")
        fixture.click(NSPoint(x: 40, y: 124))
        check(fixture.window.firstResponder === fixture.anchor, "Secure inputs must clear focus on click-away")

        _ = fixture.focus(field)
        var blurredBeforeDelivery = false
        fixture.window.onEvent = { _ in
            blurredBeforeDelivery = fixture.window.firstResponder === fixture.anchor
            secure.selectText(nil)
        }
        fixture.click(NSPoint(x: 280, y: 124))
        check(blurredBeforeDelivery && secure.currentEditor() === fixture.window.firstResponder,
              "Switching inputs must blur the old input and forward the click to the next")
        fixture.window.onEvent = nil
        fixture.click(NSPoint(x: 280, y: 74))
        check(fixture.window.firstResponder === fixture.anchor, "Shared field editors must resolve their current owner")

        fixture.anchor.nextKeyView = field
        field.nextKeyView = secure
        secure.nextKeyView = field
        fixture.window.selectNextKeyView(fixture.anchor)
        check(field.currentEditor() === fixture.window.firstResponder, "Click-away must preserve keyboard entry into the input loop")
        fixture.window.selectKeyView(following: field)
        check(secure.currentEditor() === fixture.window.firstResponder, "Native key-view navigation must still switch between inputs")

        let label = NSTextField(labelWithString: "Read-only text")
        label.isSelectable = true
        label.frame = NSRect(x: 200, y: 210, width: 180, height: 28)
        fixture.root.addSubview(label)
        let labelEditor = fixture.focus(label)
        fixture.click(NSPoint(x: 40, y: 224))
        check(fixture.window.firstResponder === labelEditor, "Selectable labels must not be treated as editable inputs")
    }

    @MainActor private static func editors() {
        let fixture = Fixture()
        defer { fixture.close() }
        let scroll = NSScrollView(frame: NSRect(x: 200, y: 160, width: 180, height: 120))
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = false
        let editor = NSTextView(frame: NSRect(x: 0, y: 0, width: 140, height: 50))
        editor.string = "Multiline draft"
        scroll.documentView = editor
        fixture.root.addSubview(scroll)
        check(fixture.window.makeFirstResponder(editor), "Fixture must focus a multiline editor")
        fixture.click(scroll.convert(NSPoint(x: 20, y: 20), to: nil))
        check(fixture.window.firstResponder === editor, "Clicking inside a multiline editor must keep focus")
        fixture.click(scroll.convert(NSPoint(x: 170, y: 110), to: nil))
        check(fixture.window.firstResponder === editor, "The editor's scroll area must keep focus")
        fixture.click(NSPoint(x: 40, y: 200))
        check(fixture.window.firstResponder === fixture.anchor, "Multiline editors must clear focus on click-away")
        check(editor.string == "Multiline draft", "Multiline drafts must not be discarded")

        editor.isEditable = false
        editor.isSelectable = true
        check(fixture.window.makeFirstResponder(editor), "Fixture must focus selectable read-only text")
        fixture.click(NSPoint(x: 40, y: 200))
        check(fixture.window.firstResponder === editor, "Read-only text selection must not be treated as an input")

        let veto = VetoEditor(frame: NSRect(x: 200, y: 60, width: 180, height: 50))
        fixture.root.addSubview(veto)
        check(fixture.window.makeFirstResponder(veto), "Fixture must focus the vetoing editor")
        let event = fixture.click(NSPoint(x: 40, y: 74))
        check(fixture.window.firstResponder === veto && fixture.window.delivered === event,
              "A native refusal to resign must be respected without swallowing the click")
        veto.canResign = true

        let form = NSScrollView(frame: NSRect(x: 20, y: 20, width: 160, height: 300))
        let rows = NSView(frame: form.bounds)
        let nested = NSTextView(frame: NSRect(x: 20, y: 20, width: 120, height: 60))
        rows.addSubview(nested)
        form.documentView = rows
        fixture.root.addSubview(form)
        check(fixture.window.makeFirstResponder(nested), "Fixture must focus an input inside a scrolling form")
        fixture.click(form.convert(NSPoint(x: 80, y: 200), to: nil))
        check(fixture.window.firstResponder === fixture.anchor, "An enclosing form must not count as the input's scroll area")
    }

    @MainActor private static func accessories() {
        let fixture = Fixture()
        defer { fixture.close() }
        let field = NSTextField()
        fixture.add(field)
        let editor = fixture.focus(field)
        let container = NSView(frame: NSRect(x: 200, y: 110, width: 180, height: 80))
        let accessory = AccessoryAnchor(frame: container.bounds)
        container.addSubview(accessory)
        fixture.root.addSubview(container)
        fixture.click(NSPoint(x: 280, y: 150))
        check(fixture.window.firstResponder === editor, "Mention suggestions must retain input focus until their action")
        check(accessory.hitTest(NSPoint(x: 20, y: 20)) == nil, "Accessory markers must not intercept clicks")
        fixture.click(NSPoint(x: 40, y: 150))
        check(fixture.window.firstResponder === fixture.anchor, "Suggestions must not exempt unrelated labels or background")
        _ = fixture.focus(field)
        container.isHidden = true
        fixture.click(NSPoint(x: 280, y: 150))
        check(fixture.window.firstResponder === fixture.anchor, "Hidden suggestions must not exempt the background")
    }

    @MainActor private static func windows() {
        let first = Fixture()
        let second = Fixture()
        defer { first.close(); second.close() }
        let field = NSTextField()
        let other = NSTextField()
        first.add(field)
        second.add(other)
        let editor = first.focus(field)
        _ = second.focus(other)
        second.click(NSPoint(x: 40, y: 74))
        check(first.window.firstResponder === editor, "Clicking another window must not change this input")
        check(second.window.firstResponder === second.anchor, "Each app window must own its focus monitor")

        first.anchor.removeFromSuperview()
        first.click(NSPoint(x: 40, y: 74))
        check(first.window.firstResponder === editor, "Removing an anchor must remove its monitor")
        first.root.addSubview(first.anchor)
        first.click(NSPoint(x: 40, y: 74))
        check(first.window.firstResponder === first.anchor, "Reattaching an anchor must restore click-away")
        check(first.anchor.hitTest(.zero) == nil, "The app-wide anchor must not intercept hit testing")
        first.anchor.detach()
        first.anchor.detach()
        let focused = first.focus(field)
        first.click(NSPoint(x: 40, y: 74))
        check(first.window.firstResponder === focused, "Dismantled input focus must not retain a monitor")
    }

    @MainActor private static func search() {
        let fixture = Fixture()
        defer { fixture.close() }
        let field = NSSearchField()
        field.placeholderString = "Search this conversation"
        fixture.add(field)
        let anchor = SearchAnchor(frame: .zero)
        anchor.prompt = field.placeholderString
        var blurred = 0
        anchor.blurred = { blurred += 1 }
        fixture.root.addSubview(anchor)
        defer { anchor.detach() }
        let editor = fixture.focus(field)
        fixture.click(NSPoint(x: 280, y: 74))
        check(fixture.window.firstResponder === editor && blurred == 0, "Clicks inside search must retain focus")
        fixture.click(NSPoint(x: 40, y: 74))
        check(fixture.window.firstResponder === fixture.anchor && blurred == 1,
              "Search focus bindings must be notified by the shared dismissal path")
        let other = NSTextField()
        fixture.add(other, y: 110)
        _ = fixture.focus(other)
        fixture.click(NSPoint(x: 40, y: 124))
        check(blurred == 1, "Other fields must not trigger a search-specific blur callback")
    }
}
