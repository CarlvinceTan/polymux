#if os(macOS)
import Foundation
import Observation
import Sparkle

public enum AppUpdateChannel: String {
    case stable, nightly

    fileprivate var feed: String {
        switch self {
        case .stable: "https://polymux.com/appcast.xml"
        case .nightly: "https://github.com/CarlvinceTan/polymux/releases/download/dev-updates/appcast.xml"
        }
    }
}

/// Holds Sparkle's standard updater. Debug builds stay quiet unless explicitly enabled.
@Observable @MainActor public final class Updates: NSObject, SPUUpdaterDelegate {
    public static let shared = Updates()
    public private(set) var checking = false
    public private(set) var status: String?
    public private(set) var available = false
    @ObservationIgnored private var controller: SPUStandardUpdaterController!
    private static let channelKey = "PolymuxAppUpdateChannel"
    public var channel: AppUpdateChannel {
        didSet {
            guard channel != oldValue else { return }
            UserDefaults.standard.set(channel.rawValue, forKey: Self.channelKey)
            available = false
            status = nil
            controller.updater.resetUpdateCycleAfterShortDelay()
        }
    }

    private override init() {
        let stored = UserDefaults.standard.string(forKey: Self.channelKey).flatMap(AppUpdateChannel.init(rawValue:))
        let bundled = Bundle.main.object(forInfoDictionaryKey: "SUFeedURL") as? String
        channel = stored ?? (bundled == AppUpdateChannel.nightly.feed ? .nightly : .stable)
        super.init()
        #if DEBUG
        let start = ProcessInfo.processInfo.environment["POLYMUX_UPDATES"] == "1"
        #else
        let start = ProcessInfo.processInfo.environment["POLYMUX_UPDATES"] != "0"
        #endif
        // Opening the app counts as overdue, so Sparkle checks as it starts instead of waiting out its interval.
        if start { UserDefaults.standard.removeObject(forKey: "SULastCheckTime") }
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
        catch { settle("Couldn’t check for updates."); return }
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
        if checking { settle("No updates available.") }
    }

    public func updater(_ updater: SPUUpdater, didFinishUpdateCycleFor updateCheck: SPUUpdateCheck, error: Error?) {
        guard checking else { return }
        checking = false
        if let error {
            let failure = error as NSError
            settle(failure.domain == SUSparkleErrorDomain && failure.code == SUError.noUpdateError.rawValue
                ? "No updates available." : "Couldn’t check for updates.")
        }
    }

    /// Shows a check's outcome briefly; a found update stays until it's installed.
    private func settle(_ text: String) {
        status = text
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(4))
            guard let self, !self.checking, self.status == text else { return }
            self.status = nil
        }
    }

    public func feedURLString(for updater: SPUUpdater) -> String? {
        #if DEBUG
        if CommandLine.arguments.contains("--verify-updates") {
            return ProcessInfo.processInfo.environment["POLYMUX_VERIFY_UPDATE_FEED"] ?? "http://127.0.0.1:1/appcast.xml"
        }
        #endif
        return channel.feed
    }
}
#endif
