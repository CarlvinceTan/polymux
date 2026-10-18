import SwiftUI

/// The window background cards and panes sit on.
enum AppBackground {
    static func color(_ scheme: ColorScheme) -> Color {
        #if os(macOS)
        scheme == .dark ? Color(white: 30.0 / 255) : Color(nsColor: .windowBackgroundColor)
        #else
        Color(uiColor: .systemBackground)
        #endif
    }
}

struct AppearanceView: View {
    let mode: String

    var body: some View {
        NativeAppearance(mode: mode)
            .accessibilityHidden(true)
    }
}

extension View {
    func appAppearance(_ mode: String) -> some View {
        modifier(AppAppearance(mode: mode))
    }

    func neutralBackground() -> some View {
        #if os(macOS)
        modifier(NeutralBackground())
        #else
        self
        #endif
    }
}

private struct AppAppearance: ViewModifier {
    let mode: String
    #if os(macOS)
    @State private var system = SystemAppearance()

    private var scheme: ColorScheme {
        switch mode {
        case "light": .light
        case "dark": .dark
        default: system.scheme
        }
    }

    func body(content: Content) -> some View {
        content
            .background { AppearanceView(mode: scheme == .dark ? "dark" : "light").frame(width: 0, height: 0) }
            .environment(\.colorScheme, scheme)
    }
    #else
    func body(content: Content) -> some View {
        content
            .background { AppearanceView(mode: mode).frame(width: 0, height: 0) }
            .preferredColorScheme(mode == "system" ? nil : mode == "dark" ? .dark : .light)
    }
    #endif
}

#if os(macOS)
import AppKit
import Observation

@MainActor
@Observable
private final class SystemAppearance {
    private(set) var scheme: ColorScheme
    private var observation: NSKeyValueObservation?

    init() {
        scheme = Self.scheme(NSApp.effectiveAppearance)
        observation = NSApp.observe(\.effectiveAppearance, options: [.new]) { [weak self] app, _ in
            MainActor.assumeIsolated {
                self?.scheme = Self.scheme(app.effectiveAppearance)
            }
        }
    }

    private static func scheme(_ appearance: NSAppearance) -> ColorScheme {
        appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua ? .dark : .light
    }
}

private struct NeutralBackground: ViewModifier {
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        let background = AnyShapeStyle(AppBackground.color(scheme))
        content
            .background(background)
            .containerBackground(background, for: .window)
            .environment(\.backgroundStyle, background)
    }
}

private struct NativeAppearance: NSViewRepresentable {
    let mode: String

    func makeNSView(context: Context) -> AppearanceAnchor {
        let view = AppearanceAnchor()
        view.mode = mode
        return view
    }

    func updateNSView(_ view: AppearanceAnchor, context: Context) {
        view.mode = mode
        view.refresh()
    }

    static func dismantleNSView(_ view: AppearanceAnchor, coordinator: ()) {
        view.stop()
    }
}

private final class AppearanceAnchor: NSView {
    var mode = "system"
    private weak var root: NSWindow?
    private var pending = false

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        stop()
        guard let window else { return }
        root = parent(of: window)
        NotificationCenter.default.addObserver(self, selector: #selector(sheetWillBegin),
                                               name: NSWindow.willBeginSheetNotification, object: nil)
        refresh()
    }

    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    func refresh() {
        apply()
        guard !pending else { return }
        pending = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.pending = false
            self.apply()
        }
    }

    func stop() {
        NotificationCenter.default.removeObserver(self, name: NSWindow.willBeginSheetNotification, object: nil)
        root = nil
        pending = false
    }

    @objc private func sheetWillBegin(_ notification: Notification) {
        guard let window = notification.object as? NSWindow, let root,
              parent(of: window) === root else { return }
        refresh()
    }

    private func parent(of window: NSWindow) -> NSWindow {
        var next = window
        while let parent = next.sheetParent { next = parent }
        return next
    }

    private func apply() {
        guard let root else { return }
        let appearance: NSAppearance?
        switch mode {
        case "light": appearance = NSAppearance(named: .aqua)
        case "dark": appearance = NSAppearance(named: .darkAqua)
        default: appearance = nil
        }
        apply(appearance, to: root)
    }

    private func apply(_ appearance: NSAppearance?, to window: NSWindow) {
        if window.appearance?.name != appearance?.name { window.appearance = appearance }
        for sheet in window.sheets { apply(appearance, to: sheet) }
    }
}
#else
import UIKit

private struct NativeAppearance: UIViewRepresentable {
    let mode: String

    func makeUIView(context: Context) -> AppearanceAnchor {
        let view = AppearanceAnchor()
        view.mode = mode
        view.isUserInteractionEnabled = false
        return view
    }

    func updateUIView(_ view: AppearanceAnchor, context: Context) {
        view.mode = mode
        view.apply()
    }
}

private final class AppearanceAnchor: UIView {
    var mode = "system"

    override func didMoveToWindow() {
        super.didMoveToWindow()
        apply()
    }

    func apply() {
        guard let window else { return }
        let style: UIUserInterfaceStyle
        switch mode {
        case "light": style = .light
        case "dark": style = .dark
        default: style = .unspecified
        }
        if window.overrideUserInterfaceStyle != style { window.overrideUserInterfaceStyle = style }
    }
}
#endif
