import Foundation
import Observation

/// The server this Mac runs: launchd starts it from the copy inside the app, so it outlives the app.
@MainActor @Observable
final class ServerHost {
    static let shared = ServerHost()
    static let address = ProcessInfo.processInfo.environment["POLYMUX_LISTEN"].map { "http://\($0)" } ?? "http://127.0.0.1:7337"

    private(set) var running = false
    private(set) var changing = false
    private(set) var leftover = false
    private(set) var issue: String?
    private var prepared = false

    private init() { running = Self.job.map { FileManager.default.fileExists(atPath: $0.path) } ?? false }

    /// False on iPhone, and in a build that doesn't carry the server.
    var available: Bool { Self.helper != nil }

    /// Whether the job runs the app's own copy, rather than one installed another way.
    var managed: Bool {
        guard let job = Self.job, let data = try? Data(contentsOf: job),
              let plist = try? PropertyListSerialization.propertyList(from: data, format: nil) as? [String: Any],
              let arguments = plist["ProgramArguments"] as? [String] else { return false }
        return arguments.first?.contains(".app/Contents/Helpers/") == true
    }

    private static var helper: URL? {
        #if os(macOS)
        let url = Bundle.main.bundleURL.appending(path: "Contents/Helpers/polymux")
        guard FileManager.default.isExecutableFile(atPath: url.path) else { return nil }
        #if DEBUG
        // A development build only ever runs a job of its own.
        guard ProcessInfo.processInfo.environment["POLYMUX_HOST_LABEL"] != nil else { return nil }
        #endif
        return url
        #else
        return nil
        #endif
    }

    private static var job: URL? {
        #if os(macOS)
        let environment = ProcessInfo.processInfo.environment
        let folder = environment["POLYMUX_HOST_DIR"].map { URL(fileURLWithPath: $0, isDirectory: true) }
            ?? FileManager.default.homeDirectoryForCurrentUser.appending(path: "Library/LaunchAgents")
        return folder.appending(path: "\(environment["POLYMUX_HOST_LABEL"] ?? "com.flarehq.polymux.server").plist")
        #else
        return nil
        #endif
    }

    /// Runs once at launch. Only a brand new install starts a server by itself, so an update can
    /// never bring one back on a Mac that's a client on purpose.
    func prepare() async {
        guard !prepared, available else { return }
        prepared = true
        let defaults = UserDefaults.standard
        let fresh = defaults.object(forKey: "serverAddress") == nil && defaults.object(forKey: "serverDevices") == nil
        if fresh, !running {
            await start()
        } else if running {
            Task { _ = await self.run("update") }
        }
    }

    /// Catches a change from outside the app, such as a move retiring this Mac's server.
    func refresh() {
        guard !changing else { return }
        running = Self.job.map { FileManager.default.fileExists(atPath: $0.path) } ?? false
        leftover = Self.oldCopy
    }

    /// Whether a server that moved away left its copy on this Mac.
    private static var oldCopy: Bool {
        guard let data = dataFolder else { return false }
        let parent = data.deletingLastPathComponent()
        let names = (try? FileManager.default.contentsOfDirectory(atPath: parent.path)) ?? []
        return names.contains { $0.hasPrefix(data.lastPathComponent + ".moved-") }
    }

    private static var dataFolder: URL? {
        #if os(macOS)
        ProcessInfo.processInfo.environment["POLYMUX_DATA_DIR"].map { URL(fileURLWithPath: $0, isDirectory: true) }
            ?? FileManager.default.homeDirectoryForCurrentUser.appending(path: ".polymux/data")
        #else
        nil
        #endif
    }

    /// Deletes what a move left here, for good.
    func forgetCopy() async {
        guard !changing else { return }
        changing = true
        defer { changing = false }
        issue = await run("forget") ? nil : "Couldn’t delete the old copy."
        leftover = Self.oldCopy
    }

    func start() async {
        guard !changing else { return }
        changing = true
        defer { changing = false }
        issue = await run("install") ? nil : "Couldn’t start the server on this Mac."
        running = Self.job.map { FileManager.default.fileExists(atPath: $0.path) } ?? false
    }

    func stop() async {
        guard !changing else { return }
        changing = true
        defer { changing = false }
        issue = await run("remove") ? nil : "Couldn’t stop the server on this Mac."
        running = Self.job.map { FileManager.default.fileExists(atPath: $0.path) } ?? false
    }

    private func run(_ command: String) async -> Bool {
        #if os(macOS)
        guard let helper = Self.helper else { return false }
        return await Task.detached {
            let process = Process()
            process.executableURL = helper
            process.arguments = ["host", command]
            process.standardOutput = FileHandle.nullDevice
            process.standardError = FileHandle.nullDevice
            guard (try? process.run()) != nil else { return false }
            process.waitUntilExit()
            return process.terminationStatus == 0
        }.value
        #else
        return false
        #endif
    }
}
