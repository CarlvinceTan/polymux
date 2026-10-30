import SwiftUI

enum DropdownStyle {
    static let maximumHeight: CGFloat = 400
    static var rowHeight: CGFloat {
        #if os(macOS)
        PanelStyle.contentInset + PanelStyle.rowInset
        #else
        PanelStyle.actionHeight + PanelStyle.headingInset
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
    func dropdown<Presented: View>(isPresented: Binding<Bool>, width: CGFloat = 200,
                                   @ViewBuilder content: @escaping () -> Presented) -> some View {
        modifier(Dropdown(isPresented: isPresented, width: width, presented: content))
    }

    func dropdownHost() -> some View { modifier(DropdownHost()) }
}

private struct DropdownEntry: Identifiable {
    let id: UUID
    let anchor: Anchor<CGRect>
    let width: CGFloat
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
    let width: CGFloat
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
                return [DropdownEntry(id: id, anchor: anchor, width: width, view: AnyView(view), close: close)]
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
                if let entry = entries.last { DropdownLayer(entry: entry).id(entry.id) }
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
                DropdownLayout(anchor: proxy[entry.anchor], width: entry.width) {
                    entry.view
                        .glassEffect(.regular, in: .rect(cornerRadius: PanelStyle.cardRadius))
                        .shadow(color: .black.opacity(0.25), radius: PanelStyle.contentInset, y: PanelStyle.rowInset)
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
    let width: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        proposal.replacingUnspecifiedDimensions()
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        guard let view = subviews.first else { return }
        let inset = PanelStyle.rowInset
        let width = max(0, min(width, bounds.width - inset * 2))
        let size = view.sizeThatFits(ProposedViewSize(width: width, height: nil))
        let rect = anchor.offsetBy(dx: bounds.minX, dy: bounds.minY)
        let x = max(bounds.minX + inset, min(rect.maxX - width, bounds.maxX - width - inset))
        let y = rect.maxY + PanelStyle.cardInset
        let height = max(0, min(size.height, bounds.maxY - y - inset))
        view.place(at: CGPoint(x: x, y: y), anchor: .topLeading,
                   proposal: ProposedViewSize(width: width, height: height))
    }
}

struct DropdownOptions: View {
    @Binding var selection: String
    let options: [(String, String)]
    @Environment(\.dropdownClose) private var close
    @State private var highlighted: String?
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
                            .background(.primary.opacity(highlighted == value ? 0.1 : 0),
                                        in: .rect(cornerRadius: PanelStyle.rowRadius))
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(label)
                        .accessibilityValue(selection == value ? "Selected" : "")
                        .accessibilityAddTraits(selection == value ? .isSelected : [])
                        .onHover { highlighted = $0 ? value : nil }
                        .id(value)
                    }
                }
                .padding(PanelStyle.cardInset)
            }
            .scrollIndicators(.hidden)
            .scrollBounceBehavior(.basedOnSize)
            .onChange(of: highlighted) { _, value in
                if let value { scroll.scrollTo(value) }
            }
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
    }
}
