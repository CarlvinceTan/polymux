import SwiftUI

#if os(macOS)
import AppKit

struct InputFocus: NSViewRepresentable {
    func makeNSView(context: Context) -> InputAnchor { InputAnchor() }
    func updateNSView(_ view: InputAnchor, context: Context) {}
    static func dismantleNSView(_ view: InputAnchor, coordinator: ()) { view.detach() }
}

final class InputAnchor: NSView {
    private var monitor: Any?

    override var acceptsFirstResponder: Bool { true }
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        detach()
        guard window != nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) { [weak self] event in
            self?.clicked(event)
            return event
        }
    }

    func detach() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
    }

    private func clicked(_ event: NSEvent) {
        guard let window, event.window === window, let content = window.contentView else { return }
        let root = content.superview ?? content
        guard let input = input(in: root) else { return }
        guard !contains(event.locationInWindow, in: input),
              !containsAccessory(event.locationInWindow, in: root) else { return }
        window.makeFirstResponder(self)
    }

    private func input(in root: NSView) -> NSView? {
        if let field = window?.firstResponder as? NSTextField {
            return field.isEditable && field.isEnabled ? field : nil
        }
        guard let editor = window?.firstResponder as? NSTextView, editor.isEditable else { return nil }
        if editor.isFieldEditor {
            // Field editors can be smaller than the input's padding and are shared between controls.
            if let owner = field(for: editor, in: root) {
                return owner.isEditable && owner.isEnabled ? owner : nil
            }
            return editor
        }
        if let scroll = editor.enclosingScrollView, scroll.documentView === editor { return scroll }
        return editor
    }

    private func field(for editor: NSTextView, in view: NSView) -> NSTextField? {
        if let field = view as? NSTextField, field.currentEditor() === editor { return field }
        for child in view.subviews {
            if let field = field(for: editor, in: child) { return field }
        }
        return nil
    }

    private func containsAccessory(_ point: NSPoint, in view: NSView) -> Bool {
        guard !view.isHiddenOrHasHiddenAncestor else { return false }
        if view is AccessoryAnchor { return contains(point, in: view) }
        return view.subviews.contains { containsAccessory(point, in: $0) }
    }

    private func contains(_ point: NSPoint, in view: NSView) -> Bool {
        // Unclipped AppKit views can report a visible rect larger than their bounds.
        view.bounds.intersection(view.visibleRect).contains(view.convert(point, from: nil))
    }
}

private struct InputAccessory: NSViewRepresentable {
    func makeNSView(context: Context) -> AccessoryAnchor { AccessoryAnchor() }
    func updateNSView(_ view: AccessoryAnchor, context: Context) {}
}

final class AccessoryAnchor: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
}
#endif

extension View {
    func inputAccessory() -> some View {
        #if os(macOS)
        background { InputAccessory().accessibilityHidden(true) }
        #else
        self
        #endif
    }
}
