import SwiftUI

enum DropdownStyle {
    static let maximumHeight: CGFloat = 400
    /// Searchable lists keep one width while their matches change.
    static let listWidth: CGFloat = 320
    static var rowHeight: CGFloat {
        #if os(macOS)
        PanelStyle.contentInset + PanelStyle.rowInset
        #else
        PanelStyle.actionHeight + PanelStyle.headingInset
        #endif
    }
    /// iPhone's system menus are rounder than a Mac's.
    static var radius: CGFloat {
        #if os(macOS)
        PanelStyle.cardRadius
        #else
        24
        #endif
    }
}

private struct DropdownScope: EnvironmentKey {
    static var defaultValue: PanelAction { PanelAction() }
}

private struct DropdownState: EnvironmentKey {
    static let defaultValue = false
}

extension EnvironmentValues {
    var dropdownClose: PanelAction {
        get { self[DropdownScope.self] }
        set { self[DropdownScope.self] = newValue }
    }

    var dropdownOpen: Bool {
        get { self[DropdownState.self] }
        set { self[DropdownState.self] = newValue }
    }
}

extension View {
    /// The menu is as wide as its content, up to the window's width.
    func dropdown<Presented: View>(isPresented: Binding<Bool>, alignment: HorizontalAlignment = .trailing,
                                   @ViewBuilder content: @escaping () -> Presented) -> some View {
        modifier(Dropdown(isPresented: isPresented, alignment: alignment, presented: content))
    }

    func dropdownHost() -> some View { modifier(DropdownHost()) }
}

private struct DropdownEntry: Identifiable {
    let id: UUID
    let anchor: Anchor<CGRect>
    let alignment: HorizontalAlignment
    let view: AnyView
    let close: PanelAction
}

private struct DropdownEntries: PreferenceKey {
    static var defaultValue: [DropdownEntry] { [] }
    static func reduce(value: inout [DropdownEntry], nextValue: () -> [DropdownEntry]) {
        value.append(contentsOf: nextValue())
    }
}

private struct DropdownIDs: PreferenceKey {
    static var defaultValue: [UUID] { [] }
    static func reduce(value: inout [UUID], nextValue: () -> [UUID]) { value.append(contentsOf: nextValue()) }
}

private struct Dropdown<Presented: View>: ViewModifier {
    @Binding var isPresented: Bool
    let alignment: HorizontalAlignment
    @ViewBuilder let presented: () -> Presented
    @State private var id = UUID()

    func body(content: Content) -> some View {
        let close = PanelAction { isPresented = false }
        content
            .anchorPreference(key: DropdownEntries.self, value: .bounds) { anchor in
                guard isPresented else { return [] }
                let view = presented()
                    .environment(\.dropdownClose, close)
                    .environment(\.isEnabled, true)
                return [DropdownEntry(id: id, anchor: anchor, alignment: alignment, view: AnyView(view), close: close)]
            }
            .preference(key: DropdownIDs.self, value: isPresented ? [id] : [])
            .onDisappear { isPresented = false }
    }
}

private struct DropdownHost: ViewModifier {
    @State private var active = false
    #if os(macOS)
    @State private var focus = FocusMemory()
    #endif

    func body(content: Content) -> some View {
        content
            #if os(macOS)
            .background { FocusWindow(memory: focus).frame(width: 0, height: 0).accessibilityHidden(true) }
            #endif
            .environment(\.dropdownOpen, active)
            .overlayPreferenceValue(DropdownEntries.self) { entries in
                ZStack {
                    if let entry = entries.last {
                        DropdownLayer(entry: entry).id(entry.id).transition(.opacity)
                    }
                }
                .motion(value: entries.last?.id)
            }
            .onPreferenceChange(DropdownIDs.self) { ids in
                let shown = !ids.isEmpty
                #if os(macOS)
                if shown && !active { focus.save() }
                if !shown && active { focus.restore() }
                #endif
                active = shown
            }
            .transformPreference(DropdownEntries.self) { $0 = [] }
            .transformPreference(DropdownIDs.self) { $0 = [] }
    }
}

private struct DropdownLayer: View {
    let entry: DropdownEntry

    var body: some View {
        GeometryReader { proxy in
            ZStack {
                Button(action: entry.close.action) {
                    Color.clear.contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .focusable(false)
                .accessibilityLabel("Dismiss menu")
                DropdownLayout(anchor: proxy[entry.anchor], alignment: entry.alignment) {
                    entry.view
                        .glassEffect(.regular, in: .rect(cornerRadius: DropdownStyle.radius))
                        .shadow(color: .black.opacity(0.12), radius: 12, y: 4)
                }
            }
        }
        .environment(\.isEnabled, true)
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(.isModal)
        #if os(macOS)
        .onExitCommand { entry.close() }
        .allowsWindowActivationEvents()
        #endif
    }
}

private struct DropdownLayout: Layout {
    let anchor: CGRect
    let alignment: HorizontalAlignment

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        proposal.replacingUnspecifiedDimensions()
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        guard let view = subviews.first else { return }
        let inset = PanelStyle.rowInset
        let menuWidth = max(0, min(view.sizeThatFits(.unspecified).width.rounded(.up), bounds.width - inset * 2))
        let size = view.sizeThatFits(ProposedViewSize(width: menuWidth, height: nil))
        let rect = anchor.offsetBy(dx: bounds.minX, dy: bounds.minY)
        let edge = alignment == .leading ? rect.minX : rect.maxX - menuWidth
        let x = max(bounds.minX + inset, min(edge, bounds.maxX - menuWidth - inset))
        let below = bounds.maxY - rect.maxY - PanelStyle.cardInset - inset
        let above = rect.minY - bounds.minY - PanelStyle.cardInset - inset
        // Opens toward the room it needs, so the list is never squeezed against an edge.
        let upward = below < size.height && above > below
        let height = max(0, min(size.height, upward ? above : below))
        let y = upward ? rect.minY - PanelStyle.cardInset - height : rect.maxY + PanelStyle.cardInset
        view.place(at: CGPoint(x: x, y: y), anchor: .topLeading,
                   proposal: ProposedViewSize(width: menuWidth, height: height))
    }
}

struct DropdownOptions: View {
    @Binding var selection: String
    let options: [(String, String)]
    @Environment(\.dropdownClose) private var close
    @State private var highlighted: String?
    @State private var keyed: String?
    @FocusState private var focused: Bool

    var body: some View {
        ScrollViewReader { scroll in
            ScrollView {
                VStack(spacing: 0) {
                    ForEach(options, id: \.1) { label, value in
                        Button { choose(value) } label: {
                            HStack(spacing: PanelStyle.rowInset) {
                                Image(systemName: "checkmark").opacity(selection == value ? 1 : 0)
                                    .frame(width: PanelStyle.contentInset).accessibilityHidden(true)
                                Text(label).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading)
                            }
                            .padding(.horizontal, PanelStyle.rowInset)
                            .frame(minHeight: DropdownStyle.rowHeight)
                            .contentShape(.rect(cornerRadius: PanelStyle.rowRadius))
                            .background(.primary.opacity(highlighted == value ? PanelStyle.selectedTint : 0),
                                        in: .rect(cornerRadius: PanelStyle.rowRadius))
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(label)
                        .accessibilityValue(selection == value ? "Selected" : "")
                        .accessibilityAddTraits(selection == value ? .isSelected : [])
                        .pointerHover { inside in
                            if inside { highlighted = value } else if highlighted == value { highlighted = nil }
                        }
                        .id(value)
                    }
                }
                .padding(PanelStyle.cardInset)
            }
            .scrollIndicators(.hidden)
            .scrollBounceBehavior(.basedOnSize)
            // Only the keyboard scrolls: a row under the pointer is already in view.
            .onChange(of: keyed) { _, value in
                if let value { scroll.scrollTo(value) }
            }
            .onAppear { scroll.scrollTo(selection, anchor: .center) }
        }
        .frame(idealHeight: height, maxHeight: height)
        #if os(macOS)
        .focusable().focused($focused).focusEffectDisabled()
        .task { focused = true }
        .onKeyPress(.downArrow) { move(1); return .handled }
        .onKeyPress(.upArrow) { move(-1); return .handled }
        .onKeyPress(.return) { choose(highlighted ?? selection); return .handled }
        .onExitCommand { close() }
        #endif
    }

    private var height: CGFloat {
        min(DropdownStyle.maximumHeight, CGFloat(options.count) * DropdownStyle.rowHeight + PanelStyle.cardInset * 2)
    }

    private func choose(_ value: String) {
        close()
        selection = value
    }

    private func move(_ offset: Int) {
        guard !options.isEmpty else { return }
        let index = options.firstIndex { $0.1 == (highlighted ?? selection) } ?? 0
        highlighted = options[max(0, min(options.count - 1, index + offset))].1
        keyed = highlighted
    }
}
