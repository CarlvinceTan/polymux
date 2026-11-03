import SwiftUI

struct SectionEditor: View {
    @State var name: String
    let save: (String) -> Void
    let cancel: () -> Void
    var saveOnBlur = true
    @State private var invalid = false
    @State private var selection: TextSelection?
    @State private var finished = false
    @FocusState private var focused: Bool

    private var cleanName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var valid: Bool { !cleanName.isEmpty && cleanName.unicodeScalars.count <= 40 }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            #if os(macOS)
            SectionInput(name: name, save: save, cancel: cancel, saveOnBlur: saveOnBlur,
                         tooLong: { invalid = true }, changed: { invalid = $0.unicodeScalars.count > 40 })
            #else
            TextField("Section name", text: $name, selection: $selection)
                .textFieldStyle(.plain)
                .font(.body)
                .foregroundStyle(Color.primary.opacity(0.7))
                .accessibilityLabel("Section name")
                .focused($focused)
                .onSubmit { finish() }
                .onChange(of: focused) { wasFocused, isFocused in
                    if wasFocused && !isFocused {
                        if saveOnBlur { finish() } else { cancel() }
                    }
                }
                .onChange(of: name) { _, value in invalid = value.unicodeScalars.count > 40 }
                .task {
                    focused = true
                    selection = TextSelection(range: name.startIndex..<name.endIndex)
                }
            #endif
            if invalid {
                Text("Section names are limited to 40 characters.")
                    .font(.caption).foregroundStyle(.muted)
            }
        }
    }

    private func finish() {
        guard !finished else { return }
        // A too-long name only notes the limit; the editor stays open so it can be corrected.
        if valid { finished = true; save(cleanName) }
        else if cleanName.unicodeScalars.count > 40 { invalid = true }
        else { finished = true; cancel() }
    }
}

#if os(macOS)
import AppKit

private struct SectionInput: NSViewRepresentable {
    let name: String
    let save: (String) -> Void
    let cancel: () -> Void
    let saveOnBlur: Bool
    let tooLong: () -> Void
    let changed: (String) -> Void

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
        field.saveOnBlur = saveOnBlur
        field.tooLong = tooLong
        field.changed = changed
        field.appearance = NSAppearance(named: context.environment.colorScheme == .dark ? .darkAqua : .aqua)
        field.styleEditor()
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
    var tooLong: () -> Void = {}
    var changed: (String) -> Void = { _ in }
    var saveOnBlur = true
    private var finished = false
    private var observers: [NSObjectProtocol] = []
    /// When editing began; the closing menu that opened it can take focus back a moment later.
    private var began = Date.distantPast

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
                self.began = Date()
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

    func styleEditor() {
        var color = NSColor.labelColor
        effectiveAppearance.performAsCurrentDrawingAppearance {
            color = NSColor.labelColor.withAlphaComponent(0.7).usingColorSpace(.deviceRGB) ?? .labelColor
        }
        textColor = color
        guard let editor = currentEditor() as? NSTextView else { return }
        editor.textColor = color
        editor.typingAttributes[.foregroundColor] = color
        editor.drawsBackground = false
        editor.backgroundColor = .clear
        let active = window?.isKeyWindow == true
        editor.selectedTextAttributes = [
            .foregroundColor: active ? NSColor.selectedTextColor : NSColor.unemphasizedSelectedTextColor,
            .backgroundColor: active ? NSColor.selectedTextBackgroundColor : NSColor.unemphasizedSelectedTextBackgroundColor
        ]
    }

    func controlTextDidChange(_ notification: Notification) {
        changed(stringValue.trimmingCharacters(in: .whitespacesAndNewlines))
    }

    func controlTextDidEndEditing(_ notification: Notification) {
        // Focus lost straight after opening came from the menu closing, not the user; keep editing.
        let movement = notification.userInfo?["NSTextMovement"] as? Int
        if movement == NSTextMovement.other.rawValue, Date().timeIntervalSince(began) < 0.6, window != nil, !finished {
            RunLoop.main.perform(inModes: [.default]) { [weak self] in
                MainActor.assumeIsolated {
                    guard let self, !self.finished, self.window != nil else { return }
                    self.selectText(nil)
                    self.styleEditor()
                }
            }
            return
        }
        if saveOnBlur { finish() }
        else if !finished { finished = true; cancel() }
    }

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
        // A too-long name only notes the limit; the editor stays open so it can be corrected.
        let name = stringValue.trimmingCharacters(in: .whitespacesAndNewlines)
        if !name.isEmpty && name.unicodeScalars.count <= 40 { finished = true; save(name) }
        else if name.unicodeScalars.count > 40 { tooLong() }
        else { finished = true; cancel() }
    }
}
#endif
