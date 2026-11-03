import SwiftUI

/// The app icon picked in Settings; Dark is the bundle's own.
enum AppLogo {
    static let key = "appLogo"
    static let light = "AppIconLight"

    #if os(macOS)
    /// Shows the logo in the Dock, and stamps it on the app for Finder and Spotlight.
    @MainActor static func apply(_ logo: String? = UserDefaults.standard.string(forKey: key)) {
        let image = logo == "light" ? NSImage(named: light) : nil
        NSApp.applicationIconImage = image
        let path = Bundle.main.bundlePath
        // A stamped bundle can't be signed again, which would break Xcode's next build.
        guard !path.contains("/Build/Products/") else { return }
        // An update replaces the app and drops the stamp.
        guard FileManager.default.fileExists(atPath: path + "/Icon\r") != (image != nil) else { return }
        NSWorkspace.shared.setIcon(image, forFile: path)
        NSWorkspace.shared.noteFileSystemChanged(path)
    }
    #endif
}

struct LogoPicker: View {
    #if os(macOS)
    @AppStorage(AppLogo.key) private var logo = "dark"
    #else
    @State private var logo = UIApplication.shared.alternateIconName == nil ? "dark" : "light"
    @State private var issue: String?
    #endif

    var body: some View {
        SettingsPicker("App logo", selection: Binding(get: { logo }, set: change),
                       options: [("Dark", "dark"), ("Light", "light")])
        #if !os(macOS)
        if let issue { Text(issue).font(.footnote).foregroundStyle(.muted) }
        #endif
    }

    private func change(_ value: String) {
        guard value != logo else { return }
        #if os(macOS)
        logo = value
        AppLogo.apply(value)
        #else
        let previous = logo
        logo = value
        Task {
            do {
                try await UIApplication.shared.setAlternateIconName(value == "light" ? AppLogo.light : nil)
                issue = nil
            } catch {
                logo = previous
                issue = "Couldn't change the app logo."
            }
        }
        #endif
    }
}
