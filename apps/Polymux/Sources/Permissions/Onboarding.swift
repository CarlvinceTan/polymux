#if os(macOS)
import SwiftUI

/// The first launch's walkthrough: a welcome, then the permissions bots need.
struct Onboarding: View {
    /// UI checks open it at launch with --verify-onboarding.
    static var verifying: Bool {
        #if DEBUG
        CommandLine.arguments.contains("--verify-onboarding")
        #else
        false
        #endif
    }

    let finish: () -> Void
    @State private var step = 0
    private let store = Permissions.shared

    var body: some View {
        if step == 0 {
            PanelView("Welcome", onClose: finish, width: 560) {
                page(title: "Get Started with Polymux", detail: "Take a moment to set up the permissions bots need.") {
                    AppIcon().frame(width: 160, height: 160)
                }
            } actions: {
                Button("Skip", action: finish)
                Button("Continue") { step = 1 }
            }
        } else {
            PanelView("Permissions", onClose: finish, onBack: { step = 0 }, width: 560) {
                page(title: "System Permissions", badge: store.summary,
                     detail: "Grant permissions so bots can work with your files and screen.") {
                    Image(systemName: "hand.raised.fill")
                        .font(.system(size: 30))
                        .foregroundStyle(.white)
                        .frame(width: 56, height: 56)
                        .background(.blue, in: RoundedRectangle(cornerRadius: 13, style: .continuous))
                        .accessibilityHidden(true)
                } action: {
                    Button("Grant…") { GrantWindow.shared.show() }
                        .buttonStyle(.bordered)
                        .buttonBorderShape(.capsule)
                        .accessibilityLabel("Grant permissions")
                        .padding(.top, 8)
                }
            } actions: {
                Button("Done", action: finish)
            }
            .task { await store.refresh() }
        }
    }

    private func page<Art: View, Action: View>(title: String, badge: String? = nil, detail: String,
                                               @ViewBuilder art: () -> Art,
                                               @ViewBuilder action: () -> Action = { EmptyView() }) -> some View {
        ScrollView {
            VStack(spacing: 12) {
                art().padding(.bottom, 8)
                Text(title)
                    .font(.largeTitle.bold())
                    .multilineTextAlignment(.center)
                if let badge {
                    Text(badge)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 1)
                        .overlay(Capsule().strokeBorder(.tertiary))
                }
                Text(detail)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                action()
            }
            .frame(maxWidth: .infinity)
            .padding(.horizontal, 40)
            .padding(.vertical, 24)
        }
    }
}
#endif
