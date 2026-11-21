import SwiftUI

struct ConnectionStatus: View {
    let connected: Bool
    @Binding var dismissed: Bool
    var collapses = false
    @State private var shown = false
    @State private var everConnected = false
    @State private var stalled = false
    @Environment(\.serverUpdating) private var updating

    private var visible: Bool { updating || !connected && shown && !dismissed }

    var body: some View {
        Group {
            if collapses {
                status
            } else {
                bubble.hidden().accessibilityHidden(true).overlay { status }
            }
        }
            .frame(maxWidth: .infinity)
            .accessibilityIdentifier("connection.status")
            .task(id: connected) {
                if connected {
                    everConnected = true
                    shown = false
                    stalled = false
                    dismissed = false
                    return
                }
                try? await Task.sleep(for: .seconds(3))
                guard !Task.isCancelled else { return }
                shown = true
                try? await Task.sleep(for: .seconds(9))
                if !Task.isCancelled { stalled = true }
            }
    }

    private var status: some View {
        ZStack {
            if visible { bubble.transition(.opacity) }
        }
        .motion(Motion.fade, value: visible)
    }

    private var bubble: some View {
        WarningBubble(text: updating ? "Server updating…" : everConnected || stalled ? "Can’t reach server" : "Connecting…",
                      symbol: updating || !everConnected && !stalled ? "arrow.triangle.2.circlepath" : "exclamationmark.triangle") {
            dismissed = true
        }
    }
}

extension EnvironmentValues {
    @Entry var connectionDismissed: Binding<Bool> = .constant(false)
    /// The server is applying an update: a quiet status, not an error.
    @Entry var serverUpdating = false
}
