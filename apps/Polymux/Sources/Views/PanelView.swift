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

    static func card(_ scheme: ColorScheme) -> Color {
        #if os(macOS)
        Color(nsColor: .controlBackgroundColor)
        #else
        Color(uiColor: .secondarySystemGroupedBackground)
        #endif
    }
}

enum PanelStyle {
    static let sectionGap: CGFloat = 22
    static let contentInset: CGFloat = 16
    static let formInset: CGFloat = 20
    static let headingGap: CGFloat = 10
    static let headingInset: CGFloat = 12
    static let cardInset: CGFloat = 4
    static let cardRadius: CGFloat = 16
    static let rowInset: CGFloat = 8
    static let rowPadding: CGFloat = 6
    static let rowRadius: CGFloat = 8
    static let actionHeight: CGFloat = 32
    /// Row fills: selection reads as a slightly stronger hover everywhere.
    static let hoverTint = 0.06
    static let selectedTint = 0.11
}

struct PanelSection<Content: View>: View {
    let title: String?
    let count: Int?
    let fills: Bool
    /// A button at the heading's trailing edge, where a count sits.
    let accessory: PanelAccessory?
    @ViewBuilder let content: () -> Content
    @Environment(\.colorScheme) private var scheme

    init(_ title: String? = nil, count: Int? = nil, fills: Bool = false, accessory: PanelAccessory? = nil,
         @ViewBuilder content: @escaping () -> Content) {
        self.title = title
        self.count = count
        self.fills = fills
        self.accessory = accessory
        self.content = content
    }

    var body: some View {
        VStack(alignment: .leading, spacing: PanelStyle.headingGap) {
            if let title {
                HStack {
                    Text(title).accessibilityAddTraits(.isHeader)
                    Spacer(minLength: 8)
                    if let count { Text(count, format: .number).monospacedDigit() }
                    if let accessory {
                        Button(action: accessory.action) {
                            Image(systemName: accessory.symbol)
                                // Reaches the tap minimum without making the heading taller.
                                .contentShape(.rect.inset(by: -14))
                        }
                        .buttonStyle(.plain)
                        .help(accessory.label)
                        .accessibilityLabel(accessory.label)
                    }
                }
                .font(.body).foregroundStyle(.muted)
                .padding(.horizontal, PanelStyle.headingInset)
            }
            VStack(alignment: .leading, spacing: 0, content: content)
                .frame(maxWidth: .infinity, maxHeight: fills ? .infinity : nil, alignment: .topLeading)
                .padding(PanelStyle.cardInset)
                .background(PanelSurface.card(scheme), in: .rect(cornerRadius: PanelStyle.cardRadius))
                .overlay {
                    RoundedRectangle(cornerRadius: PanelStyle.cardRadius)
                        .strokeBorder(.primary.opacity(0.08), lineWidth: 0.5)
                        .allowsHitTesting(false)
                }
        }
    }
}

struct SectionButton: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body).foregroundStyle(.muted)
            .frame(maxWidth: .infinity, minHeight: PanelStyle.actionHeight)
            .contentShape(.rect(cornerRadius: PanelStyle.rowRadius))
            .background(.primary.opacity(configuration.isPressed ? 0.08 : 0), in: .rect(cornerRadius: PanelStyle.rowRadius))
    }
}

extension View {
    func sectionRow() -> some View {
        frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, PanelStyle.rowInset)
            .padding(.vertical, PanelStyle.rowPadding)
    }

    /// Reaches the 44-point touch target on iPhone without changing how a small control looks.
    func tapArea(_ margin: CGFloat) -> some View {
        #if os(iOS)
        contentShape(Rectangle().inset(by: -margin))
        #else
        self
        #endif
    }

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
    /// An extra header button beside close, such as search.
    let accessory: PanelAccessory?
    let scrollUnder: Bool
    let width: CGFloat?
    let height: CGFloat?
    /// Overrides the close button's label, for titles that change with the content.
    let closeLabel: String
    let content: Content
    let actions: Actions
    @Environment(\.panelContext) private var panel
    @Environment(\.colorScheme) private var scheme
    @State private var headerHeight: CGFloat = 76
    @State private var actionHeight: CGFloat = 0
    @State private var contentHeight: CGFloat?

    init(_ title: String, enabled: Bool = true, onClose: @escaping () -> Void, onBack: (() -> Void)? = nil,
         header: AnyView? = nil, accessory: PanelAccessory? = nil, scrollUnder: Bool = true,
         width: CGFloat? = nil, height: CGFloat? = nil, closeLabel: String? = nil,
         @ViewBuilder content: () -> Content, @ViewBuilder actions: () -> Actions) {
        self.title = title
        self.enabled = enabled
        self.onClose = onClose
        self.onBack = onBack
        self.header = header
        self.accessory = accessory
        self.scrollUnder = scrollUnder
        self.width = width
        self.height = height
        self.closeLabel = closeLabel ?? "Close \(title)"
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
        VStack(spacing: 0) {
            if scrollUnder {
                scrollContent
            } else {
                panelHeader.fixedSize(horizontal: false, vertical: true)
                scrollContent
                    .clipped()
                    .contentShape(Rectangle())
            }
        }
            .preference(key: PanelHeight.self,
                        value: height ?? contentHeight.map { $0 + (scrollUnder ? 0 : headerHeight) })
            .preference(key: PanelWidth.self, value: width)
            .background(background)
            .overlay(alignment: .top) {
                if scrollUnder {
                    LinearGradient(colors: [fade, fade.opacity(0.85), .clear],
                                   startPoint: .top, endPoint: .bottom)
                        .frame(height: headerHeight + 28)
                        .allowsHitTesting(false)
                }
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
                if scrollUnder { panelHeader }
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

    private var scrollContent: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .scrollContentBackground(.hidden)
            .scrollIndicators(.hidden)
            .safeAreaInset(edge: .top, spacing: 0) {
                Color.clear.frame(height: scrollUnder ? headerHeight + 8 : 0)
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                Color.clear.frame(height: actionHeight > 0 ? actionHeight + 40 : 16)
            }
            .onScrollGeometryChange(for: CGFloat.self) { geometry in
                ceil(geometry.contentSize.height + geometry.contentInsets.top + geometry.contentInsets.bottom)
            } action: { _, height in
                if height > 0 && height.isFinite { contentHeight = height }
            }
    }

    private var panelHeader: some View {
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
                    if let accessory {
                        navButton(accessory.symbol, label: accessory.label, action: accessory.action)
                    }
                    navButton("xmark", label: closeLabel, action: close)
                }
            }
            .frame(minHeight: 44)
            if let header { header }
        }
        .padding(16)
        .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { headerHeight = $0 }
    }

    private func close() {
        if let panel { panel.end(onClose) }
        else { onClose() }
    }

    private func navButton(_ symbol: String, label: String, action: @escaping () -> Void) -> some View {
        PanelNavButton(symbol: symbol, label: label, enabled: enabled, action: action)
    }
}

/// The round glass button panels use for back, close and similar header actions.
struct PanelNavButton: View {
    let symbol: String
    let label: String
    var enabled = true
    let action: () -> Void

    var body: some View {
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

struct PanelAccessory {
    let symbol: String
    let label: String
    let action: () -> Void
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
         header: AnyView? = nil, accessory: PanelAccessory? = nil, scrollUnder: Bool = true,
         width: CGFloat? = nil, height: CGFloat? = nil, closeLabel: String? = nil, @ViewBuilder content: () -> Content) {
        self.init(title, enabled: enabled, onClose: onClose, onBack: onBack, header: header, accessory: accessory,
                  scrollUnder: scrollUnder, width: width, height: height, closeLabel: closeLabel,
                  content: content, actions: { EmptyView() })
    }
}
