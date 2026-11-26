#if os(macOS)
import AppKit
import SwiftUI
import PolymuxKit

/// A pill around the notch while bots work; it opens into their chats on hover.
@MainActor @Observable
final class NotchPanel {
    static let shared = NotchPanel()

    /// The notch's size on the screen the pill sits on.
    private(set) var notch = CGSize(width: 200, height: 32)
    /// --verify-notch-open starts the pill open, since a check can't hover it.
    var expanded = CommandLine.arguments.contains("--verify-notch-open") {
        didSet { if expanded != oldValue { place() } }
    }
    /// Whether a notched screen is attached, for the Settings toggle.
    private(set) var available = NotchPanel.screen != nil
    @ObservationIgnored private var panel: NSPanel?
    @ObservationIgnored private var observing = false

    static let wing: CGFloat = 34
    static let width: CGFloat = 360
    static let row: CGFloat = 48

    private init() {
        NotificationCenter.default.addObserver(forName: NSApplication.didChangeScreenParametersNotification,
                                               object: nil, queue: .main) { _ in
            Task { @MainActor in NotchPanel.shared.update() }
        }
    }

    /// Shows the pill while the setting is on, a notched screen is present and a bot is working.
    func update() {
        observe()
        available = Self.screen != nil
        let bar = StatusBar.shared
        guard bar.notch, let screen = Self.screen, !bar.working.isEmpty else {
            panel?.orderOut(nil)
            if !CommandLine.arguments.contains("--verify-notch-open") { expanded = false }
            return
        }
        let panel = panel ?? make()
        notch = Self.notch(on: screen)
        place()
        if !panel.isVisible { panel.orderFrontRegardless() }
    }

    /// The built-in screen with a notch, if any.
    static var screen: NSScreen? {
        #if DEBUG
        // --verify-notch draws the pill on a Mac without a notch, around a notch-sized gap.
        if CommandLine.arguments.contains("--verify-notch") { return NSScreen.main }
        #endif
        return NSScreen.screens.first { $0.safeAreaInsets.top > 0 && $0.auxiliaryTopLeftArea != nil }
    }

    private static func notch(on screen: NSScreen) -> CGSize {
        #if DEBUG
        if CommandLine.arguments.contains("--verify-notch") { return CGSize(width: 200, height: 32) }
        #endif
        let left = screen.auxiliaryTopLeftArea?.width ?? 0
        let right = screen.auxiliaryTopRightArea?.width ?? 0
        return CGSize(width: screen.frame.width - left - right, height: screen.safeAreaInsets.top)
    }

    /// Fits the panel to the pill, so the menu bar beside it stays clickable.
    private func place() {
        guard let panel, let screen = Self.screen else { return }
        let rows = CGFloat(min(StatusBar.shared.working.count, 5))
        let size = expanded
            ? CGSize(width: max(Self.width, notch.width + 2 * Self.wing), height: notch.height + rows * Self.row + 12)
            : CGSize(width: notch.width + 2 * Self.wing, height: notch.height)
        let frame = CGRect(x: screen.frame.midX - size.width / 2, y: screen.frame.maxY - size.height,
                           width: size.width, height: size.height)
        panel.setFrame(frame, display: true)
    }

    private func make() -> NSPanel {
        let panel = NotchWindow(contentRect: .zero, styleMask: [.borderless, .nonactivatingPanel],
                                backing: .buffered, defer: false)
        panel.level = .statusBar
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        panel.isFloatingPanel = true
        panel.hidesOnDeactivate = false
        panel.isReleasedWhenClosed = false
        panel.collectionBehavior = [.canJoinAllSpaces, .stationary, .fullScreenAuxiliary, .ignoresCycle]
        panel.title = "Polymux Activity"
        let host = NSHostingView(rootView: NotchView(panel: self, bar: StatusBar.shared))
        host.sizingOptions = []
        panel.contentView = host
        self.panel = panel
        return panel
    }

    /// Re-runs `update` whenever the working chats or the setting change.
    private func observe() {
        guard !observing else { return }
        observing = true
        withObservationTracking {
            if StatusBar.shared.notch { _ = StatusBar.shared.working.map(\.id) }
        } onChange: {
            Task { @MainActor in
                NotchPanel.shared.observing = false
                NotchPanel.shared.update()
            }
        }
    }
}

/// Never takes focus from the app in front.
private final class NotchWindow: NSPanel {
    override var canBecomeKey: Bool { false }
    override var canBecomeMain: Bool { false }
    /// Sits over the menu bar, where the notch is, instead of being pushed below it.
    override func constrainFrameRect(_ frameRect: NSRect, to screen: NSScreen?) -> NSRect { frameRect }
}

private struct NotchView: View {
    let panel: NotchPanel
    let bar: StatusBar
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let working = bar.working
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                if let first = working.first {
                    face(first, size: 22).frame(width: NotchPanel.wing)
                }
                Spacer(minLength: panel.notch.width)
                Text("\(working.count)")
                    .font(.callout.monospacedDigit().weight(.semibold))
                    .foregroundStyle(.white)
                    .frame(width: NotchPanel.wing)
                    .accessibilityLabel("\(working.count) working")
            }
            .frame(height: panel.notch.height)
            if panel.expanded {
                VStack(spacing: 0) {
                    ForEach(working.prefix(5)) { entry in
                        NotchRow(entry: entry, bar: bar, face: face(entry, size: 30))
                    }
                }
                .padding(.horizontal, 6)
                .padding(.bottom, 6)
                .transition(.opacity)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .background(.black, in: UnevenRoundedRectangle(bottomLeadingRadius: panel.expanded ? 18 : 10,
                                                       bottomTrailingRadius: panel.expanded ? 18 : 10))
        .environment(\.colorScheme, .dark)
        .environment(bar.session?.avatars)
        .onHover { hovering in
            #if DEBUG
            if CommandLine.arguments.contains("--verify-notch-open") { return }
            #endif
            withAnimation(Motion.quick.unless(reduceMotion)) { panel.expanded = hovering }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Polymux activity")
    }

    @ViewBuilder private func face(_ entry: StatusBar.Entry, size: CGFloat) -> some View {
        if bar.session != nil {
            AgentAvatar(agentID: entry.agentID, conversationID: entry.id, runs: entry.thread.runs,
                        activity: bar.session?.sidebar.activity, showsMarker: false)
                .frame(width: size, height: size)
                .accessibilityHidden(true)
        }
    }
}

private struct NotchRow<Face: View>: View {
    let entry: StatusBar.Entry
    let bar: StatusBar
    let face: Face
    @State private var hovered = false

    var body: some View {
        Button { bar.open(entry.id) } label: {
            HStack(spacing: 10) {
                face
                VStack(alignment: .leading, spacing: 2) {
                    Text(entry.name).font(.callout.weight(.medium)).foregroundStyle(.white).lineLimit(1)
                    Text(entry.preview).font(.caption).foregroundStyle(.white.opacity(0.7)).lineLimit(1)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.horizontal, 8)
            .frame(height: NotchPanel.row)
            .background(hovered ? Color.white.opacity(0.12) : .clear, in: .rect(cornerRadius: 10))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .onHover { hovered = $0 }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(entry.name), \(entry.preview)")
    }
}
#endif
