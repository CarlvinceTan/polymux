import SwiftUI
import Observation
#if os(macOS)
import AppKit
#endif

/// Tells a pointer the user moved from content that slid under a still one.
@MainActor
enum PointerIntent {
    #if os(macOS)
    private static let settle: Duration = .milliseconds(180)
    private static var last = NSEvent.mouseLocation
    private static var movedAt = ContinuousClock.now - .seconds(1)
    private static var scrolledAt = ContinuousClock.now - .seconds(1)
    private static var monitor: Any?

    /// Scrolling moves rows under the pointer, so it never counts as intent.
    static func start() {
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: .scrollWheel) { event in
            scrolledAt = .now
            return event
        }
    }

    /// True when the pointer itself moved. Regions entered by one move all agree.
    static func moved() -> Bool {
        let point = NSEvent.mouseLocation
        let now = ContinuousClock.now
        if point != last {
            last = point
            if scrolledAt.duration(to: now) >= settle { movedAt = now }
        }
        return movedAt.duration(to: now) < .milliseconds(50)
    }
    #else
    static func start() {}
    static func moved() -> Bool { true }
    #endif
}

extension View {
    /// Hover that follows the pointer, not rows arriving beneath a still one. Leaving always counts.
    func pointerHover(_ changed: @escaping (Bool) -> Void) -> some View {
        onHover { inside in
            if !inside || PointerIntent.moved() { changed(inside) }
        }
    }
}

/// The hovered row. Only the rows it names redraw, and settling on one lets the app prepare it.
@MainActor @Observable
final class RowHover {
    private(set) var id: String?
    @ObservationIgnored var settled: (String) -> Void = { _ in }
    @ObservationIgnored private var dwell: Task<Void, Never>?

    func update(_ row: String, inside: Bool) {
        if inside {
            guard id != row else { return }
            id = row
            dwell?.cancel()
            dwell = Task { [weak self] in
                // A pointer crossing the list on its way elsewhere leaves a row sooner than this.
                try? await Task.sleep(for: .milliseconds(70))
                guard !Task.isCancelled else { return }
                self?.settled(row)
            }
        } else if id == row {
            id = nil
            dwell?.cancel()
        }
    }
}
