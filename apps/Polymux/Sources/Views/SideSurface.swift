#if os(macOS)
import AppKit
import SwiftUI

struct SideSurface: NSViewRepresentable {
    func makeNSView(context: Context) -> NSVisualEffectView {
        let view = NSVisualEffectView()
        view.material = .sidebar
        view.blendingMode = .behindWindow
        return view
    }

    func updateNSView(_ view: NSVisualEffectView, context: Context) {}
}

struct SideDivider: View {
    @Binding var width: Double
    let collapse: () -> Void
    @State private var origin: Double?
    @State private var collapsed = false

    var body: some View {
        Color.clear
            .frame(width: 8)
            .contentShape(Rectangle())
            .overlay(alignment: .leading) {
                Divider().allowsHitTesting(false)
            }
            .pointerStyle(.columnResize)
            .gesture(DragGesture(minimumDistance: 0, coordinateSpace: .global)
                .onChanged { value in
                    guard !collapsed else { return }
                    if origin == nil { origin = width }
                    let proposed = (origin ?? width) - value.translation.width
                    if proposed < Double(PaneWidth.profileMin) / 2 {
                        collapsed = true
                        collapse()
                    } else {
                        resize(proposed)
                    }
                }
                .onEnded { _ in origin = nil; collapsed = false })
            .accessibilityRepresentation {
                Slider(value: $width, in: Double(PaneWidth.profileMin)...Double(PaneWidth.profile), step: 20) {
                    Text("Resize side panel")
                }
                .accessibilityValue("\(Int(width)) points")
                .accessibilityAction(named: "Collapse side panel", collapse)
            }
    }

    private func resize(_ value: Double) {
        width = min(Double(PaneWidth.profile), max(Double(PaneWidth.profileMin), value))
    }
}
#endif
