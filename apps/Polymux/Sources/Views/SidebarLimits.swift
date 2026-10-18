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
        withTransaction(transaction) { state?.stage = stage }
    }

    private func move(_ collapsed: Bool) {
        guard let item, let split else { return }
        let token = UUID()
        movement = token
        publish(collapsed ? .collapsing : .expanding)
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
                    MainActor.assumeIsolated {
                        guard let self, self.movement == token, let item = self.item else { return }
                        item.animator().isCollapsed = collapsed
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
    }

    private func configure() {
        guard window != nil else { return }
        if split == nil || item == nil { attach() }
        guard let split, let item else { return }
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
}
#endif
