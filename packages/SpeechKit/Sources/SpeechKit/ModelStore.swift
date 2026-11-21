import Foundation
import CoreML
import CryptoKit

/// Where decision models live on disk and how they become ready. Non-isolated so
/// the inference path can resolve a model without hopping to the main actor.
public enum ModelCache {
    /// Beside the client's other data, in the folder a test instance relocates.
    public static var root: URL {
        let environment = ProcessInfo.processInfo.environment
        if let path = environment["POLYMUX_CLIENT_DIR"] ?? environment["POLYMUX_DATA_DIR"], !path.isEmpty {
            return URL(fileURLWithPath: path, isDirectory: true).appending(path: "models", directoryHint: .isDirectory)
        }
        #if os(macOS)
        return FileManager.default.homeDirectoryForCurrentUser.appending(path: ".polymux/models", directoryHint: .isDirectory)
        #else
        return FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appending(path: "Polymux/models", directoryHint: .isDirectory)
        #endif
    }

    public static func directory(_ id: String) -> URL {
        root.appendingPathComponent(id, isDirectory: true)
    }

    /// The compiled model to load, or nil when not installed.
    public static func compiledModel(_ id: String) -> URL? {
        let compiled = directory(id).appendingPathComponent("\(id).mlmodelc")
        return FileManager.default.fileExists(atPath: compiled.path) ? compiled : nil
    }

    static func package(_ id: String) -> URL? {
        let url = directory(id).appendingPathComponent("\(id).mlpackage")
        return FileManager.default.fileExists(atPath: url.path) ? url : nil
    }

    public static func isReady(_ id: String) -> Bool { compiledModel(id) != nil }

    /// Compiles the downloaded .mlpackage into a cached .mlmodelc.
    @discardableResult
    static func compile(_ id: String) throws -> URL {
        let dir = directory(id)
        let dest = dir.appendingPathComponent("\(id).mlmodelc")
        if FileManager.default.fileExists(atPath: dest.path) { return dest }
        guard let package = package(id) else { throw ModelError.noPackage }
        let compiled = try MLModel.compileModel(at: package)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        if FileManager.default.fileExists(atPath: dest.path) {
            try FileManager.default.removeItem(at: dest)
        }
        try FileManager.default.moveItem(at: compiled, to: dest)
        try? FileManager.default.removeItem(at: package)
        return dest
    }
}

public enum ModelError: LocalizedError {
    case noSource
    case noPackage
    case server
    case checksum
    case archive

    public var errorDescription: String? {
        switch self {
        case .noSource: "No download source is configured for this model."
        case .noPackage: "The downloaded model is missing its package."
        case .server: "The model isn’t available to download right now."
        case .checksum: "The download failed its integrity check."
        case .archive: "The model archive could not be read."
        }
    }
}

/// Download state for one model, shown in settings.
public enum ModelState: Sendable, Equatable {
    case notDownloaded
    case downloading(Double)
    case ready
    case failed(String)
}

/// On-demand download manager. Models are never bundled; the user fetches the one
/// they select. Tracks per-model state and installs into the model cache.
@MainActor @Observable
public final class ModelStore {
    public static let shared = ModelStore()

    public private(set) var states: [String: ModelState] = [:]
    @ObservationIgnored private var delegates: [String: DownloadDelegate] = [:]

    public init() {
        for descriptor in DecisionModels.all {
            states[descriptor.id] = ModelCache.isReady(descriptor.id) ? .ready : .notDownloaded
        }
    }

    public func state(_ id: String) -> ModelState { states[id] ?? .notDownloaded }

    public func download(_ descriptor: DecisionModelDescriptor) {
        guard let url = descriptor.downloadURL else {
            states[descriptor.id] = .failed(ModelError.noSource.localizedDescription)
            return
        }
        if case .downloading = state(descriptor.id) { return }
        states[descriptor.id] = .downloading(0)
        let delegate = DownloadDelegate(descriptor: descriptor, store: self)
        delegates[descriptor.id] = delegate
        let session = URLSession(configuration: .default, delegate: delegate, delegateQueue: nil)
        delegate.task = session.downloadTask(with: url)
        delegate.task?.resume()
    }

    public func remove(_ id: String) {
        delegates[id]?.task?.cancel()
        delegates[id] = nil
        try? FileManager.default.removeItem(at: ModelCache.directory(id))
        states[id] = .notDownloaded
    }

    fileprivate func update(_ delegate: DownloadDelegate, _ state: ModelState) {
        let id = delegate.descriptor.id
        guard delegates[id] === delegate else {
            // Removed mid-install: drop what the install left behind.
            if delegates[id] == nil, states[id] == .notDownloaded {
                try? FileManager.default.removeItem(at: ModelCache.directory(id))
            }
            return
        }
        states[id] = state
        if case .downloading = state {} else { delegates[id] = nil }
    }

    /// Verifies, unpacks and compiles a downloaded archive. Slow, so it runs off the main actor.
    nonisolated static func install(_ descriptor: DecisionModelDescriptor, archive: URL) throws {
        guard try sha256(archive) == descriptor.sha256?.lowercased() else { throw ModelError.checksum }
        var dir = ModelCache.directory(descriptor.id)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        // It can be downloaded again, so it stays out of device backups.
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try? dir.setResourceValues(values)
        try extractTar(archive, into: dir)
        try ModelCache.compile(descriptor.id)
    }

    nonisolated static func sha256(_ url: URL) throws -> String {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        var hasher = SHA256()
        while let chunk = try handle.read(upToCount: 1 << 20), !chunk.isEmpty {
            hasher.update(data: chunk)
        }
        return hasher.finalize().map { String(format: "%02x", $0) }.joined()
    }

    /// Minimal ustar extractor: regular files and directories, no compression.
    nonisolated static func extractTar(_ url: URL, into dir: URL) throws {
        let data = try Data(contentsOf: url, options: .mappedIfSafe)
        let fm = FileManager.default
        var offset = 0
        func field(_ range: Range<Int>) -> String {
            let slice = data[range]
            let bytes = slice.prefix { $0 != 0 }
            return String(decoding: bytes, as: UTF8.self)
        }
        while offset + 512 <= data.count {
            let name = field(offset..<(offset + 100))
            if name.isEmpty { break }
            let sizeText = field((offset + 124)..<(offset + 136)).trimmingCharacters(in: .whitespaces)
            let size = Int(sizeText, radix: 8) ?? 0
            let type = data[offset + 156]
            let prefix = field((offset + 345)..<(offset + 500))
            let full = prefix.isEmpty ? name : prefix + "/" + name
            let target = dir.appendingPathComponent(full)
            let start = offset + 512
            if type == UInt8(ascii: "5") {
                try fm.createDirectory(at: target, withIntermediateDirectories: true)
            } else {
                try fm.createDirectory(at: target.deletingLastPathComponent(), withIntermediateDirectories: true)
                guard start + size <= data.count else { throw ModelError.archive }
                try data[start..<(start + size)].write(to: target)
            }
            offset = start + ((size + 511) / 512) * 512
        }
    }
}

private final class DownloadDelegate: NSObject, URLSessionDownloadDelegate, @unchecked Sendable {
    let descriptor: DecisionModelDescriptor
    weak var store: ModelStore?
    var task: URLSessionDownloadTask?

    init(descriptor: DecisionModelDescriptor, store: ModelStore) {
        self.descriptor = descriptor
        self.store = store
    }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask,
                    didWriteData bytesWritten: Int64, totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        let fraction = totalBytesExpectedToWrite > 0 ? Double(totalBytesWritten) / Double(totalBytesExpectedToWrite) : 0
        Task { @MainActor in store?.update(self, .downloading(fraction)) }
    }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {
        // The file at `location` only lasts until this returns, so install here on the session queue.
        let state: ModelState
        do {
            if let status = (downloadTask.response as? HTTPURLResponse)?.statusCode, !(200..<300).contains(status) {
                throw ModelError.server
            }
            try ModelStore.install(descriptor, archive: location)
            state = .ready
        } catch {
            state = .failed(error.localizedDescription)
        }
        Task { @MainActor in store?.update(self, state) }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        session.finishTasksAndInvalidate()
        guard let error, (error as? URLError)?.code != .cancelled else { return }
        Task { @MainActor in store?.update(self, .failed(error.localizedDescription)) }
    }
}
