import Foundation

public enum DroppedFile: Equatable, Sendable {
    case file(URL)
    case failed
}

#if os(macOS)
import AppKit
import ImageIO
import UniformTypeIdentifiers

extension DroppedFile {
    public static var types: [String] {
        [UTType.fileURL.identifier, UTType.image.identifier] + NSFilePromiseReceiver.readableDraggedTypes
    }

    // Prepare before a drag so accepting a promise never needs disk I/O on the main thread.
    public static func prepare() async throws -> URL {
        try await Task.detached(priority: .userInitiated) { try directory() }.value
    }

    // SwiftUI's item providers cannot always load a browser's promised image.
    @MainActor
    @discardableResult
    public static func receive(from pasteboard: NSPasteboard, into folder: URL,
                               received: @escaping @MainActor @Sendable (Self) -> Void) -> Bool {
        let objects = read(pasteboard)
        guard !objects.isEmpty else { return false }
        let queue = OperationQueue()
        queue.qualityOfService = .userInitiated
        for object in objects {
            if let receiver = object as? NSFilePromiseReceiver {
                // AppKit requires this request before the synchronous drop callback returns.
                receiver.receivePromisedFiles(atDestination: folder, options: [:], operationQueue: queue,
                                              reader: reader(received))
            } else if let url = object as? URL {
                Task {
                    let result = await Task.detached(priority: .userInitiated) {
                        do { return try Self.file(copy(url, into: folder)) }
                        catch { return Self.failed }
                    }.value
                    received(result)
                }
            } else if let image = object as? DropImage {
                let data = image.data
                Task {
                    let result = await Task.detached(priority: .userInitiated) {
                        do { return try Self.file(save(data, into: folder)) }
                        catch { return Self.failed }
                    }.value
                    received(result)
                }
            }
        }
        return true
    }

    @MainActor
    static func read(_ pasteboard: NSPasteboard) -> [Any] {
        guard pasteboard.types?.contains(NSPasteboard.PasteboardType(SidebarItem.contentType.identifier)) != true else { return [] }
        return pasteboard.readObjects(forClasses: [NSURL.self, NSFilePromiseReceiver.self, DropImage.self],
                                      options: [.urlReadingFileURLsOnly: true]) ?? []
    }

    static func reader(_ received: @escaping @MainActor @Sendable (Self) -> Void) -> @Sendable (URL, (any Error)?) -> Void {
        { url, error in
            let valid = error == nil && (try? url.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) == false
            Task { @MainActor in received(valid ? .file(url) : .failed) }
        }
    }

    private static func directory(in parent: URL = FileManager.default.temporaryDirectory) throws -> URL {
        let folder = parent.appending(path: UUID().uuidString, directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        return folder
    }

    private static func copy(_ url: URL, into folder: URL) throws -> URL {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        let source = (url as NSURL).filePathURL ?? url
        guard url.isFileURL, try source.resourceValues(forKeys: [.isDirectoryKey]).isDirectory != true else {
            throw CocoaError(.fileReadUnsupportedScheme)
        }
        let destination = try directory(in: folder).appending(path: source.lastPathComponent)
        try FileManager.default.copyItem(at: source, to: destination)
        return destination
    }

    private static func save(_ data: Data, into folder: URL) throws -> URL {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil), CGImageSourceGetCount(source) > 0,
              let identifier = CGImageSourceGetType(source), let type = UTType(identifier as String) else {
            throw CocoaError(.fileReadCorruptFile)
        }
        let destination = try directory(in: folder).appending(path: "Image")
            .appendingPathExtension(type.preferredFilenameExtension ?? "png")
        try data.write(to: destination, options: .atomic)
        return destination
    }
}

private final class DropImage: NSObject, NSPasteboardReading {
    let data: Data

    static func readableTypes(for pasteboard: NSPasteboard) -> [NSPasteboard.PasteboardType] {
        NSImage.imageTypes.filter { UTType($0)?.conforms(to: .image) == true }.map { NSPasteboard.PasteboardType($0) }
    }

    static func readingOptions(forType type: NSPasteboard.PasteboardType, pasteboard: NSPasteboard) -> NSPasteboard.ReadingOptions {
        .asData
    }

    required init?(pasteboardPropertyList propertyList: Any, ofType type: NSPasteboard.PasteboardType) {
        guard let data = propertyList as? Data else { return nil }
        self.data = data
    }
}
#endif
