import SwiftUI

extension View {
    func shareMenu<Menu: View>(
        item: ShareItem?, @ViewBuilder menu: @escaping (ShareButton?) -> Menu
    ) -> some View {
        modifier(ShareMenu(item: item, menu: menu))
    }
}

enum ShareItem {
    case text(String)
    case file(URL)

    var value: Any {
        switch self {
        case .text(let text): text
        case .file(let file): file
        }
    }
}

struct ShareButton: View {
    let action: () -> Void

    var body: some View {
        Button("Share", action: action)
    }
}

private struct ShareMenu<Menu: View>: ViewModifier {
    let item: ShareItem?
    let menu: (ShareButton?) -> Menu
    #if os(macOS)
    @State private var sharing = ShareStore()
    #else
    @State private var sharing: ShareRequest?
    #endif

    func body(content: Content) -> some View {
        content
            #if os(macOS)
            .background { ShareAnchor(store: sharing).accessibilityHidden(true) }
            #endif
            .contextMenu {
                menu(item.map { item in
                    ShareButton {
                        #if os(macOS)
                        sharing.show([item.value])
                        #else
                        sharing = ShareRequest(item: item)
                        #endif
                    }
                })
            }
            #if os(iOS)
            .sheet(item: $sharing) { request in
                ShareSheet(item: request.item)
            }
            #endif
    }
}

#if os(macOS)
import AppKit

@MainActor
private final class ShareStore: NSObject, @MainActor NSSharingServicePickerDelegate {
    weak var anchor: NSView?
    private var picker: NSSharingServicePicker?

    func show(_ items: [Any]) {
        // Present after the context menu has released its event tracking.
        DispatchQueue.main.async { [weak self] in
            guard let self, let anchor, anchor.window != nil else { return }
            picker?.close()
            let next = NSSharingServicePicker(items: items)
            next.delegate = self
            picker = next
            next.show(relativeTo: anchor.bounds, of: anchor, preferredEdge: .minY)
        }
    }

    func sharingServicePicker(_ picker: NSSharingServicePicker, didChoose service: NSSharingService?) {
        if self.picker === picker { self.picker = nil }
    }
}

private struct ShareAnchor: NSViewRepresentable {
    let store: ShareStore

    func makeNSView(context: Context) -> ShareView {
        let view = ShareView()
        store.anchor = view
        return view
    }

    func updateNSView(_ view: ShareView, context: Context) { store.anchor = view }
}

private final class ShareView: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
}
#else
import UIKit

private struct ShareRequest: Identifiable {
    let id = UUID()
    let item: ShareItem
}

private struct ShareSheet: UIViewControllerRepresentable {
    let item: ShareItem

    func makeUIViewController(context: Context) -> UIActivityViewController {
        UIActivityViewController(activityItems: [item.value], applicationActivities: nil)
    }

    func updateUIViewController(_ controller: UIActivityViewController, context: Context) {}
}
#endif
