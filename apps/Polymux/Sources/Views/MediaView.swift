import SwiftUI
import ImageIO
import CryptoKit
import Combine
import QuickLook
import UniformTypeIdentifiers
import PolymuxKit

struct MediaView: View {
    let attachment: Attachment
    let client: APIClient
    var bubbleColor = Color.secondary.opacity(0.1)
    var bubbleInk = Color.primary
    var compact = false
    var alignment: HorizontalAlignment = .leading
    var respond: (() -> Void)? = nil
    var action: ((String) -> Void)? = nil
    var reactions: [Reaction] = []
    var react: ((String) -> Void)? = nil
    @State private var preview: URL?
    @State private var downloaded: URL?
    @State private var downloading = false
    @State private var error: String?
    @State private var exporting = false
    @State private var document: DownloadDocument?

    var body: some View {
        VStack(alignment: alignment, spacing: 6) {
            if let url = client.resolve(attachment.url) {
                if compact {
                    compactRow
                } else {
                    switch attachment.kind {
                    case AttachmentKind.image:
                        Button { download(previewing: true) } label: {
                            CachedImage(url: client.resolve(attachment.thumbnailURL ?? attachment.url), name: attachment.name, client: client) { image in
                                image.resizable().scaledToFit()
                                    .clipShape(.rect(cornerRadius: BubbleStyle.radius))
                                    .overlay {
                                        if downloading { MediaProgress(name: attachment.name) }
                                    }
                            } placeholder: {
                                ProgressView().frame(width: 180, height: 140)
                                    .accessibilityLabel("Loading \(attachment.name)")
                            }
                            .frame(maxWidth: 320, maxHeight: 280, alignment: alignment == .trailing ? .trailing : .leading)
                        }.buttonStyle(.plain).accessibilityLabel("View \(attachment.name)")
                            .accessibilityValue(downloading ? "Loading" : "")
                    case AttachmentKind.video:
                        VideoPreview(name: attachment.name, poster: client.resolve(attachment.thumbnailURL ?? ""),
                                     client: client, loading: downloading) {
                            download(previewing: true)
                        }
                    case AttachmentKind.audio:
                        AudioView(url: url, name: attachment.name, client: client, bubbleColor: bubbleColor, ink: bubbleInk)
                    case AttachmentKind.html:
                        HTMLView(url: url, name: attachment.name, client: client,
                                 width: attachment.width, height: attachment.height, action: action)
                    default:
                        Button { download(previewing: true) } label: {
                            HStack(spacing: 10) {
                                Image(systemName: "doc").font(.title2).foregroundStyle(bubbleInk.opacity(0.75))
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(attachment.name).font(.subheadline).lineLimit(2)
                                    Text(ByteCountFormatter.string(fromByteCount: attachment.size, countStyle: .file))
                                        .font(.caption).foregroundStyle(bubbleInk.opacity(0.75))
                                }
                                Spacer(minLength: 4)
                                Image(systemName: "arrow.down.circle").foregroundStyle(bubbleInk.opacity(0.75))
                            }
                        }.buttonStyle(.plain)
                    }
                }
            } else {
                Text("Attachment unavailable").font(.caption).foregroundStyle(bubbleInk.opacity(0.75))
            }
            if let error { Text(error).font(.caption).foregroundStyle(bubbleInk.opacity(0.75)) }
        }
        .overlay {
            if downloading, compact || ![AttachmentKind.image, AttachmentKind.video].contains(attachment.kind) {
                ProgressView().controlSize(.small).accessibilityLabel("Loading \(attachment.name)")
            }
        }
        .shareMenu(item: downloaded.map(ShareItem.file)) { share in
            if let react { ReactionMenu(reactions: reactions, react: react) }
            if let respond {
                Button("Reply", systemImage: "arrowshape.turn.up.left", action: respond)
            }
            Button("Open", systemImage: "arrow.up.right.square") { download(previewing: true) }
                .disabled(downloading)
            Button("Save…", systemImage: "square.and.arrow.down") { download(previewing: false, saving: true) }
                .disabled(downloading)
            if let share { share }
        }
        .quickLookPreview($preview)
        .fileExporter(isPresented: $exporting, document: document, contentType: UTType(mimeType: attachment.mimeType) ?? .data,
                      defaultFilename: attachment.name) { result in
            if case .failure = result { error = "Couldn’t save this file." }
        }
    }

    private var compactRow: some View {
        Button { download(previewing: true) } label: {
            HStack(spacing: 6) {
                Group {
                    if [AttachmentKind.image, AttachmentKind.video].contains(attachment.kind) {
                        AttachmentPreview(attachment: attachment, client: client, showsName: false, square: 20)
                    } else {
                        Image(systemName: attachment.kind == AttachmentKind.audio ? "waveform" : attachment.kind == AttachmentKind.html ? "globe" : "doc.text")
                            .foregroundStyle(.muted)
                    }
                }
                .frame(width: 24, height: 24)
                .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(attachment.name).lineLimit(1).truncationMode(.middle)
                    Text(ByteCountFormatter.string(fromByteCount: attachment.size, countStyle: .file))
                        .font(.caption).foregroundStyle(.muted)
                }
                Spacer(minLength: 4)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("View \(attachment.name)")
    }

    private func download(previewing: Bool, saving: Bool = false) {
        guard let url = client.resolve(attachment.url), !downloading else { return }
        if let downloaded {
            if previewing { preview = downloaded }
            if saving { export(downloaded) }
            return
        }
        downloading = true
        error = nil
        Task {
            do {
                let file = try await MediaCache.shared.file(url: url, name: attachment.name, client: client)
                downloaded = file
                if previewing { preview = file }
                if saving { export(file) }
            } catch { self.error = error.isOffline ? "Offline" : "Couldn’t open this file." }
            downloading = false
        }
    }

    private func export(_ file: URL) {
        // Reading a large file off the main thread keeps Save from stalling the UI.
        Task {
            do {
                let data = try await Task.detached(priority: .userInitiated) { try Data(contentsOf: file) }.value
                document = DownloadDocument(data: data)
                exporting = true
            } catch { self.error = "Couldn’t save this file." }
        }
    }
}

private struct MediaProgress: View {
    let name: String

    var body: some View {
        ProgressView().controlSize(.small)
            .accessibilityLabel("Loading \(name)")
    }
}

private struct VideoPreview: View {
    let name: String
    let poster: URL?
    let client: APIClient
    let loading: Bool
    let open: () -> Void

    var body: some View {
        Button(action: open) {
            ZStack {
                Color.black.opacity(0.85)
                if let poster {
                    CachedImage(url: poster, name: "poster-" + name, client: client) { image in
                        image.resizable().scaledToFit()
                    } placeholder: { Color.clear }
                }
                if loading {
                    // Light like the play icon, since the tile is always dark.
                    MediaProgress(name: name).environment(\.colorScheme, .dark)
                } else {
                    Image(systemName: "play.circle.fill").font(.system(size: 42)).foregroundStyle(.white)
                }
            }
            .frame(width: 280, height: 190)
            .clipShape(.rect(cornerRadius: BubbleStyle.radius))
        }
        .buttonStyle(.plain)
        .accessibilityLabel("View \(name)")
        .accessibilityValue(loading ? "Loading" : "")
    }
}

/// An image loaded through the authenticated media cache, which `AsyncImage` can't do.
struct CachedImage<Content: View, Placeholder: View>: View {
    let url: URL?
    let name: String
    let client: APIClient
    @ViewBuilder var content: (Image) -> Content
    @ViewBuilder var placeholder: () -> Placeholder
    var failure: (() -> AnyView)? = nil
    @State private var image: CGImage?
    @State private var failed = false

    var body: some View {
        Group {
            if let shown = image ?? url.flatMap(DecodedImages.cached) {
                content(Image(decorative: shown, scale: 1))
            } else if failed, let failure {
                failure()
            } else {
                placeholder()
            }
        }
        .task(id: url) {
            failed = false
            guard let url else { image = nil; failed = true; return }
            if let memo = DecodedImages.cached(url) { image = memo; return }
            image = nil
            do {
                let file = try await MediaCache.shared.file(url: url, name: name, client: client)
                image = try await DecodedImages.load(file, for: url)
            } catch { if !Task.isCancelled { failed = true } }
        }
    }
}

/// Decoded images by source URL, so a row scrolled back into view shows at once.
private enum DecodedImages {
    nonisolated(unsafe) private static let cache: NSCache<NSURL, CGImage> = {
        let cache = NSCache<NSURL, CGImage>()
        cache.totalCostLimit = 64 << 20
        return cache
    }()

    static func cached(_ url: URL) -> CGImage? { cache.object(forKey: url as NSURL) }

    /// Decodes off the main actor and keeps the result for `url`.
    static func load(_ file: URL, for url: URL) async throws -> CGImage {
        let image = try await Task.detached { try decode(file) }.value
        cache.setObject(image, forKey: url as NSURL, cost: image.bytesPerRow * image.height)
        return image
    }

    /// Decodes at most 1024 px on the long side: every use draws within 320 points.
    nonisolated private static func decode(_ file: URL) throws -> CGImage {
        let options = [kCGImageSourceCreateThumbnailFromImageAlways: true, kCGImageSourceCreateThumbnailWithTransform: true,
                       kCGImageSourceShouldCacheImmediately: true, kCGImageSourceThumbnailMaxPixelSize: 1024] as CFDictionary
        if let source = CGImageSourceCreateWithURL(file as CFURL, nil),
           let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options) {
            return image
        }
        // Formats ImageIO can't read, such as SVG, go through the platform image.
        #if os(macOS)
        guard let image = NSImage(contentsOf: file)?.cgImage(forProposedRect: nil, context: nil, hints: nil) else { throw CocoaError(.fileReadCorruptFile) }
        #else
        guard let image = UIImage(contentsOfFile: file.path)?.cgImage else { throw CocoaError(.fileReadCorruptFile) }
        #endif
        return image
    }
}

struct DownloadDocument: FileDocument {
    static let readableContentTypes: [UTType] = [.data]
    var data: Data
    init(data: Data) { self.data = data }
    init(configuration: ReadConfiguration) throws { data = configuration.file.regularFileContents ?? Data() }
    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper { FileWrapper(regularFileWithContents: data) }
}

actor MediaCache {
    static let shared = MediaCache()
    private let limit: Int64 = 200 * 1024 * 1024

    func text(url: URL, name: String, client: APIClient) async throws -> String {
        let file = try await file(url: url, name: name, client: client)
        let size = try file.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
        guard size <= 1024 * 1024 else { throw CocoaError(.fileReadCorruptFile) }
        let data = try Data(contentsOf: file)
        guard let text = String(data: data, encoding: .utf8) else { throw CocoaError(.fileReadCorruptFile) }
        return text
    }

    func file(url: URL, name: String, client: APIClient) async throws -> URL {
        // A file still uploading is shown from where it is.
        if url.isFileURL { return url }
        let root: URL
        if let path = ProcessInfo.processInfo.environment["POLYMUX_DATA_DIR"], !path.isEmpty {
            root = URL(fileURLWithPath: path, isDirectory: true).appending(path: "media", directoryHint: .isDirectory)
        } else {
            root = try FileManager.default.url(for: .cachesDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
                .appending(path: "Polymux/Media", directoryHint: .isDirectory)
        }
        let hash = SHA256.hash(data: Data(url.absoluteString.utf8)).map { String(format: "%02x", $0) }.joined()
        let directory = root.appending(path: hash, directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let file = directory.appending(path: (name as NSString).lastPathComponent)
        if FileManager.default.fileExists(atPath: file.path) {
            try FileManager.default.setAttributes([.modificationDate: Date()], ofItemAtPath: file.path)
            return file
        }
        let (temporary, response) = try await client.download(url)
        defer { try? FileManager.default.removeItem(at: temporary) }
        guard let response = response as? HTTPURLResponse, response.statusCode == 200 else { throw URLError(.badServerResponse) }
        if FileManager.default.fileExists(atPath: file.path) { return file }
        try FileManager.default.moveItem(at: temporary, to: file)
        try evict(root: root, keeping: file)
        return file
    }

    private func evict(root: URL, keeping: URL) throws {
        let keys: [URLResourceKey] = [.isRegularFileKey, .fileSizeKey, .contentModificationDateKey]
        guard let enumerator = FileManager.default.enumerator(at: root, includingPropertiesForKeys: keys) else { return }
        var files: [(URL, Int64, Date)] = []
        for case let url as URL in enumerator {
            let values = try url.resourceValues(forKeys: Set(keys))
            if values.isRegularFile == true {
                files.append((url, Int64(values.fileSize ?? 0), values.contentModificationDate ?? .distantPast))
            }
        }
        var size = files.reduce(Int64(0)) { $0 + $1.1 }
        for entry in files.sorted(by: { $0.2 < $1.2 }) where size > limit && entry.0 != keeping {
            try FileManager.default.removeItem(at: entry.0)
            size -= entry.1
        }
    }
}
