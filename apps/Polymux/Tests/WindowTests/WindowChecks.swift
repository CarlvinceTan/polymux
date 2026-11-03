import AppKit
import SwiftUI

@main
struct WindowChecks {
    @MainActor static func main() {
        NSApplication.shared.setActivationPolicy(.prohibited)
        let state = SidebarState()
        var reveals = 0
        let root = ZStack {
            Color.clear
                .panelBar(value: 0) {
                    HStack {
                        Spacer()
                        Button("Chat action") {}
                    }
                }
                .panelBarHost()
            SidebarControl(state: state, reveal: { reveals += 1 }, search: {}, menu: [], settings: {})
                .frame(width: 0, height: 0)
        }
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1000, height: 760),
                              styleMask: [.titled, .closable, .resizable, .fullSizeContentView],
                              backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.contentView = NSHostingView(rootView: root)
        defer { window.close() }
        settle(window)
        guard let frame = window.contentView?.superview else { fatalError("Missing window frame") }
        require(descendants(frame).contains { $0 is PanelHosting }, "The real panel bar was not installed")

        for _ in 0..<3 {
            state.stage = .collapsed
            state.edge = 0
            settle(window)
            let reveal = button("Show sidebar", in: frame)
            require(glass(of: reveal).layer?.mask == nil, "The reveal glass is masked at rest, clipping its press effect")
            state.stage = .collapsing
            state.edge = 200
            settle(window)
            require(glass(of: reveal).layer?.mask != nil, "The reveal control is not masked while the sidebar covers it")
            state.stage = .collapsed
            state.edge = 0
            settle(window)
            require(glass(of: reveal).layer?.mask == nil, "The reveal glass stays masked after the sidebar collapses")
            let point = reveal.convert(NSPoint(x: reveal.bounds.midX, y: reveal.bounds.midY), to: frame)
            require(frame.hitTest(point) === reveal, "Chat toolbar intercepts the sidebar's mouse hit")
            (frame.hitTest(point) as? NSButton)?.performClick(nil)
            state.stage = .expanding
            state.edge = 100
            settle(window)
            require(glass(of: reveal).layer?.mask == nil, "The reveal glass is masked while opening")
            state.stage = .expanded
            state.edge = 320
            settle(window)
            let settings = button("Settings", in: frame)
            require(!glass(of: settings).superview!.clipsToBounds, "Settings glass is clipped to the button height")
            let overlay = descendants(frame).first { $0 is PanelHosting }!
            let chatPoint = NSPoint(x: overlay.frame.maxX - 20, y: overlay.frame.midY)
            require(overlay.hitTest(chatPoint) != nil, "Chat toolbar actions lost their hit area")
        }
        require(reveals == 3, "Sidebar reveal action was not delivered each time")
        guard let clip = window.titlebarAccessoryViewControllers.map(\.view).first(where: {
            descendants($0).contains { ($0 as? NSButton)?.accessibilityLabel() == "Settings" }
        }), let bar = clip.subviews.first else { fatalError("Missing sidebar accessory") }
        let probe = LayoutProbe()
        var moving = false
        var layouts = 0
        probe.onLayout = {
            require(!moving, "Sidebar forced layout inside a titlebar frame-change notification")
            layouts += 1
        }
        bar.addSubview(probe)
        probe.needsLayout = true
        moving = true
        NotificationCenter.default.post(name: NSView.frameDidChangeNotification, object: clip)
        moving = false
        settle(window)
        require(layouts > 0, "Sidebar did not finish its deferred layout")
        print("Window controls passed: native sidebar hits, reveal glass masks, Settings glass bounds, chat toolbar hits, deferred titlebar layout")
    }

    @MainActor private static func settle(_ window: NSWindow) {
        let end = Date().addingTimeInterval(0.15)
        while Date() < end {
            RunLoop.main.run(until: Date().addingTimeInterval(0.01))
            window.contentView?.superview?.layoutSubtreeIfNeeded()
        }
    }

    @MainActor private static func descendants(_ view: NSView) -> [NSView] {
        [view] + view.subviews.flatMap(descendants)
    }

    @MainActor private static func button(_ label: String, in view: NSView) -> NSButton {
        guard let button = button(named: label, in: view), !button.isHiddenOrHasHiddenAncestor else {
            fatalError("Missing \(label) button")
        }
        return button
    }

    @MainActor private static func button(named label: String, in view: NSView) -> NSButton? {
        descendants(view).compactMap { $0 as? NSButton }.first { $0.accessibilityLabel() == label }
    }

    @MainActor private static func glass(of view: NSView) -> NSGlassEffectView {
        var parent = view.superview
        while let view = parent, !(view is NSGlassEffectView) { parent = view.superview }
        guard let glass = parent as? NSGlassEffectView else { fatalError("Missing glass container") }
        return glass
    }

    private static func require(_ condition: Bool, _ message: String) {
        guard condition else {
            print("FAIL: \(message)")
            exit(1)
        }
    }
}

private final class LayoutProbe: NSView {
    var onLayout: (() -> Void)?

    override func layout() {
        super.layout()
        onLayout?()
    }
}
