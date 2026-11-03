import Foundation

enum SidebarWidth {
    static let minimum: CGFloat = 260
    static let maximum: CGFloat = 400
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
    var stage: SidebarStage

    init(stage: SidebarStage = .expanded) {
        self.stage = stage
        if stage == .collapsed { edge = 0 }
    }
    @ObservationIgnored var edge: CGFloat = SidebarWidth.initial {
        didSet { handle?.moveEdge(edge) }
    }
    @ObservationIgnored weak var anchor: SidebarAnchor?
    @ObservationIgnored weak var handle: SidebarHandle?

    func collapse() { anchor?.collapse() }
    func beginReveal() -> UUID? { anchor?.beginReveal() }
    func endReveal(_ token: UUID) { anchor?.endReveal(token) }
}

struct SidebarLimits: NSViewRepresentable {
    @Binding var width: Double
    let state: SidebarState
    let minimum: CGFloat

    func makeNSView(context: Context) -> SidebarAnchor {
        let view = SidebarAnchor()
        updateNSView(view, context: context)
        return view
    }

    func updateNSView(_ view: SidebarAnchor, context: Context) {
        view.state = state
        state.anchor = view
        view.savedWidth = width
        view.minimum = minimum
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
    var minimum = PaneWidth.window
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

    @objc private func resized(_ notification: Notification) {
        publishEdge()
        schedule()
    }

    private func track(_ event: NSEvent) -> NSEvent? {
        guard let split, let item, let window else { return event }
        if dragging {
            if event.type == .leftMouseUp {
                dragging = false
                snapped = false
                let actual = item.viewController.view.frame.width
                if !item.isCollapsed { onWidth?(Double(min(SidebarWidth.maximum, max(SidebarWidth.minimum, actual)))) }
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
                collapse()
            } else {
                split.setPosition(min(SidebarWidth.maximum, max(SidebarWidth.minimum, position)), ofDividerAt: 0)
                split.layoutSubtreeIfNeeded()
                publishEdge()
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

    /// Starts tracking a reveal that SwiftUI animates, so the toolbar follows the edge.
    func beginReveal() -> UUID? {
        guard window != nil, split != nil, item != nil, movement == nil else { return nil }
        let token = UUID()
        movement = token
        publish(.expanding)
        trackEdge()
        return token
    }

    func endReveal(_ token: UUID) {
        guard let item, let split else { return }
        finish(token, item: item, split: split)
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

    private func trackEdge() {
        revealTimer?.invalidate()
        let timer = Timer(timeInterval: 1.0 / 60, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.publishEdge() }
        }
        RunLoop.main.add(timer, forMode: .common)
        revealTimer = timer
    }

    func collapse() {
        guard let item, let split, movement == nil, !item.isCollapsed else { return }
        let token = UUID()
        movement = token
        publish(.collapsing)
        trackEdge()
        // Apply the toolbar diff before AppKit starts changing column geometry.
        DispatchQueue.main.async { [weak self, weak item, weak split] in
            guard let self, let item, let split, self.movement == token,
                  self.item === item, self.split === split else { return }
            if NSWorkspace.shared.accessibilityDisplayShouldReduceMotion {
                item.isCollapsed = true
                self.finish(token, item: item, split: split)
            } else {
                NSAnimationContext.runAnimationGroup { @Sendable [weak self] context in
                    context.duration = Motion.columnTime
                    context.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
                    context.allowsImplicitAnimation = true
                    MainActor.assumeIsolated {
                        guard let self, self.movement == token, let item = self.item, let split = self.split,
                              let controller = split.delegate as? NSSplitViewController else { return }
                        if !item.isCollapsed { controller.toggleSidebar(nil) }
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
        guard let window else { return }
        if window.title != "Polymux" { window.title = "Polymux" }
        window.titleVisibility = .hidden
        window.standardWindowButton(.closeButton)?.setAccessibilityLabel("Close")
        window.standardWindowButton(.miniaturizeButton)?.setAccessibilityLabel("Minimize")
        window.standardWindowButton(.zoomButton)?.setAccessibilityLabel("Full screen")
        if abs(window.contentMinSize.width - minimum) > 0.5 {
            window.contentMinSize.width = minimum
            if window.contentLayoutRect.width < minimum {
                window.setContentSize(NSSize(width: minimum, height: window.contentLayoutRect.height))
            }
        }
        if split == nil || item == nil { attach() }
        guard let split, let item else { return }
        publishEdge()
        if item.minimumThickness != SidebarWidth.minimum { item.minimumThickness = SidebarWidth.minimum }
        if item.maximumThickness != SidebarWidth.maximum { item.maximumThickness = SidebarWidth.maximum }
        if !item.canCollapse { item.canCollapse = true }
        if item.collapseBehavior != .preferResizingSiblingsWithFixedSplitView {
            item.collapseBehavior = .preferResizingSiblingsWithFixedSplitView
        }
        guard movement == nil, !dragging else { return }
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
            let target = min(SidebarWidth.maximum, max(SidebarWidth.minimum, CGFloat(savedWidth)))
            if abs(item.viewController.view.frame.width - target) > 0.5 {
                split.setPosition(target, ofDividerAt: 0)
            }
        }
        let restored = item.viewController.view.frame.width
        if restored < SidebarWidth.minimum || restored > SidebarWidth.maximum {
            split.setPosition(min(SidebarWidth.maximum, max(SidebarWidth.minimum, restored)), ofDividerAt: 0)
        } else {
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

struct SidebarControl: View {
    let state: SidebarState
    let reveal: () -> Void
    let search: () -> Void
    let menu: [PluginAction]
    let settings: () -> Void

    var body: some View {
        SidebarBridge(state: state, stage: state.stage,
                      reveal: reveal, search: search, menu: menu, settings: settings)
    }
}

private struct SidebarBridge: NSViewRepresentable {
    let state: SidebarState
    let stage: SidebarStage
    let reveal: () -> Void
    let search: () -> Void
    let menu: [PluginAction]
    let settings: () -> Void
    @Environment(\.isEnabled) private var enabled
    @Environment(\.colorScheme) private var scheme

    func makeNSView(context: Context) -> SidebarHandle { SidebarHandle() }

    func updateNSView(_ view: SidebarHandle, context: Context) {
        state.handle = view
        view.update(edge: state.edge,
                    content: SidebarContent(stage: stage, enabled: enabled, scheme: scheme,
                                            reveal: reveal, search: search, menu: menu, settings: settings))
    }

    static func dismantleNSView(_ view: SidebarHandle, coordinator: ()) { view.detach() }
}

final class SidebarHandle: NSView {
    private var accessory: NSTitlebarAccessoryViewController?
    private var button: SidebarBar?
    private var clip: SidebarClip?
    private var moves: [NSObjectProtocol] = []
    private var edge: CGFloat = SidebarWidth.initial
    private var scheduled = false

    fileprivate func update(edge: CGFloat, content: SidebarContent) {
        self.edge = edge
        if button == nil { button = SidebarBar() }
        button?.update(content)
        attach()
        clipButton()
    }

    func moveEdge(_ edge: CGFloat) {
        self.edge = edge
        clipButton()
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil { detach() } else { attach() }
    }

    func detach() {
        clip?.onLayout = nil
        moves.forEach(NotificationCenter.default.removeObserver)
        moves = []
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
        clip.addSubview(button)
        clip.onLayout = { [weak self] in self?.clipButton() }
        let accessory = NSTitlebarAccessoryViewController()
        // Keep the button fixed while the sidebar edge reveals it.
        accessory.layoutAttribute = .left
        accessory.view = clip
        self.clip = clip
        self.accessory = accessory
        window.addTitlebarAccessoryViewController(accessory)
        // AppKit settles the accessory's position after the first layout; a move alone
        // doesn't relayout the clip, so its width would keep the stale origin.
        var view: NSView? = clip
        while let moved = view {
            moved.postsFrameChangedNotifications = true
            moves.append(NotificationCenter.default.addObserver(forName: NSView.frameDidChangeNotification, object: moved, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.clipButton() }
            })
            view = moved.superview
        }
        clipButton()
    }

    private func clipButton() {
        guard clip != nil, !scheduled else { return }
        scheduled = true
        // Frame notifications can arrive while SwiftUI is replacing the window toolbar.
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.scheduled = false
            self.updateClip()
        }
    }

    private func updateClip() {
        guard let clip else { return }
        let origin = clip.convert(NSPoint.zero, to: nil).x
        let visible = max(0, edge - origin)
        let width = max(44, visible)
        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0
            context.allowsImplicitAnimation = false
            if abs(clip.frame.width - width) > 0.5 { clip.setFrameSize(NSSize(width: width, height: clip.frame.height)) }
            button?.frame = NSRect(x: 0, y: (clip.bounds.height - 36) / 2, width: width, height: 36)
            button?.moveEdge(visible)
        }
    }
}

private struct SidebarContent {
    let stage: SidebarStage
    let enabled: Bool
    let scheme: ColorScheme
    let reveal: () -> Void
    let search: () -> Void
    let menu: [PluginAction]
    let settings: () -> Void

}

private final class SidebarBar: NSView {
    private let controls = NSView()
    private var visible: CGFloat = SidebarWidth.initial
    private var occluding = false
    private var fading = false
    private let group = NSGlassEffectView()
    private let settings = NSGlassEffectView()
    private let reveal = NSGlassEffectView()
    private var buttons: [NSButton] = []
    private var actions: [() -> Void] = []
    private var creations: [PluginAction] = []

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
        }
        controls.wantsLayer = true
        controls.clipsToBounds = false
        controls.addSubview(group)
        controls.addSubview(settings)
        addSubview(reveal)
        addSubview(controls)
    }

    required init?(coder: NSCoder) { nil }

    func update(_ content: SidebarContent) {
        appearance = NSAppearance(named: content.scheme == .dark ? .darkAqua : .aqua)
        actions = [content.search, {}, content.settings, content.reveal]
        creations = content.menu
        controls.isHidden = content.stage == .collapsed
        controls.clipsToBounds = content.stage.moving
        occluding = content.stage == .collapsing
        switch content.stage {
        case .expanded:
            reveal.layer?.removeAnimation(forKey: "revealFade")
            fading = false
            reveal.isHidden = true
            reveal.alphaValue = 1
        case .expanding:
            reveal.isHidden = false
            if NSWorkspace.shared.accessibilityDisplayShouldReduceMotion {
                reveal.layer?.removeAnimation(forKey: "revealFade")
                reveal.alphaValue = 0
                fading = false
            } else if !fading {
                fading = true
                let fade = CABasicAnimation(keyPath: "opacity")
                fade.fromValue = 1
                fade.toValue = 0
                fade.duration = 0.18
                fade.timingFunction = CAMediaTimingFunction(name: .easeOut)
                reveal.layer?.add(fade, forKey: "revealFade")
                reveal.alphaValue = 0
            }
        case .collapsing, .collapsed:
            reveal.layer?.removeAnimation(forKey: "revealFade")
            fading = false
            reveal.isHidden = false
            reveal.alphaValue = 1
        }
        for button in buttons { button.isEnabled = content.enabled && !content.stage.moving }
        buttons[3].isEnabled = content.enabled && content.stage == .collapsed
        needsLayout = true
    }

    func moveEdge(_ visible: CGFloat) {
        guard self.visible != visible else { return }
        self.visible = visible
        needsLayout = true
    }

    override func layout() {
        super.layout()
        let y = (bounds.height - 36) / 2
        controls.frame = NSRect(x: 0, y: y, width: visible, height: 36)
        group.frame = NSRect(x: visible - 128, y: 0, width: 72, height: 36)
        settings.frame = NSRect(x: visible - 48, y: 0, width: 36, height: 36)
        reveal.frame = NSRect(x: 4, y: y, width: 36, height: 36)
        let left = min(36, max(0, visible - 4))
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        // Mask only while the sidebar edge reveals the disabled control. A mask at
        // rest clips the glass press effect to the control's bounds.
        if occluding && left > 0 {
            let mask = CAShapeLayer()
            mask.path = CGPath(rect: CGRect(x: left, y: 0, width: 36 - left, height: 36), transform: nil)
            reveal.layer?.mask = mask
        } else {
            reveal.layer?.mask = nil
        }
        CATransaction.commit()
    }

    @objc private func activate(_ button: NSButton) {
        if button.tag == 1 {
            // Opened once the press finishes: the menu's tracking loop would otherwise hold an accessibility press open.
            perform(#selector(showCreate(_:)), with: button, afterDelay: 0, inModes: [.default])
            return
        }
        actions[button.tag]()
    }

    @objc private func showCreate(_ button: NSButton) {
        let popup = NSMenu()
        popup.autoenablesItems = false
        popup.allowsContextMenuPlugIns = false
        for (tag, action) in creations.enumerated() {
            let item = NSMenuItem(title: action.title, action: #selector(create(_:)), keyEquivalent: "")
            item.target = self
            item.tag = tag
            popup.addItem(item)
        }
        // Clear the menu's top inset as well as the button.
        let y = button.isFlipped ? button.bounds.maxY + 12 : button.bounds.minY - 12
        popup.popUp(positioning: nil, at: NSPoint(x: 0, y: y), in: button)
    }

    @objc private func create(_ item: NSMenuItem) { creations[item.tag].run() }
}

private final class SidebarClip: NSView {
    var onLayout: (() -> Void)?

    override func layout() {
        super.layout()
        onLayout?()
    }
}
#endif
