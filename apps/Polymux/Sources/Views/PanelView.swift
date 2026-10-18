import SwiftUI

enum PanelSurface {
    /// macOS window background values, so panels read the same on macOS and iOS.
    static func color(_ scheme: ColorScheme) -> Color {
        #if os(macOS)
        Color(nsColor: .windowBackgroundColor)
        #else
        scheme == .dark ? Color(white: 30.0 / 255) : Color(uiColor: .systemBackground)
        #endif
    }
}

extension View {
    /// Keeps a list or form row on the panel surface instead of a separate row colour.
    func panelRow() -> some View {
        modifier(PanelRow())
    }
}

private struct PanelRow: ViewModifier {
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        content.listRowBackground(PanelSurface.color(scheme))
    }
}

struct PanelView<Content: View, Actions: View>: View {
    let title: String
    let enabled: Bool
    let onClose: () -> Void
    let onBack: (() -> Void)?
    let header: AnyView?
    let content: Content
    let actions: Actions
    @Environment(\.panelContext) private var panel
    @Environment(\.colorScheme) private var scheme
    @State private var headerHeight: CGFloat = 76
    @State private var actionHeight: CGFloat = 0
    @State private var contentHeight: CGFloat?

    init(_ title: String, enabled: Bool = true, onClose: @escaping () -> Void, onBack: (() -> Void)? = nil,
         header: AnyView? = nil,
         @ViewBuilder content: () -> Content, @ViewBuilder actions: () -> Actions) {
        self.title = title
        self.enabled = enabled
        self.onClose = onClose
        self.onBack = onBack
        self.header = header
        self.content = content()
        self.actions = actions()
    }

    private var background: Color {
        PanelSurface.color(scheme)
    }

    private var fade: Color {
        #if os(macOS)
        background.opacity(0.65)
        #else
        background
        #endif
    }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .scrollContentBackground(.hidden)
            .scrollIndicators(.hidden)
            .safeAreaInset(edge: .top, spacing: 0) {
                Color.clear.frame(height: headerHeight + 8)
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                Color.clear.frame(height: actionHeight > 0 ? actionHeight + 40 : 16)
            }
            .onScrollGeometryChange(for: CGFloat.self) { geometry in
                ceil(geometry.contentSize.height + geometry.contentInsets.top + geometry.contentInsets.bottom)
            } action: { _, height in
                if height > 0 && height.isFinite { contentHeight = height }
            }
            .preference(key: PanelHeight.self, value: contentHeight)
            .background(background)
            .overlay(alignment: .top) {
                LinearGradient(colors: [fade, fade.opacity(0.85), .clear],
                               startPoint: .top, endPoint: .bottom)
                    .frame(height: headerHeight + 28)
                    .allowsHitTesting(false)
            }
            .overlay(alignment: .bottom) {
                if actionHeight > 0 {
                    LinearGradient(colors: [.clear, fade.opacity(0.85), fade],
                                   startPoint: .top, endPoint: .bottom)
                        .frame(height: actionHeight + 64)
                        .allowsHitTesting(false)
                }
            }
            .overlay(alignment: .top) {
                VStack(spacing: 12) {
                    ZStack {
                        Text(title)
                            .font(.headline)
                            .lineLimit(1)
                            .padding(.horizontal, 64)
                            .accessibilityAddTraits(.isHeader)
                        HStack {
                            if let back = onBack ?? panel?.back?.action {
                                navButton("chevron.left", label: "Back", action: back)
                            }
                            Spacer()
                            navButton("xmark", label: "Close \(title)", action: close)
                        }
                    }
                    .frame(minHeight: 44)
                    if let header { header }
                }
                .padding(16)
                .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { headerHeight = $0 }
            }
            .overlay(alignment: .bottomTrailing) {
                HStack(spacing: 10) { actions }
                    .buttonStyle(PanelButton())
                    .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { actionHeight = $0 }
                    .padding(16)
            }
            #if os(macOS)
            .allowsWindowActivationEvents()
            #endif
    }

    private func close() {
        if let panel { panel.end(onClose) }
        else { onClose() }
    }

    private func navButton(_ symbol: String, label: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 15))
                .foregroundStyle(enabled ? Color.primary : Color.secondary)
                .frame(width: 36, height: 36)
                .contentShape(Circle())
                .glassEffect(.regular.interactive(enabled), in: Circle())
        }
        .accessibilityLabel(label)
        .buttonStyle(.plain)
        .disabled(!enabled)
    }
}

private struct PanelButton: ButtonStyle {
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body)
            .foregroundStyle(enabled ? Color.primary : Color.secondary)
            .padding(.horizontal, 18)
            .padding(.vertical, 10)
            .contentShape(Capsule())
            .glassEffect(.regular.interactive(enabled), in: Capsule())
    }
}

extension PanelView where Actions == EmptyView {
    init(_ title: String, enabled: Bool = true, onClose: @escaping () -> Void, onBack: (() -> Void)? = nil,
         @ViewBuilder content: () -> Content) {
        self.init(title, enabled: enabled, onClose: onClose, onBack: onBack, content: content, actions: { EmptyView() })
    }
}
