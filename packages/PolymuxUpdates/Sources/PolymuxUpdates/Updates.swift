#if os(macOS)
import Foundation
import Observation
import Sparkle

/// Holds Sparkle's standard updater. Debug builds stay quiet unless explicitly enabled.
@Observable @MainActor public final class Updates: NSObject, SPUUpdaterDelegate {
    public static let shared = Updates()
    public private(set) var checking = false
    public private(set) var status: String?
    public private(set) var available = false
    @ObservationIgnored private var controller: SPUStandardUpdaterController!

    private override init() {
        super.init()
        #if DEBUG
        let start = ProcessInfo.processInfo.environment["POLYMUX_UPDATES"] == "1"
        #else
        let start = ProcessInfo.processInfo.environment["POLYMUX_UPDATES"] != "0"
        #endif
        controller = SPUStandardUpdaterController(startingUpdater: start, updaterDelegate: self, userDriverDelegate: nil)
    }

    /// Sparkle's scheduled-check preference; safe to read and write before the updater starts.
    public var automaticallyChecks: Bool {
        get { controller.updater.automaticallyChecksForUpdates }
        set { controller.updater.automaticallyChecksForUpdates = newValue }
    }

    public func checkForUpdates() {
        #if DEBUG
        try? controller.updater.start()
        #endif
        controller.checkForUpdates(nil)
    }

    public func check() {
        guard !checking else { return }
        do { try controller.updater.start() }
        catch { status = "Couldn’t check for updates."; return }
        guard !controller.updater.sessionInProgress else {
            status = "An update check is already in progress."
            return
        }
        checking = true
        available = false
        status = "Checking for updates…"
        controller.updater.checkForUpdateInformation()
    }

    public func updater(_ updater: SPUUpdater, didFindValidUpdate item: SUAppcastItem) {
        available = true
        if checking { status = "An update is available." }
    }

    public func updaterDidNotFindUpdate(_ updater: SPUUpdater) {
        available = false
        if checking { status = "No updates available." }
    }

    public func updater(_ updater: SPUUpdater, didFinishUpdateCycleFor updateCheck: SPUUpdateCheck, error: Error?) {
        guard checking else { return }
        checking = false
        if let error {
            let failure = error as NSError
            status = failure.domain == SUSparkleErrorDomain && failure.code == SUError.noUpdateError.rawValue
                ? "No updates available." : "Couldn’t check for updates."
        }
    }

    #if DEBUG
    public func feedURLString(for updater: SPUUpdater) -> String? {
        guard CommandLine.arguments.contains("--verify-updates") else { return nil }
        return ProcessInfo.processInfo.environment["POLYMUX_VERIFY_UPDATE_FEED"] ?? "http://127.0.0.1:1/appcast.xml"
    }
    #endif
}
#endif
