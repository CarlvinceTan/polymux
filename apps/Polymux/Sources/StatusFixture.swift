#if DEBUG
import SwiftUI

struct StatusFixture: View {
    @State private var connected = true
    @State private var dismissed = false

    var body: some View {
        VStack {
            Text("Cached chat")
            Button(connected ? "Disconnect" : "Connect") { connected.toggle() }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .overlay(alignment: .bottom) {
            VStack(spacing: 0) {
                ConnectionStatus(connected: connected, dismissed: $dismissed)
                Text("Composer").padding()
            }
        }
    }
}
#endif
