import SwiftUI

struct ConnectionStatus: View {
    let connected: Bool
    @Binding var dismissed: Bool
    @State private var shown = false
    @State private var everConnected = false
    @State private var stalled = false

    var body: some View {
        WarningBubble(text: everConnected || stalled ? "Can’t reach server" : "Connecting…",
                      symbol: everConnected || stalled ? "exclamationmark.triangle" : "arrow.triangle.2.circlepath",
                      visible: !connected && shown && !dismissed) { dismissed = true }
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
}

extension EnvironmentValues {
    @Entry var connectionDismissed: Binding<Bool> = .constant(false)
}
