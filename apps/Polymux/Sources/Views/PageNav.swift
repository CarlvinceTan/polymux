import SwiftUI

/// Drives page pushes inside one panel: pages slide in from the side they lead to.
@MainActor @Observable
final class PageNav {
    private(set) var back = false

    private var queue: [(back: Bool, change: () -> Void)] = []

    func push(_ change: @escaping () -> Void) { go(back: false, change) }
    func pop(_ change: @escaping () -> Void) { go(back: true, change) }

    /// Drops changes still waiting to run, such as a push from a tab that was left.
    func cancel() { queue.removeAll() }

    /// Changes run in order; a changed direction renders first, so the leaving page takes it too.
    private func go(back: Bool, _ change: @escaping () -> Void) {
        if queue.isEmpty && self.back == back { return withAnimation(Motion.page) { change() } }
        queue.append((back, change))
        if queue.count == 1 { step() }
    }

    private func step() {
        guard let next = queue.first else { return }
        guard back != next.back else { return run() }
        back = next.back
        DispatchQueue.main.async { self.run() }
    }

    private func run() {
        guard !queue.isEmpty else { return }
        let next = queue.removeFirst()
        withAnimation(Motion.page) { next.change() }
        if !queue.isEmpty { DispatchQueue.main.async { self.step() } }
    }

    func transition(reduced: Bool) -> AnyTransition {
        if reduced { return .opacity }
        let shift: CGFloat = 36
        // The leaving page clears quickly, so the two never read as one.
        return .asymmetric(
            insertion: .opacity.combined(with: .offset(x: back ? -shift : shift)),
            removal: .opacity.combined(with: .offset(x: back ? shift / 2 : -shift / 2)).animation(Motion.quick))
    }
}

/// What a page shows in its host's header.
struct PageCrumb: Equatable {
    /// The page publishing it, so two pages with the same title stay apart.
    let id: UUID
    let title: String
    let enabled: Bool
    let back: PanelAction?
    let close: PanelAction
    let accessory: PanelAccessory?

    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.id == rhs.id && lhs.title == rhs.title && lhs.enabled == rhs.enabled && (lhs.back == nil) == (rhs.back == nil)
            && lhs.accessory?.symbol == rhs.accessory?.symbol && lhs.accessory?.label == rhs.accessory?.label
    }
}

struct PageCrumbs: PreferenceKey {
    static var defaultValue: PageCrumb? { nil }
    static func reduce(value: inout PageCrumb?, nextValue: () -> PageCrumb?) {
        if let next = nextValue() { value = next }
    }
}

extension EnvironmentValues {
    /// Set inside a panel that hosts pages; a PanelView there draws only its body.
    @Entry var pageNav: PageNav?
}

extension Optional where Wrapped == PageNav {
    @MainActor func push(_ change: @escaping () -> Void) {
        if let self { self.push(change) } else { change() }
    }

    @MainActor func pop(_ change: @escaping () -> Void) {
        if let self { self.pop(change) } else { change() }
    }
}

/// Slides a hosted page in and out with the host's direction.
private struct PageSlide: ViewModifier {
    let nav: PageNav?
    @Environment(\.accessibilityReduceMotion) private var reduced

    func body(content: Content) -> some View {
        if let nav { content.transition(nav.transition(reduced: reduced)) } else { content }
    }
}

extension View {
    func pageSlide(_ nav: PageNav?) -> some View { modifier(PageSlide(nav: nav)) }
}
