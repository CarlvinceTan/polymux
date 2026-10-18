#if os(macOS)
import AppKit
import SwiftUI

struct SearchFocus: NSViewRepresentable {
    var sidebar = false
    var prompt: String?
    var blurred: (() -> Void)?

    func makeNSView(context: Context) -> SearchAnchor {
        let view = SearchAnchor()
        updateNSView(view, context: context)
        return view
    }

    func updateNSView(_ view: SearchAnchor, context: Context) {
        view.sidebar = sidebar
        view.prompt = prompt
        view.blurred = blurred
        view.schedule()
    }

    static func dismantleNSView(_ view: SearchAnchor, coordinator: ()) { view.detach() }
}

final class SearchAnchor: NSView {
    var sidebar = false
    var prompt: String?
    var blurred: (() -> Void)?
    private var scheduled = false

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        detach()
        guard let window else { return }
        NotificationCenter.default.addObserver(self, selector: #selector(windowUpdated(_:)),
                                               name: NSWindow.didUpdateNotification, object: window)
        NotificationCenter.default.addObserver(self, selector: #selector(editingBegan(_:)),
                                               name: NSControl.textDidBeginEditingNotification, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(editingEnded(_:)),
                                               name: NSControl.textDidEndEditingNotification, object: nil)
        schedule()
    }

    override func viewDidMoveToSuperview() {
        super.viewDidMoveToSuperview()
        schedule()
    }

    func detach() {
        NotificationCenter.default.removeObserver(self)
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

    @objc private func windowUpdated(_ notification: Notification) { schedule() }

    @objc private func editingBegan(_ notification: Notification) {
        guard let field = notification.object as? NSTextField, field.window === window else { return }
        configure()
    }

    @objc private func editingEnded(_ notification: Notification) {
        guard let field = notification.object as? NSTextField, field.window === window,
              fields().contains(where: { $0 === field }) else { return }
        blurred?()
    }

    private func configure() {
        for field in fields() {
            if field.focusRingType != .none {
                field.focusRingType = .none
                field.noteFocusRingMaskChanged()
            }
            if let cell = field.cell, cell.focusRingType != .none {
                cell.focusRingType = .none
                field.needsDisplay = true
            }
        }
    }

    private func fields() -> [NSTextField] {
        guard window != nil else { return [] }
        var ancestor = superview
        while let view = ancestor {
            if let split = view as? NSSplitView {
                guard sidebar, let controller = split.delegate as? NSSplitViewController,
                      let item = controller.splitViewItems.first,
                      isDescendant(of: item.viewController.view) else { return [] }
                let roots = [item.viewController.view] + item.topAlignedAccessoryViewControllers.map(\.view)
                return roots.flatMap { matches(in: $0) }
            }
            let found = matches(in: view)
            if !found.isEmpty { return found }
            ancestor = view.superview
        }
        return []
    }

    private func matches(in view: NSView) -> [NSTextField] {
        let fields = descendants(in: view).filter { !$0.isHidden && $0.isEditable && $0.window === window }
        if sidebar { return fields.filter { $0 is NSSearchField } }
        let exact = fields.filter { $0.placeholderString == prompt }
        if !exact.isEmpty { return exact }
        let containing = fields.filter { $0.bounds.contains($0.convert(bounds.origin, from: self)) }
        return containing.count == 1 ? containing : []
    }

    private func descendants(in view: NSView) -> [NSTextField] {
        if let field = view as? NSTextField {
            return [field]
        }
        return view.subviews.flatMap { descendants(in: $0) }
    }
}
#endif
