import SwiftUI

enum PanelSize {
    static let width: CGFloat = 620
    static let maximumHeight: CGFloat = 720

    static func height(_ content: CGFloat?, available: CGFloat = .infinity) -> CGFloat {
        let limit = max(0, min(maximumHeight, available - 48))
        return min(max(0, content ?? limit), limit)
    }
}

struct PanelHeight: PreferenceKey {
    static var defaultValue: CGFloat? { nil }
    static func reduce(value: inout CGFloat?, nextValue: () -> CGFloat?) {
        if let next = nextValue() { value = next }
    }
}

struct PanelAction {
    var action: () -> Void = {}

    @MainActor func callAsFunction() { action() }
}

private struct PanelActiveKey: EnvironmentKey {
    static var defaultValue: Bool { false }
}

extension EnvironmentValues {
    var panelActive: Bool {
        get { self[PanelActiveKey.self] }
        set { self[PanelActiveKey.self] = newValue }
    }
}

struct PanelContext {
    let dismiss: PanelAction
    let ancestor: PanelAction?

    var back: PanelAction? { ancestor == nil ? nil : dismiss }

    @MainActor var close: PanelAction { PanelAction { end(dismiss.action) } }

    @MainActor func end(_ action: () -> Void) {
        action()
        ancestor?()
    }
}

private struct PanelClose: EnvironmentKey {
    static var defaultValue: PanelAction { PanelAction() }
}

private struct PanelScope: EnvironmentKey {
    static var defaultValue: PanelContext? { nil }
}

extension EnvironmentValues {
    var panelClose: PanelAction {
        get { self[PanelClose.self] }
        set { self[PanelClose.self] = newValue }
    }

    var panelContext: PanelContext? {
        get { self[PanelScope.self] }
        set { self[PanelScope.self] = newValue }
    }
}

extension View {
    func panel<Presented: View>(isPresented: Binding<Bool>,
                                @ViewBuilder content: @escaping () -> Presented) -> some View {
        modifier(PanelPresentation(isPresented: isPresented, presented: content))
    }

    func panel<Item: Identifiable, Presented: View>(item: Binding<Item?>,
                                                   @ViewBuilder content: @escaping (Item) -> Presented) -> some View {
        panel(isPresented: Binding(get: { item.wrappedValue != nil }, set: { shown in
            if !shown { item.wrappedValue = nil }
        })) {
            if let value = item.wrappedValue { content(value).id(value.id) }
        }
    }

    func panelHost() -> some View {
        #if os(macOS)
        modifier(PanelHost())
        #else
        self
        #endif
    }
}

private struct PanelPresentation<Presented: View>: ViewModifier {
    @Binding var isPresented: Bool
    @ViewBuilder let presented: () -> Presented
    @Environment(\.panelContext) private var parent
    @State private var id = UUID()

    private var panel: some View {
        let dismiss = PanelAction { isPresented = false }
        return presented()
            .environment(\.panelClose, dismiss)
            .environment(\.panelContext, PanelContext(dismiss: dismiss, ancestor: parent?.close))
    }

    func body(content: Content) -> some View {
        #if os(macOS)
        let entry = isPresented ? PanelEntry(id: id, view: AnyView(panel)) : nil
        content
            .transformPreference(PanelEntries.self) { entries in
                if let entry { entries.append(entry) }
            }
            .transformPreference(PanelKeys.self) { keys in
                if isPresented { keys.append(id) }
            }
        #else
        content.sheet(isPresented: $isPresented) { PanelSheet(content: panel) }
        #endif
    }
}

#if os(iOS)
private struct PanelSheet<Content: View>: View {
    let content: Content

    var body: some View {
        content
            .presentationSizing(PanelFit())
            .presentationDetents([.height(PanelSize.maximumHeight)])
            .transformPreference(PanelHeight.self) { $0 = nil }
    }
}

/// Sizes a panel like a macOS one: at most `PanelSize.width` wide and
/// `PanelSize.maximumHeight` tall, with the content scrolling inside it. A
/// height measured from the presented content cannot be used here: the first
/// measurement reports a partly laid out form, and the sheet keeps it.
private struct PanelFit: PresentationSizing {
    func proposedSize(for root: PresentationSizingRoot, context: PresentationSizingContext) -> ProposedViewSize {
        ProposedViewSize(width: PanelSize.width, height: PanelSize.maximumHeight)
    }
}
#endif

#if os(macOS)
import AppKit

private struct PanelEntry: Identifiable {
    let id: UUID
    let view: AnyView
}

private struct PanelEntries: PreferenceKey {
    static var defaultValue: [PanelEntry] { [] }
    static func reduce(value: inout [PanelEntry], nextValue: () -> [PanelEntry]) {
        value.append(contentsOf: nextValue())
    }
}

private struct PanelKeys: PreferenceKey {
    static var defaultValue: [UUID] { [] }
    static func reduce(value: inout [UUID], nextValue: () -> [UUID]) {
        value.append(contentsOf: nextValue())
    }
}

struct PanelExit: Equatable {
    let id: UUID
    let enabled: Bool
    let close: PanelAction

    static func == (lhs: Self, rhs: Self) -> Bool { lhs.id == rhs.id && lhs.enabled == rhs.enabled }
}

struct PanelDismissal: PreferenceKey {
    static var defaultValue: PanelExit? { nil }
    static func reduce(value: inout PanelExit?, nextValue: () -> PanelExit?) {
        if let next = nextValue() { value = next }
    }
}

private struct PanelHost: ViewModifier {
    @State private var active = false
    @State private var focus = FocusMemory()

    func body(content: Content) -> some View {
        content
            .environment(\.panelActive, active)
            .background { FocusWindow(memory: focus).frame(width: 0, height: 0).accessibilityHidden(true) }
            .disabled(active)
            .accessibilityHidden(active)
            .overlayPreferenceValue(PanelEntries.self) { entries in
                if let entry = entries.last {
                    AnyView(PanelLayer(entry: entry).id(entry.id).ignoresSafeArea())
                }
            }
            .onPreferenceChange(PanelKeys.self) { keys in
                let shown = !keys.isEmpty
                if shown && !active { focus.save() }
                if !shown && active { focus.restore() }
                active = shown
            }
            // Keep nested registrations in their own presenter.
            .transformPreference(PanelEntries.self) { $0 = [] }
            .transformPreference(PanelKeys.self) { $0 = [] }
    }
}

@MainActor
private final class FocusMemory {
    weak var window: NSWindow?
    private weak var responder: NSResponder?
    private var active = false

    func save() {
        active = true
        // Capture focus before disabling the underlying SwiftUI controls.
        if let editor = window?.firstResponder as? NSTextView, editor.isFieldEditor {
            responder = editor.delegate as? NSResponder
        } else {
            responder = window?.firstResponder
        }
    }

    func restore() {
        active = false
        let previous = responder
        responder = nil
        DispatchQueue.main.async { [weak self, weak previous] in
            guard let self, !self.active, let window = self.window,
                  window.attachedSheet == nil, let previous else { return }
            if let view = previous as? NSView, view.window !== window { return }
            window.makeFirstResponder(previous)
        }
    }
}

private struct FocusWindow: NSViewRepresentable {
    let memory: FocusMemory

    func makeNSView(context: Context) -> WindowAnchor { WindowAnchor(memory: memory) }
    func updateNSView(_ view: WindowAnchor, context: Context) { memory.window = view.window }
}

private final class WindowAnchor: NSView {
    let memory: FocusMemory

    init(memory: FocusMemory) {
        self.memory = memory
        super.init(frame: .zero)
    }

    required init?(coder: NSCoder) { nil }
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        memory.window = window
    }
}

private struct PanelLayer: View {
    let entry: PanelEntry
    @State private var exit: PanelExit?
    @State private var height: CGFloat?
    @Namespace private var focus

    var body: some View {
        GeometryReader { proxy in
            ZStack {
                Button(action: close) {
                    Color.black.opacity(0.24).ignoresSafeArea()
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(exit?.enabled != true)
                .focusable(false)
                .accessibilityLabel("Dismiss panel")
                entry.view
                    .frame(width: max(0, min(PanelSize.width, proxy.size.width - 48)),
                           height: PanelSize.height(height, available: proxy.size.height))
                    .onPreferenceChange(PanelHeight.self) { height = $0 }
                    .clipShape(RoundedRectangle(cornerRadius: 24))
                    .overlay {
                        RoundedRectangle(cornerRadius: 24).strokeBorder(.primary.opacity(0.12), lineWidth: 0.5)
                            .allowsHitTesting(false)
                    }
                    .shadow(color: .black.opacity(0.25), radius: 24, y: 12)
                    .focusScope(focus)
                    .onPreferenceChange(PanelDismissal.self) { exit = $0 }
                    .onExitCommand(perform: close)
                    .background { PanelFocus(close: close).frame(width: 0, height: 0) }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .accessibilityElement(children: .contain)
            .accessibilityAddTraits(.isModal)
            .panelHost()
        }
        .allowsWindowActivationEvents()
        .transformPreference(PanelDismissal.self) { $0 = nil }
        .transformPreference(PanelHeight.self) { $0 = nil }
    }

    private func close() {
        if let exit, exit.enabled { exit.close() }
    }
}

private struct PanelFocus: NSViewRepresentable {
    let close: () -> Void
    @Environment(\.isEnabled) private var enabled

    func makeNSView(context: Context) -> FocusAnchor { FocusAnchor() }

    func updateNSView(_ view: FocusAnchor, context: Context) {
        view.enabled = enabled
        view.close = close
    }

    static func dismantleNSView(_ view: FocusAnchor, coordinator: ()) { view.stop() }
}

private final class FocusAnchor: NSView {
    var enabled = true
    var close: () -> Void = {}
    private var monitor: Any?

    override var acceptsFirstResponder: Bool { true }
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        stop()
        guard let window else { return }
        DispatchQueue.main.async { [weak self, weak window] in
            guard let self, let window, self.window === window, self.enabled else { return }
            if window.firstResponder === window || window.firstResponder == nil {
                window.makeFirstResponder(self)
                window.selectNextKeyView(self)
            }
        }
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            guard let self, self.enabled, let window = self.window,
                  event.window === window, window.attachedSheet == nil, event.keyCode == 53,
                  event.modifierFlags.intersection([.command, .control, .option]).isEmpty else { return event }
            self.close()
            return nil
        }
    }

    func stop() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
    }
}
#endif
