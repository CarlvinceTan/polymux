import SwiftUI

/// Whether a device is connected, shown as a small light.
struct StatusDot: View {
    enum State { case online, offline, pairing, checking, unknown }
    let state: State

    var body: some View {
        Group {
            switch state {
            case .online: Circle().fill(Color("Online"))
            case .offline: Circle().fill(Color("Offline"))
            case .unknown: Color.clear
            case .pairing, .checking:
                // Fills and empties while the state is still settling.
                Circle().strokeBorder(.secondary, lineWidth: 1.5)
                    .phaseAnimator([false, true]) { ring, filled in
                        ring.background(Circle().fill(.secondary).opacity(filled ? 1 : 0))
                    } animation: { _ in .easeInOut(duration: 0.9) }
            }
        }
        .frame(width: 8, height: 8)
        .accessibilityElement()
        .accessibilityLabel(label)
    }

    private var label: String {
        switch state {
        case .online: "Online"
        case .offline: "Offline"
        case .pairing: "Pairing"
        case .checking: "Checking"
        case .unknown: "Status unknown"
        }
    }
}
