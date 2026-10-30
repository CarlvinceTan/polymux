#if os(macOS)
import AppKit
import SwiftUI

struct InlineInput: NSViewRepresentable {
    @Binding var text: String
    @Binding var focused: Bool
    let prompt: String
    let font: Font
    let tint: Color

    func makeNSView(context: Context) -> InlineField {
        let field = InlineField()
        field.isBezeled = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.alignment = .center
        field.usesSingleLineMode = true
        field.lineBreakMode = .byTruncatingTail
        field.delegate = field
        field.setAccessibilityLabel(prompt)
        return field
    }

    func updateNSView(_ field: InlineField, context: Context) {
        field.edit = { text = $0 }
        field.focus = { focused = $0 }
        field.font = font.resolve(in: context.environment.fontResolutionContext).ctFont as NSFont
        field.textColor = focused || text.isEmpty ? NSColor(tint) : .clear
        field.isEnabled = context.environment.isEnabled
        if field.currentEditor() == nil, field.stringValue != text { field.stringValue = text }
    }
}

final class InlineField: NSTextField, NSTextFieldDelegate {
    var edit: (String) -> Void = { _ in }
    var focus: (Bool) -> Void = { _ in }

    override func mouseDown(with event: NSEvent) {
        if currentEditor() == nil { selectText(nil) }
        else { super.mouseDown(with: event) }
    }

    func controlTextDidBeginEditing(_ notification: Notification) {
        (currentEditor() as? NSTextView)?.alignment = .center
        focus(true)
    }
    func controlTextDidChange(_ notification: Notification) { edit(stringValue) }
    func controlTextDidEndEditing(_ notification: Notification) {
        edit(stringValue)
        focus(false)
    }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        guard selector == #selector(NSResponder.insertNewline(_:)) else { return false }
        window?.makeFirstResponder(nil)
        return true
    }
}
#endif
