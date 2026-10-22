import Foundation

enum SidebarWidth {
    static let minimum: CGFloat = 260
    static let maximum: CGFloat = 440
    static let initial = 320.0
}

#if os(macOS)
import AppKit
import SwiftUI
import QuartzCore
import Observation

enum SidebarStage {
    case expanded, collapsing, collapsed, expanding

    var moving: Bool { self == .collapsing || self == .expanding }
}

@Observable @MainActor
final class SidebarState {
    var stage = SidebarStage.expanded
    var edge: CGFloat = SidebarWidth.initial
    @ObservationIgnored weak var anchor: SidebarAnchor?

    func reveal() -> Bool { anchor?.reveal() ?? false }
}

struct SidebarLimits: NSViewRepresentable {
    @Binding var width: Double
    let state: SidebarState

    func makeNSView(context: Context) -> SidebarAnchor {
        let view = SidebarAnchor()
        updateNSView(view, context: context)
        return view
    }

    func updateNSView(_ view: SidebarAnchor, context: Context) {
        view.state = state
        state.anchor = view
        view.savedWidth = width
        view.onWidth = { value in
            if abs(width - value) > 0.5 { width = value }
        }
        view.schedule()
    }

    static func dismantleNSView(_ view: SidebarAnchor, coordinator: ()) {
        view.detach()
    }
}

final class SidebarAnchor: NSView {
    var savedWidth = SidebarWidth.initial {
        didSet {
            if abs(oldValue - savedWidth) > 0.5 && !dragging { restoreWidth = true }
        }
    }
    var onWidth: ((Double) -> Void)?
    weak var state: SidebarState?
    private weak var split: NSSplitView?
    private weak var item: NSSplitViewItem?
    private var scheduled = false
    private var restoreWidth = false
    private var wasCollapsed = false
    private var monitor: Any?
    private var dragging = false
    private var snapped = false
    private var dragOffset: CGFloat = 0
    private var movement: UUID?
    private var collapseWatch: NSKeyValueObservation?
    private var revealTimer: Timer?

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil { detach() } else { schedule() }
    }

    override func viewDidMoveToSuperview() {
        super.viewDidMoveToSuperview()
        schedule()
    }

    override func layout() {
        super.layout()
        schedule()
    }

    func schedule() {
        guard !scheduled else { return }
        scheduled = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.scheduled = false
            self.configure()
        }
    }

    func detach() {
        revealTimer?.invalidate()
        revealTimer = nil
        NotificationCenter.default.removeObserver(self, name: NSSplitView.didResizeSubviewsNotification, object: split)
        collapseWatch?.invalidate()
        collapseWatch = nil
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
        dragging = false
        snapped = false
        movement = nil
        if state?.anchor === self {
            publish(.collapsed)
            state?.anchor = nil
        }
        split = nil
        item = nil
    }

    @objc private func resized(_ notification: Notification) { schedule() }

    private func track(_ event: NSEvent) -> NSEvent? {
        guard let split, let item, let window else { return event }
        if dragging {
            if event.type == .leftMouseUp {
                dragging = false
                snapped = false
                return nil
            }
            guard event.type == .leftMouseDragged else { return event }
            guard !snapped else { return nil }
            let point = split.convert(event.locationInWindow, from: nil)
            let position = point.x - dragOffset
            if position < SidebarWidth.minimum / 2 {
                snapped = true
                let actual = item.viewController.view.frame.width
                onWidth?(Double(min(SidebarWidth.maximum, max(SidebarWidth.minimum, actual))))
                move(true)
            } else {
                split.setPosition(min(SidebarWidth.maximum, max(SidebarWidth.minimum, position)), ofDividerAt: 0)
            }
            return nil
        }
        guard event.type == .leftMouseDown, event.window === window,
              event.clickCount == 1, split.isVertical, !item.isCollapsed,
              movement == nil else { return event }
        let point = split.convert(event.locationInWindow, from: nil)
        let drawn = NSRect(x: item.viewController.view.frame.maxX, y: split.bounds.minY,
                           width: split.dividerThickness, height: split.bounds.height)
        let proposed = drawn.insetBy(dx: -4, dy: 0)
        let effective = split.delegate?.splitView?(split, effectiveRect: proposed, forDrawnRect: drawn, ofDividerAt: 0) ?? proposed
        guard effective.contains(point) else { return event }
        dragging = true
        dragOffset = point.x - drawn.minX
        return nil
    }

    func reveal() -> Bool {
        guard window != nil, split != nil, item != nil else { return false }
        guard movement == nil else { return true }
        move(false)
        return true
    }

    private func publish(_ stage: SidebarStage) {
        guard state?.stage != stage else { return }
        var transaction = Transaction(animation: nil)
        transaction.disablesAnimations = true
        withTransaction(transaction) {
            state?.stage = stage
            if stage == .collapsed { state?.edge = 0 }
        }
    }

    private func move(_ collapsed: Bool) {
        guard let item, let split else { return }
        let token = UUID()
        movement = token
        publish(collapsed ? .collapsing : .expanding)
        revealTimer?.invalidate()
        revealTimer = Timer.scheduledTimer(withTimeInterval: 1.0 / 60, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.publishEdge() }
        }
        // Apply the toolbar diff before AppKit starts changing column geometry.
        DispatchQueue.main.async { [weak self, weak item, weak split] in
            guard let self, let item, let split, self.movement == token,
                  self.item === item, self.split === split else { return }
            if NSWorkspace.shared.accessibilityDisplayShouldReduceMotion {
                item.isCollapsed = collapsed
                self.finish(token, item: item, split: split)
            } else {
                NSAnimationContext.runAnimationGroup { @Sendable [weak self] context in
                    context.duration = 0.22
                    context.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
                    context.allowsImplicitAnimation = true
                    MainActor.assumeIsolated {
                        guard let self, self.movement == token, let item = self.item, let split = self.split,
                              let controller = split.delegate as? NSSplitViewController else { return }
                        if item.isCollapsed != collapsed { controller.toggleSidebar(nil) }
                        split.layoutSubtreeIfNeeded()
                    }
                } completionHandler: { @Sendable [weak self] in
                    MainActor.assumeIsolated {
                        guard let self, let item = self.item, let split = self.split else { return }
                        self.finish(token, item: item, split: split)
                    }
                }
            }
        }
    }

    private func finish(_ token: UUID, item: NSSplitViewItem, split: NSSplitView) {
        guard movement == token, self.item === item, self.split === split else { return }
        movement = nil
        revealTimer?.invalidate()
        revealTimer = nil
        split.layoutSubtreeIfNeeded()
        if item.isCollapsed || split.isSubviewCollapsed(item.viewController.view) {
            wasCollapsed = true
            publish(.collapsed)
        } else {
            restoreWidth = false
            wasCollapsed = false
            split.setPosition(min(SidebarWidth.maximum, max(SidebarWidth.minimum, CGFloat(savedWidth))), ofDividerAt: 0)
            split.layoutSubtreeIfNeeded()
            configure()
        }
        publishEdge()
    }

    private func configure() {
        guard window != nil else { return }
        if split == nil || item == nil { attach() }
        guard let split, let item else { return }
        publishEdge()
        if item.minimumThickness != SidebarWidth.minimum { item.minimumThickness = SidebarWidth.minimum }
        if item.maximumThickness != SidebarWidth.maximum { item.maximumThickness = SidebarWidth.maximum }
        if !item.canCollapse { item.canCollapse = true }
        guard movement == nil else { return }
        let collapsed = item.isCollapsed || split.isSubviewCollapsed(item.viewController.view)
        if collapsed {
            wasCollapsed = true
            publish(.collapsed)
            return
        }
        let actual = item.viewController.view.frame.width
        if wasCollapsed && actual + 0.5 < SidebarWidth.minimum {
            publish(.expanding)
            return
        }
        if restoreWidth || wasCollapsed {
            restoreWidth = false
            wasCollapsed = false
            split.setPosition(min(SidebarWidth.maximum, max(SidebarWidth.minimum, CGFloat(savedWidth))), ofDividerAt: 0)
        }
        let restored = item.viewController.view.frame.width
        if restored < SidebarWidth.minimum || restored > SidebarWidth.maximum {
            split.setPosition(min(SidebarWidth.maximum, max(SidebarWidth.minimum, restored)), ofDividerAt: 0)
        } else {
            onWidth?(Double(restored))
            publish(.expanded)
        }
    }

    private func attach() {
        var ancestor = superview
        while let view = ancestor {
            if let split = view as? NSSplitView,
               let controller = split.delegate as? NSSplitViewController,
               let item = controller.splitViewItems.first,
               isDescendant(of: item.viewController.view) {
                self.split = split
                self.item = item
                restoreWidth = true
                wasCollapsed = item.isCollapsed
                collapseWatch = item.observe(\.isCollapsed, options: [.new]) { [weak self] _, change in
                    MainActor.assumeIsolated {
                        guard let self, self.movement == nil, let collapsed = change.newValue else { return }
                        self.wasCollapsed = true
                        self.publish(collapsed ? .collapsed : .expanding)
                        self.schedule()
                    }
                }
                NotificationCenter.default.addObserver(self, selector: #selector(resized(_:)),
                                                       name: NSSplitView.didResizeSubviewsNotification, object: split)
                monitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .leftMouseDragged, .leftMouseUp]) { [weak self] event in
                    guard let self else { return event }
                    return self.track(event)
                }
                return
            }
            ancestor = view.superview
        }
    }

    private func publishEdge() {
        guard let split, let item else { return }
        let view = item.viewController.view
        let edge: CGFloat
        if movement != nil, let layer = view.layer?.presentation(), let target = split.layer?.presentation() {
            let point = layer.convert(CGPoint(x: layer.bounds.maxX, y: layer.bounds.midY), to: target)
            edge = split.convert(point, to: nil).x
        } else {
            edge = item.isCollapsed ? 0 : view.convert(NSPoint(x: view.bounds.maxX, y: 0), to: nil).x
        }
        if state?.edge != edge { state?.edge = edge }
    }
}

struct SidebarControl: NSViewRepresentable {
    let state: SidebarState
    let edge: CGFloat
    let expanded: Bool
    let reveal: () -> Void
    let search: () -> Void
    let add: () -> Void
    let settings: () -> Void
    @Environment(\.isEnabled) private var enabled
    @Environment(\.colorScheme) private var scheme

    func makeNSView(context: Context) -> SidebarHandle { SidebarHandle() }

    func updateNSView(_ view: SidebarHandle, context: Context) {
        view.update(edge: edge, expanded: expanded,
                    content: SidebarContent(state: state, expanded: expanded, enabled: enabled, scheme: scheme,
                                            reveal: reveal, search: search, add: add, settings: settings))
    }

    static func dismantleNSView(_ view: SidebarHandle, coordinator: ()) { view.detach() }
}

final class SidebarHandle: NSView {
    private var accessory: NSTitlebarAccessoryViewController?
    private var button: SidebarBar?
    private var clip: SidebarClip?
    private var edge: CGFloat = SidebarWidth.initial
    private var expanded = true

    fileprivate func update(edge: CGFloat, expanded: Bool, content: SidebarContent) {
        self.edge = edge
        self.expanded = expanded
        if button == nil { button = SidebarBar() }
        button?.update(content)
        attach()
        clipButton()
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil { detach() } else { attach() }
    }

    func detach() {
        if let accessory, let window = accessory.view.window,
           let index = window.titlebarAccessoryViewControllers.firstIndex(of: accessory) {
            window.removeTitlebarAccessoryViewController(at: index)
        }
        accessory = nil
        clip = nil
    }

    private func attach() {
        guard accessory == nil, let window, let button else { return }
        let clip = SidebarClip(frame: NSRect(x: 0, y: 0, width: 44, height: 36))
        clip.wantsLayer = true
        clip.clipsToBounds = false
        button.translatesAutoresizingMaskIntoConstraints = false
        clip.addSubview(button)
        NSLayoutConstraint.activate([
            button.leadingAnchor.constraint(equalTo: clip.leadingAnchor),
            button.trailingAnchor.constraint(equalTo: clip.trailingAnchor),
            button.centerYAnchor.constraint(equalTo: clip.centerYAnchor),
            button.heightAnchor.constraint(equalToConstant: 36)
        ])
        clip.onLayout = { [weak self] in self?.clipButton() }
        let accessory = NSTitlebarAccessoryViewController()
        // Keep the button fixed while the sidebar edge reveals it.
        accessory.layoutAttribute = .left
        accessory.view = clip
        self.clip = clip
        self.accessory = accessory
        window.addTitlebarAccessoryViewController(accessory)
        clipButton()
    }

    private func clipButton() {
        guard let clip else { return }
        let origin = clip.convert(NSPoint.zero, to: nil).x
        let width = expanded ? max(0, edge - origin) : 44
        if abs(clip.frame.width - width) > 0.5 { clip.setFrameSize(NSSize(width: width, height: clip.frame.height)) }
        if expanded {
            clip.layer?.mask = nil
            return
        }
        let left = min(clip.bounds.width, max(0, edge - origin))
        let mask = CAShapeLayer()
        mask.frame = clip.bounds
        mask.path = CGPath(rect: NSRect(x: left, y: 0, width: clip.bounds.width - left,
                                      height: clip.bounds.height), transform: nil)
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        clip.layer?.mask = mask
        CATransaction.commit()
    }
}

private struct SidebarContent {
    let state: SidebarState
    let expanded: Bool
    let enabled: Bool
    let scheme: ColorScheme
    let reveal: () -> Void
    let search: () -> Void
    let add: () -> Void
    let settings: () -> Void

}

private final class SidebarBar: NSView {
    private let group = NSGlassEffectView()
    private let settings = NSGlassEffectView()
    private let reveal = NSGlassEffectView()
    private var buttons: [NSButton] = []
    private var actions: [() -> Void] = []

    init() {
        super.init(frame: .zero)
        clipsToBounds = false
        let titles = [("Search", "magnifyingglass"), ("New chat", "plus"),
                      ("Settings", "gearshape"), ("Show sidebar", "sidebar.left")]
        for (index, item) in titles.enumerated() {
            let button = NSButton(frame: NSRect(x: 0, y: 0, width: 36, height: 36))
            button.isBordered = false
            button.imagePosition = .imageOnly
            button.image = NSImage(systemSymbolName: item.1, accessibilityDescription: nil)?
                .withSymbolConfiguration(NSImage.SymbolConfiguration(pointSize: 17, weight: .regular))
            button.contentTintColor = .labelColor
            button.toolTip = item.0
            button.setAccessibilityLabel(item.0)
            button.target = self
            button.action = #selector(activate(_:))
            button.tag = index
            buttons.append(button)
        }
        let pair = NSView(frame: NSRect(x: 0, y: 0, width: 72, height: 36))
        pair.addSubview(buttons[0])
        pair.addSubview(buttons[1])
        buttons[1].setFrameOrigin(NSPoint(x: 36, y: 0))
        group.contentView = pair
        settings.contentView = buttons[2]
        reveal.contentView = buttons[3]
        for glass in [group, settings, reveal] {
            glass.cornerRadius = 18
            glass.style = .regular
            if #available(macOS 27, *) { glass.effectIsInteractive = true }
            addSubview(glass)
        }
    }

    required init?(coder: NSCoder) { nil }

    func update(_ content: SidebarContent) {
        appearance = NSAppearance(named: content.scheme == .dark ? .darkAqua : .aqua)
        actions = [content.search, content.add, content.settings, content.reveal]
        group.isHidden = !content.expanded
        settings.isHidden = !content.expanded
        reveal.isHidden = content.expanded
        for button in buttons { button.isEnabled = content.enabled }
        buttons[3].isEnabled = content.enabled && content.state.stage == .collapsed
        needsLayout = true
    }

    override func layout() {
        super.layout()
        let y = (bounds.height - 36) / 2
        group.frame = NSRect(x: bounds.width - 128, y: y, width: 72, height: 36)
        settings.frame = NSRect(x: bounds.width - 48, y: y, width: 36, height: 36)
        reveal.frame = NSRect(x: (bounds.width - 36) / 2, y: y, width: 36, height: 36)
    }

    @objc private func activate(_ button: NSButton) {
        actions[button.tag]()
    }
}

private final class SidebarClip: NSView {
    var onLayout: (() -> Void)?

    override func layout() {
        super.layout()
        onLayout?()
    }
}
#endif
