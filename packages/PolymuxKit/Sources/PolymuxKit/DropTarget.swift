import Foundation
import CoreGraphics

public enum SidebarArea: Hashable, Sendable {
    case pins
    case pin(String)
    case section(String)
    case unassigned
}

public enum SidebarDestination: Equatable, Sendable {
    case chats(SidebarArea)
    case section(String, after: Bool)
    case pin(before: String?)
}

public enum DropTarget {
    public static func sidebar(at point: CGPoint, item: SidebarItem, frames: [SidebarArea: CGRect],
                               sections: [String], pins: [String]) -> SidebarDestination? {
        if case let .section(id) = item {
            let targets = sections.compactMap { id -> (String, CGRect)? in
                let area: SidebarArea = id == SidebarLayout.unassignedID ? .unassigned : .section(id)
                return frames[area].map { (id, $0) }
            }
            guard let source = sections.firstIndex(of: id),
                  let target = targets.min(by: { distance(point, $0.1) < distance(point, $1.1) }),
                  let index = sections.firstIndex(of: target.0), source != index else { return nil }
            return .section(target.0, after: source < index)
        }
        if frames[.pins]?.contains(point) == true {
            return .pin(before: pin(at: point, frames: frames, pins: pins))
        }
        for id in sections {
            let area: SidebarArea = id == SidebarLayout.unassignedID ? .unassigned : .section(id)
            if frames[area]?.contains(point) == true { return .chats(area) }
        }
        return nil
    }

    public static func pins(_ pins: [String], moving id: String, before target: String?) -> [String] {
        guard id != target else { return pins }
        var result = pins.filter { $0 != id }
        result.insert(id, at: target.flatMap { result.firstIndex(of: $0) } ?? result.count)
        return result
    }

    private static func pin(at point: CGPoint, frames: [SidebarArea: CGRect], pins: [String]) -> String? {
        var rows: [[(String, CGRect)]] = []
        for id in pins {
            guard let frame = frames[.pin(id)] else { continue }
            if rows.last?.first?.1.minY == frame.minY { rows[rows.count - 1].append((id, frame)) }
            else { rows.append([(id, frame)]) }
        }
        for (index, row) in rows.enumerated() {
            let bottom = row.map { $0.1.maxY }.max() ?? 0
            let next = index + 1 < rows.count ? rows[index + 1][0].1.minY : bottom
            if point.y <= (bottom + next) / 2 {
                return row.first(where: { point.x <= $0.1.midX })?.0
                    ?? (index + 1 < rows.count ? rows[index + 1][0].0 : nil)
            }
        }
        return nil
    }

    private static func distance(_ point: CGPoint, _ frame: CGRect) -> CGFloat {
        pow(point.x - frame.midX, 2) + pow(point.y - frame.midY, 2)
    }

    public static func chat(at point: CGPoint, frames: [String: CGRect], window: CGRect) -> String? {
        let location = CGPoint(x: point.x - window.minX, y: window.maxY - point.y)
        return frames.first(where: { $0.value.contains(location) })?.key
    }
}

/// Opens a chat once a drag rests on it, so crossing the list or passing slowly over it opens nothing.
@MainActor
public final class DropDwell {
    private let wait: Duration
    private var held: String?
    private var rest = CGPoint.zero
    private var task: Task<Void, Never>?

    public init(wait: Duration = .milliseconds(350)) { self.wait = wait }

    public func hold(_ id: String?, at point: CGPoint, open: @escaping (String) -> Void) {
        guard id != held || hypot(point.x - rest.x, point.y - rest.y) > 6 else { return }
        held = id
        rest = point
        task?.cancel()
        guard let id else { return }
        task = Task { [wait] in
            try? await Task.sleep(for: wait)
            if !Task.isCancelled { open(id) }
        }
    }
}
