#if os(macOS)
import AppKit
import SwiftUI

/// The app's icon as the Dock shows it, including the logo picked in Settings.
struct AppIcon: View {
    var body: some View {
        Image(nsImage: NSApp.applicationIconImage)
            .resizable()
            .interpolation(.high)
            .aspectRatio(contentMode: .fit)
    }
}

/// Polymux Server as System Settings lists it, which is what permissions are granted to.
struct ServerIcon: View {
    var body: some View {
        Image(nsImage: NSWorkspace.shared.icon(forFile: ServerHost.bundle.path))
            .resizable()
            .interpolation(.high)
            .aspectRatio(contentMode: .fit)
    }
}
#endif

#if os(iOS)
import SwiftUI

/// The app's logo, standing in for the icon on iPhone.
struct AppIcon: View {
    var body: some View {
        Image("Logo")
            .resizable()
            .interpolation(.high)
            .aspectRatio(contentMode: .fit)
    }
}
#endif
