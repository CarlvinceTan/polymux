import SwiftUI
import PolymuxKit
import CoreTransferable

struct SidebarDrop: DropDelegate {
    let item: SidebarItem?
    let origin: CGPoint
    let frames: [SidebarArea: CGRect]
    let sections: [String]
    let pins: [String]
    @Binding var target: SidebarDestination?
    let receive: @MainActor @Sendable (SidebarItem, SidebarDestination) -> Bool

    func validateDrop(info: DropInfo) -> Bool {
        item != nil && info.hasItemsConforming(to: [SidebarItem.contentType.identifier])
    }

    func dropEntered(info: DropInfo) { target = destination(info) }

    func dropUpdated(info: DropInfo) -> DropProposal? {
        target = destination(info)
        return DropProposal(operation: target == nil ? .forbidden : .move)
    }

    func dropExited(info: DropInfo) { target = nil }

    func performDrop(info: DropInfo) -> Bool {
        guard let destination = destination(info) else { target = nil; return false }
        target = nil
        guard let provider = info.itemProviders(for: [SidebarItem.contentType.identifier]).first else { return false }
        let receive = receive
        _ = provider.loadTransferable(type: SidebarItem.self) { result in
            guard case let .success(item) = result else { return }
            Task { @MainActor in _ = receive(item, destination) }
        }
        return true
    }

    private func destination(_ info: DropInfo) -> SidebarDestination? {
        guard let item else { return nil }
        let point = CGPoint(x: info.location.x + origin.x, y: info.location.y + origin.y)
        return DropTarget.sidebar(at: point, item: item, frames: frames, sections: sections, pins: pins)
    }
}

struct SidebarFeedback: View {
    let frames: [SidebarArea: CGRect]
    let target: SidebarDestination?

    var body: some View {
        GeometryReader { _ in
            if let target {
                switch target {
                case let .section(id, after):
                    let area: SidebarArea = id == SidebarLayout.unassignedID ? .unassigned : .section(id)
                    if let frame = frames[area] {
                        Rectangle().fill(Color.primary.opacity(0.3))
                            .frame(width: frame.width, height: 1)
                            .position(x: frame.midX, y: after ? frame.maxY : frame.minY)
                    }
                case let .chats(area): highlight(area)
                case .pin: highlight(.pins)
                }
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    @ViewBuilder private func highlight(_ area: SidebarArea) -> some View {
        if let frame = frames[area] {
            RoundedRectangle(cornerRadius: 8).fill(Color.primary.opacity(0.06))
                .frame(width: frame.width, height: frame.height)
                .position(x: frame.midX, y: frame.midY)
        }
    }
}

struct SidebarFrames: PreferenceKey {
    static let defaultValue: [SidebarArea: CGRect] = [:]

    static func reduce(value: inout [SidebarArea: CGRect], nextValue: () -> [SidebarArea: CGRect]) {
        value.merge(nextValue(), uniquingKeysWith: { $0.union($1) })
    }
}

extension View {
    func sidebarArea(_ area: SidebarArea) -> some View {
        background {
            GeometryReader { geometry in
                Color.clear.preference(key: SidebarFrames.self,
                                       value: [area: geometry.frame(in: .named("chatList"))])
            }
        }
    }

    func sidebarDrag<Preview: View>(_ item: SidebarItem, active: Binding<SidebarItem?>,
                                   @ViewBuilder preview: () -> Preview) -> some View {
        onDrag {
            active.wrappedValue = item
            return item.provider()
        } preview: {
            preview()
        }
        .dragConfiguration(DragConfiguration(allowMove: true))
        .onDragSessionUpdated { session in
            switch session.phase {
            case .initial, .active: active.wrappedValue = item
            case .ended, .dataTransferCompleted: active.wrappedValue = nil
            default: break
            }
        }
    }
}
