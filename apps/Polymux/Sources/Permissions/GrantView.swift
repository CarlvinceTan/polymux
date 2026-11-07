#if os(macOS)
import AppKit
import SwiftUI

/// Manage Permissions: what bots and alerts need, each with its own way to grant it.
struct GrantView: View {
    let close: () -> Void
    private let store = Permissions.shared
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(spacing: 0) {
            AppIcon()
                .frame(width: 96, height: 96)
                .onDrag { NSItemProvider(contentsOf: ServerHost.bundle) ?? NSItemProvider() }
                .help(store.opened.map { "Drag this icon into the list in System Settings to grant \($0.title)." } ?? "")
                .accessibilityLabel("Polymux Server")
                .padding(.top, 36)
            Text("Manage Permissions")
                .font(.largeTitle.bold())
                .accessibilityAddTraits(.isHeader)
                .padding(.top, 10)
            VStack(spacing: 0) {
                ForEach(Array(store.all.enumerated()), id: \.element) { index, permission in
                    if index > 0 { Divider().padding(.horizontal, 12) }
                    row(permission)
                }
            }
            .background(PanelSurface.card(scheme), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(.separator))
            .padding(.top, 24)
            .padding(.horizontal, 20)
        }
        .padding(.bottom, 20)
        .frame(width: 500)
        .overlay(alignment: .topTrailing) {
            PanelNavButton(symbol: "xmark", label: "Close", action: close)
                .padding(16)
        }
        .background(PanelSurface.color(scheme), in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .appAppearance(store.appearance)
    }

    private func row(_ permission: Permission) -> some View {
        HStack(spacing: 13) {
            Image(systemName: permission.symbol)
                .font(.system(size: 15, weight: .light))
                .foregroundStyle(.secondary)
                .frame(width: 34, height: 34)
                .background(.quinary, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            VStack(alignment: .leading, spacing: 4) {
                Button { open(permission) } label: {
                    HStack(spacing: 4) {
                        Text(permission.title).font(.headline)
                        Image(systemName: "arrow.up.forward.circle")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                .buttonStyle(.plain)
                .help("Open \(permission.title) in System Settings")
                Text(permission.detail)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(2, reservesSpace: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if store.granted.contains(permission) {
                Label("Granted", systemImage: "checkmark.circle.fill")
                    .foregroundStyle(.secondary)
            } else {
                Button("Grant…") { store.grant(permission) }
                    .buttonStyle(.bordered)
                    .buttonBorderShape(.capsule)
                    .accessibilityLabel("Grant \(permission.title)")
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 14)
    }

    private func open(_ permission: Permission) {
        if permission.dragged { store.grant(permission) } else { NSWorkspace.shared.open(permission.pane) }
    }
}

/// The app's icon as the Dock shows it, including the logo picked in Settings.
struct AppIcon: View {
    var body: some View {
        Image(nsImage: NSApp.applicationIconImage)
            .resizable()
            .interpolation(.high)
            .aspectRatio(contentMode: .fit)
    }
}
#endif
