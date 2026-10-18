import SwiftUI

extension View {
    func panelDismiss(enabled: Bool = true, onClose: @escaping () -> Void) -> some View {
        modifier(ExitObserver(enabled: enabled, onClose: onClose))
    }
}

private struct ExitObserver: ViewModifier {
    let enabled: Bool
    let onClose: () -> Void
    @Environment(\.panelContext) private var panel
    @State private var id = UUID()

    func body(content: Content) -> some View {
        #if os(macOS)
        content.preference(key: PanelDismissal.self, value: PanelExit(id: id, enabled: enabled,
                                                                     close: PanelAction(action: close)))
        #else
        content.background {
            PanelObserver(enabled: enabled, onClose: close)
                .frame(width: 0, height: 0)
                .accessibilityHidden(true)
        }
        #endif
    }

    private func close() {
        if let panel { panel.end(onClose) }
        else { onClose() }
    }
}

#if os(iOS)
import UIKit

private struct PanelObserver: UIViewControllerRepresentable {
    let enabled: Bool
    let onClose: () -> Void

    func makeUIViewController(context: Context) -> PanelController {
        PanelController()
    }

    func updateUIViewController(_ controller: PanelController, context: Context) {
        controller.enabled = enabled
        controller.onClose = onClose
        controller.attach()
    }

    static func dismantleUIViewController(_ controller: PanelController, coordinator: ()) {
        controller.detach()
    }
}

private final class PanelController: UIViewController, UIGestureRecognizerDelegate {
    var enabled = true
    var onClose: () -> Void = {}
    private weak var presentation: UIPresentationController?
    private lazy var tap = UITapGestureRecognizer(target: self, action: #selector(close))

    override func loadView() {
        view = UIView()
        view.isUserInteractionEnabled = false
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        attach()
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        attach()
    }

    func attach() {
        var controller = parent
        while let current = controller {
            if current.presentingViewController != nil, let next = current.presentationController,
               let container = next.containerView {
                guard presentation !== next || tap.view !== container else { return }
                detach()
                presentation = next
                tap.delegate = self
                tap.cancelsTouchesInView = false
                tap.delaysTouchesBegan = false
                container.addGestureRecognizer(tap)
                return
            }
            controller = current.parent
        }
    }

    func detach() {
        tap.view?.removeGestureRecognizer(tap)
        presentation = nil
    }

    func gestureRecognizer(_ gestureRecognizer: UIGestureRecognizer, shouldReceive touch: UITouch) -> Bool {
        guard enabled, let presentation, let panel = presentation.presentedView,
              presentation.presentedViewController.presentedViewController == nil,
              !presentation.presentedViewController.isBeingDismissed else { return false }
        return !panel.point(inside: touch.location(in: panel), with: nil)
    }

    @objc private func close() {
        if enabled, tap.state == .ended { onClose() }
    }
}
#endif
