import SwiftUI
import GraphKit
import PolymuxKit

private enum GraphControl {
    #if os(macOS)
    static let size: CGFloat = 30
    #else
    static let size: CGFloat = 44
    #endif
}

struct GraphToolButton: View {
    let symbol: String
    let label: String
    var enabled = true
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 14))
                .foregroundStyle(enabled ? Color.primary : Color.secondary.opacity(0.6))
                .frame(width: GraphControl.size, height: GraphControl.size)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!enabled)
        .help(label)
        .accessibilityLabel(label)
    }
}

/// The floating controls. Everything here changes the view, never the work it shows.
struct GraphToolbar: View {
    let model: GraphModel
    /// Off beside a chat, where Command-Z belongs to the composer.
    let shortcuts: Bool

    private var centre: CGPoint { CGPoint(x: model.viewport.width / 2, y: model.viewport.height / 2) }

    var body: some View {
        VStack(spacing: 8) {
            VStack(spacing: 0) {
                let full = model.mode == .full
                GraphToolButton(symbol: full ? "rectangle.compress.vertical" : "rectangle.expand.vertical",
                                label: full ? "Show compact view" : "Show full view") {
                    model.setMode(full ? .compact : .full)
                }
                undo
                redo
            }
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 14))
            VStack(spacing: 0) {
                GraphToolButton(symbol: "arrow.up.left.and.down.right.magnifyingglass", label: "Fit to view") { model.fit() }
                GraphToolButton(symbol: "wand.and.rays", label: "Auto layout", enabled: !model.scene.nodes.isEmpty) { model.autoLayout() }
                Divider().frame(width: 16).padding(.vertical, 2)
                GraphToolButton(symbol: "plus", label: "Zoom in", enabled: model.scale < GraphModel.maximumScale) {
                    model.zoom(by: 1.15, around: centre, snaps: true)
                }
                Text("\(Int((model.scale * 100).rounded()))%")
                    .font(.system(size: 10)).monospacedDigit().foregroundStyle(.secondary)
                    .accessibilityLabel("Zoom")
                    .accessibilityValue("\(Int((model.scale * 100).rounded())) percent")
                GraphToolButton(symbol: "minus", label: "Zoom out", enabled: model.scale > GraphModel.minimumScale) {
                    model.zoom(by: 1 / 1.15, around: centre, snaps: true)
                }
                Divider().frame(width: 16).padding(.vertical, 2)
                Menu {
                    Toggle("Grid", isOn: Binding(get: { model.place.grid }, set: { model.setGrid($0) }))
                    Toggle("Snap to grid", isOn: Binding(get: { model.place.snap }, set: { model.setSnap($0) }))
                } label: {
                    Image(systemName: "gearshape")
                        .font(.system(size: 14))
                        .frame(width: GraphControl.size, height: GraphControl.size)
                        .contentShape(Rectangle())
                }
                .menuStyle(.button)
                .menuIndicator(.hidden)
                .buttonStyle(.plain)
                .help("Graph settings")
                .accessibilityLabel("Graph settings")
            }
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 14))
        }
    }

    @ViewBuilder private var undo: some View {
        let button = GraphToolButton(symbol: "arrow.uturn.backward", label: "Undo", enabled: model.canUndo) { model.undo() }
        if shortcuts { button.keyboardShortcut("z", modifiers: .command) } else { button }
    }

    @ViewBuilder private var redo: some View {
        let button = GraphToolButton(symbol: "arrow.uturn.forward", label: "Redo", enabled: model.canRedo) { model.redo() }
        if shortcuts { button.keyboardShortcut("z", modifiers: [.command, .shift]) } else { button }
    }
}

/// Shown while more than one node is selected.
struct GraphSelectionBar: View {
    let model: GraphModel

    var body: some View {
        HStack(spacing: 10) {
            Text("\(model.selected.count) selected").font(.subheadline)
            Button("Clear") { model.clearSelection() }
                .keyboardShortcut(.cancelAction)
        }
        .padding(.horizontal, 12).frame(height: 36)
        .glassEffect(.regular, in: Capsule())
    }
}
