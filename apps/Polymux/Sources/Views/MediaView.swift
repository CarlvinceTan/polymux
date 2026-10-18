import SwiftUI
import AVKit
import Combine
import QuickLook
import UniformTypeIdentifiers
import PolymuxKit

struct MediaView: View {
    let attachment: Attachment
    let client: APIClient
    var bubbleColor = Color.secondary.opacity(0.1)
    var bubbleInk = Color.primary
    var respond: (() -> Void)? = nil
    var action: ((String) -> Void)? = nil
    @State private var preview: URL?
    @State private var downloaded: URL?
    @State private var downloading = false
    @State private var error: String?
    @State private var exporting = false
    @State private var document: DownloadDocument?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let url = client.resolve(attachment.url) {
                switch attachment.kind {
                case "image":
                    Button { download(previewing: true) } label: {
                        AsyncImage(url: client.resolve(attachment.thumbnailURL ?? attachment.url)) { image in
                            image.resizable().scaledToFit()
                        } placeholder: {
                            ProgressView().frame(width: 180, height: 140)
                        }
                        .frame(maxWidth: 320, maxHeight: 280)
                        .clipShape(RoundedRectangle(cornerRadius: BubbleStyle.radius))
                    }.buttonStyle(.plain).accessibilityLabel("View \(attachment.name)")
                case "video":
                    MovieView(url: url, poster: client.resolve(attachment.thumbnailURL ?? ""))
                        .frame(width: 280, height: 190)
                        .clipShape(RoundedRectangle(cornerRadius: BubbleStyle.radius))
                case "audio":
                    AudioView(url: url, name: attachment.name, bubbleColor: bubbleColor, ink: bubbleInk)
                case "html":
                    HTMLView(url: url, name: attachment.name,
                             width: attachment.width, height: attachment.height, action: action)
                default:
                    Button { download(previewing: true) } label: {
                        HStack(spacing: 10) {
                            Image(systemName: "doc").font(.title2).foregroundStyle(.secondary)
                            VStack(alignment: .leading, spacing: 3) {
                                Text(attachment.name).font(.subheadline).lineLimit(2)
                                Text(ByteCountFormatter.string(fromByteCount: attachment.size, countStyle: .file))
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer(minLength: 4)
                            Image(systemName: "arrow.down.circle").foregroundStyle(.secondary)
                        }
                    }.buttonStyle(.plain)
                }
            }
            if let error { Text(error).font(.caption).foregroundStyle(.secondary) }
        }
        .overlay {
            if downloading { ProgressView().controlSize(.small) }
        }
        .shareMenu(item: downloaded.map(ShareItem.file)) { share in
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
            if case .failure(let failure) = result { error = failure.localizedDescription }
        }
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
                let file = try await MediaCache.shared.file(url: url, name: attachment.name)
                downloaded = file
                if previewing { preview = file }
                if saving { export(file) }
            } catch { self.error = error.localizedDescription }
            downloading = false
        }
    }

    private func export(_ file: URL) {
        do {
            document = DownloadDocument(data: try Data(contentsOf: file))
            exporting = true
        } catch { self.error = error.localizedDescription }
    }
}

struct MovieView: View {
    let url: URL
    let poster: URL?
    @State private var player: AVPlayer?

    var body: some View {
        Group {
            if let player {
                NativeMovie(player: player)
            } else {
                Button {
                    let next = AVPlayer(url: url)
                    player = next
                    next.play()
                } label: {
                    ZStack {
                        Color.black.opacity(0.85)
                        if let poster {
                            AsyncImage(url: poster) { image in image.resizable().scaledToFit() } placeholder: { Color.clear }
                        }
                        Image(systemName: "play.circle.fill").font(.system(size: 42)).foregroundStyle(.white)
                    }
                }.buttonStyle(.plain).accessibilityLabel("Play video")
            }
        }
        .onDisappear { player?.pause() }
    }
}

#if os(macOS)
private struct NativeMovie: NSViewRepresentable {
    let player: AVPlayer

    func makeNSView(context: Context) -> AVPlayerView {
        let view = AVPlayerView()
        view.player = player
        view.controlsStyle = .inline
        view.videoGravity = .resizeAspect
        view.showsFullScreenToggleButton = true
        return view
    }

    func updateNSView(_ view: AVPlayerView, context: Context) {
        if view.player !== player { view.player = player }
    }

    static func dismantleNSView(_ view: AVPlayerView, coordinator: ()) {
        view.player?.pause()
        view.player = nil
    }
}
#else
private struct NativeMovie: UIViewControllerRepresentable {
    let player: AVPlayer

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let controller = AVPlayerViewController()
        controller.player = player
        controller.showsPlaybackControls = true
        controller.videoGravity = .resizeAspect
        return controller
    }

    func updateUIViewController(_ controller: AVPlayerViewController, context: Context) {
        if controller.player !== player { controller.player = player }
    }

    static func dismantleUIViewController(_ controller: AVPlayerViewController, coordinator: ()) {
        controller.player?.pause()
        controller.player = nil
    }
}
#endif

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

    func file(url: URL, name: String) async throws -> URL {
        let support = try FileManager.default.url(for: .cachesDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
        let hash = url.absoluteString.utf8.reduce(UInt64(5381)) { ($0 &* 33) &+ UInt64($1) }
        let directory = support.appending(path: "Polymux/Media/\(hash)", directoryHint: .isDirectory)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let file = directory.appending(path: (name as NSString).lastPathComponent)
        if FileManager.default.fileExists(atPath: file.path) {
            try FileManager.default.setAttributes([.modificationDate: Date()], ofItemAtPath: file.path)
            return file
        }
        let (temporary, response) = try await URLSession.shared.download(from: url)
        guard let response = response as? HTTPURLResponse, response.statusCode == 200 else { throw URLError(.badServerResponse) }
        try FileManager.default.moveItem(at: temporary, to: file)
        try evict(root: support.appending(path: "Polymux/Media"), keeping: file)
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
