import SwiftUI

struct SectionEditor: View {
    @State var name: String
    let save: (String) -> Void
    let cancel: () -> Void
    @State private var selection: TextSelection?
    @State private var finished = false
    @FocusState private var focused: Bool

    private var cleanName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var valid: Bool { !cleanName.isEmpty && cleanName.unicodeScalars.count <= 40 }

    var body: some View {
        #if os(macOS)
        SectionInput(name: name, save: save, cancel: cancel)
        #else
        TextField("Section name", text: $name, selection: $selection)
            .textFieldStyle(.plain)
            .font(.body)
            .foregroundStyle(Color.primary.opacity(0.7))
            .accessibilityLabel("Section name")
            .focused($focused)
            .onSubmit { finish() }
            .onChange(of: focused) { wasFocused, isFocused in
                if wasFocused && !isFocused { finish() }
            }
            .task {
                focused = true
                selection = TextSelection(range: name.startIndex..<name.endIndex)
            }
        #endif
    }

    private func finish() {
        guard !finished else { return }
        finished = true
        if valid { save(cleanName) }
        else { cancel() }
    }
}

#if os(macOS)
import AppKit

private struct SectionInput: NSViewRepresentable {
    let name: String
    let save: (String) -> Void
    let cancel: () -> Void

    func makeNSView(context: Context) -> SectionField {
        let field = SectionField()
        field.stringValue = name
        field.font = .preferredFont(forTextStyle: .body)
        field.textColor = .labelColor.withAlphaComponent(0.7)
        field.isBezeled = false
        field.isBordered = false
        field.drawsBackground = false
        field.isEditable = true
        field.isSelectable = true
        field.focusRingType = .none
        field.setContentHuggingPriority(.defaultLow, for: .horizontal)
        field.setAccessibilityLabel("Section name")
        field.delegate = field
        return field
    }

    func updateNSView(_ field: SectionField, context: Context) {
        field.save = save
        field.cancel = cancel
    }

    static func dismantleNSView(_ field: SectionField, coordinator: ()) { field.detach() }

    func sizeThatFits(_ proposal: ProposedViewSize, nsView: SectionField, context: Context) -> CGSize? {
        CGSize(width: proposal.width ?? nsView.intrinsicContentSize.width,
               height: nsView.intrinsicContentSize.height)
    }
}

private final class SectionField: NSTextField, NSTextFieldDelegate {
    var save: (String) -> Void = { _ in }
    var cancel: () -> Void = {}
    private var finished = false
    private var observers: [NSObjectProtocol] = []

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        detach()
        guard let window else { return }
        for name in [NSWindow.didBecomeKeyNotification, NSWindow.didResignKeyNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: window, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.styleEditor() }
            })
        }
        // Wait for context-menu tracking to finish before taking the field editor.
        RunLoop.main.perform(inModes: [.default]) { [weak self] in
            MainActor.assumeIsolated {
                guard let self, !self.finished, self.window != nil else { return }
                self.selectText(nil)
                self.styleEditor()
            }
        }
    }

    func controlTextDidBeginEditing(_ notification: Notification) {
        styleEditor()
    }

    override func viewDidChangeEffectiveAppearance() {
        super.viewDidChangeEffectiveAppearance()
        styleEditor()
    }

    func detach() {
        for observer in observers { NotificationCenter.default.removeObserver(observer) }
        observers.removeAll()
    }

    private func styleEditor() {
        guard let editor = currentEditor() as? NSTextView else { return }
        editor.drawsBackground = false
        editor.backgroundColor = .clear
        let active = window?.isKeyWindow == true
        editor.selectedTextAttributes = [
            .foregroundColor: active ? NSColor.selectedTextColor : NSColor.unemphasizedSelectedTextColor,
            .backgroundColor: active ? NSColor.selectedTextBackgroundColor : NSColor.unemphasizedSelectedTextBackgroundColor
        ]
    }

    func controlTextDidEndEditing(_ notification: Notification) { finish() }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        switch selector {
        case #selector(NSResponder.insertNewline(_:)):
            finish()
        case #selector(NSResponder.cancelOperation(_:)):
            guard !finished else { return true }
            finished = true
            cancel()
        default:
            return false
        }
        return true
    }

    private func finish() {
        guard !finished else { return }
        finished = true
        let name = stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        if !name.isEmpty && name.unicodeScalars.count <= 40 { save(name) }
        else { cancel() }
    }
}
#endif
