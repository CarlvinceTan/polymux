import SwiftUI
#if os(iOS)
import UIKit
#elseif os(macOS)
import AppKit
#endif

struct RecipientField {
    @Binding var text: String
    @Binding var focused: Bool
    var placeholder: String
    var onSubmit: () -> Void
    var onDelete: () -> Void
}

#if os(iOS)
extension RecipientField: UIViewRepresentable {
    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIView(context: Context) -> EntryField {
        let field = EntryField()
        field.delegate = context.coordinator
        field.onDelete = { [weak coordinator = context.coordinator] in coordinator?.emptyDelete() }
        field.addTarget(context.coordinator, action: #selector(Coordinator.changed(_:)), for: .editingChanged)
        field.borderStyle = .none
        field.autocapitalizationType = .none
        field.autocorrectionType = .no
        field.spellCheckingType = .no
        field.returnKeyType = .done
        field.font = .preferredFont(forTextStyle: .body)
        field.adjustsFontForContentSizeCategory = true
        field.textColor = .label
        field.tintColor = .label
        field.setContentHuggingPriority(.defaultLow, for: .horizontal)
        field.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        return field
    }

    func updateUIView(_ field: EntryField, context: Context) {
        context.coordinator.parent = self
        field.placeholder = placeholder
        if field.text != text, field.markedTextRange == nil { field.text = text }
        context.coordinator.syncFocus(field)
    }

    static func dismantleUIView(_ field: EntryField, coordinator: Coordinator) {
        field.onDelete = nil
        field.delegate = nil
        field.removeTarget(coordinator, action: #selector(Coordinator.changed(_:)), for: .editingChanged)
    }

    final class EntryField: UITextField {
        var onDelete: (() -> Void)?
        override func deleteBackward() {
            let empty = (text ?? "").isEmpty && markedTextRange == nil
            super.deleteBackward()
            if empty { onDelete?() }
        }
    }

    @MainActor final class Coordinator: NSObject, UITextFieldDelegate {
        var parent: RecipientField
        init(_ parent: RecipientField) { self.parent = parent }

        @objc func changed(_ field: UITextField) {
            let text = field.text ?? ""
            if parent.text != text { parent.text = text }
        }

        func textFieldDidBeginEditing(_ textField: UITextField) {
            if !parent.focused { parent.focused = true }
        }

        func textFieldDidEndEditing(_ textField: UITextField) {
            if parent.focused { parent.focused = false }
        }

        func textFieldShouldReturn(_ textField: UITextField) -> Bool {
            guard textField.markedTextRange == nil else { return false }
            changed(textField)
            parent.onSubmit()
            return false
        }

        func emptyDelete() { parent.onDelete() }

        func syncFocus(_ field: UITextField) {
            guard field.window != nil else {
                DispatchQueue.main.async { [weak self, weak field] in
                    guard let self, let field, field.window != nil else { return }
                    self.syncFocus(field)
                }
                return
            }
            if parent.focused, !field.isFirstResponder { field.becomeFirstResponder() }
            else if !parent.focused, field.isFirstResponder { field.resignFirstResponder() }
        }
    }
}
#elseif os(macOS)
extension RecipientField: NSViewRepresentable {
    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSTextField {
        let field = NSTextField()
        field.delegate = context.coordinator
        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.isEditable = true
        field.isSelectable = true
        field.maximumNumberOfLines = 1
        field.lineBreakMode = .byClipping
        field.font = .preferredFont(forTextStyle: .body)
        field.textColor = .labelColor
        field.setContentHuggingPriority(.defaultLow, for: .horizontal)
        field.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        return field
    }

    func updateNSView(_ field: NSTextField, context: Context) {
        context.coordinator.parent = self
        field.placeholderString = placeholder
        let composing = (field.currentEditor() as? NSTextView)?.hasMarkedText() == true
        if field.stringValue != text, !composing { field.stringValue = text }
        context.coordinator.syncFocus(field)
    }

    static func dismantleNSView(_ field: NSTextField, coordinator: Coordinator) {
        field.delegate = nil
    }

    @MainActor final class Coordinator: NSObject, NSTextFieldDelegate {
        var parent: RecipientField
        init(_ parent: RecipientField) { self.parent = parent }

        func controlTextDidChange(_ notification: Notification) {
            guard let field = notification.object as? NSTextField else { return }
            if parent.text != field.stringValue { parent.text = field.stringValue }
        }

        func controlTextDidBeginEditing(_ notification: Notification) {
            if !parent.focused { parent.focused = true }
        }

        func controlTextDidEndEditing(_ notification: Notification) {
            if parent.focused { parent.focused = false }
        }

        func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
            guard !textView.hasMarkedText() else { return false }
            if selector == #selector(NSResponder.deleteBackward(_:)), textView.string.isEmpty {
                parent.onDelete()
                return true
            }
            if selector == #selector(NSResponder.insertNewline(_:)) {
                if parent.text != textView.string { parent.text = textView.string }
                parent.onSubmit()
                return true
            }
            return false
        }

        func syncFocus(_ field: NSTextField) {
            guard let window = field.window else {
                DispatchQueue.main.async { [weak self, weak field] in
                    guard let self, let field, field.window != nil else { return }
                    self.syncFocus(field)
                }
                return
            }
            if parent.focused, field.currentEditor() == nil { window.makeFirstResponder(field) }
            else if !parent.focused, field.currentEditor() != nil { window.makeFirstResponder(nil) }
        }
    }
}
#endif
