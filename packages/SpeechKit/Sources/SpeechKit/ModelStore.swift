import Foundation
import CoreML
import CryptoKit

/// Where decision models live on disk and how they become ready. Non-isolated so
/// the inference path can resolve a model without hopping to the main actor.
public enum ModelCache {
    /// Pre-seeded directory for local verification (no public host needed).
    static let overrideKey = "POLYMUX_DECISION_MODEL_DIR"

    public static var root: URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return base.appendingPathComponent("DecisionModels", isDirectory: true)
    }

    public static func directory(_ id: String) -> URL {
        root.appendingPathComponent(id, isDirectory: true)
    }

    /// The compiled model to load, or nil when not installed. A dev override directory
    /// is checked first so a pre-seeded model can be used without downloading.
    public static func compiledModel(_ id: String) -> URL? {
        let fm = FileManager.default
        var dirs = [URL]()
        if let override = ProcessInfo.processInfo.environment[overrideKey] {
            dirs.append(URL(fileURLWithPath: override, isDirectory: true))
        }
        dirs.append(directory(id))
        for dir in dirs {
            if dir.pathExtension == "mlmodelc", fm.fileExists(atPath: dir.path) { return dir }
            let compiled = dir.appendingPathComponent("\(id).mlmodelc")
            if fm.fileExists(atPath: compiled.path) { return compiled }
        }
        return nil
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
        return dest
    }
}

public enum ModelError: LocalizedError {
    case noSource
    case noPackage
    case checksum
    case archive

    public var errorDescription: String? {
        switch self {
        case .noSource: "No download source is configured for this model."
        case .noPackage: "The downloaded model is missing its package."
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

    public func refresh() {
        for descriptor in DecisionModels.all where state(descriptor.id) != .downloading(0) {
            if case .downloading = state(descriptor.id) { continue }
            states[descriptor.id] = ModelCache.isReady(descriptor.id) ? .ready : .notDownloaded
        }
    }

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

    fileprivate func update(_ id: String, _ state: ModelState) {
        states[id] = state
        if case .downloading = state {} else { delegates[id] = nil }
    }

    fileprivate func install(_ descriptor: DecisionModelDescriptor, archive: URL) {
        let id = descriptor.id
        do {
            if let expected = descriptor.sha256 {
                let digest = try Self.sha256(archive)
                guard digest == expected.lowercased() else { throw ModelError.checksum }
            }
            let dir = ModelCache.directory(id)
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try Self.extractTar(archive, into: dir)
            try ModelCache.compile(id)
            states[id] = .ready
        } catch {
            states[id] = .failed(error.localizedDescription)
        }
        delegates[id] = nil
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
        let data = try Data(contentsOf: url)
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
        let id = descriptor.id
        Task { @MainActor in store?.update(id, .downloading(fraction)) }
    }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {
        let id = descriptor.id
        let temp = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".tar")
        do {
            try FileManager.default.moveItem(at: location, to: temp)
        } catch {
            Task { @MainActor in store?.update(id, .failed(error.localizedDescription)) }
            return
        }
        Task { @MainActor in
            store?.install(descriptor, archive: temp)
            try? FileManager.default.removeItem(at: temp)
        }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        guard let error else { return }
        let id = descriptor.id
        Task { @MainActor in store?.update(id, .failed(error.localizedDescription)) }
    }
}