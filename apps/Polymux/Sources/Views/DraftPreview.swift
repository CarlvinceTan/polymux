import SwiftUI
import QuickLook
import QuickLookThumbnailing
import UniformTypeIdentifiers

struct DraftPreview: View {
    let file: DraftFile
    let remove: () -> Void
    @Environment(\.displayScale) private var displayScale
    @State private var thumbnail: CGImage?
    @State private var preview: URL?
    @State private var hovered = false

    private let side: CGFloat = 80

    var body: some View {
        Button { if file.url.isFileURL { preview = file.url } } label: {
            ZStack {
                Color.secondary.opacity(0.1)
                if let thumbnail {
                    Image(decorative: thumbnail, scale: displayScale)
                        .resizable().scaledToFill()
                        .frame(width: side, height: side)
                } else {
                    Image(systemName: symbol)
                        .font(.system(size: 28, weight: .light))
                        .foregroundStyle(.secondary)
                    VStack {
                        Spacer()
                        filename
                            .padding(.horizontal, 7).padding(.bottom, 7)
                    }
                }
            }
            .frame(width: side, height: side)
            .clipShape(.rect(cornerRadius: 12))
            .contentShape(.rect(cornerRadius: 12))
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Preview \(file.url.lastPathComponent)")
        .accessibilityHint(file.kind.localizedDescription ?? "Attachment")
        .overlay(alignment: .topTrailing) {
            removeButton
                #if os(macOS)
                .opacity(hovered ? 1 : 0)
                .allowsHitTesting(hovered)
                .accessibilityHidden(!hovered)
                #endif
        }
        #if os(macOS)
        .onHover { hovered = $0 }
        #endif
        .contextMenu {
            Button("Remove attachment", systemImage: "trash", action: remove)
        }
        .quickLookPreview($preview)
        .task(id: DraftKey(url: file.url, scale: displayScale)) {
            thumbnail = nil
            let image = await DraftThumbnails.image(file: file, side: side, scale: displayScale)
            guard !Task.isCancelled else { return }
            thumbnail = image
        }
    }

    private var filename: some View {
        let extensionName = file.url.pathExtension
        return HStack(spacing: 0) {
            Text(extensionName.isEmpty ? file.url.lastPathComponent : file.url.deletingPathExtension().lastPathComponent)
                .lineLimit(1).truncationMode(.middle)
            if !extensionName.isEmpty {
                Text(".\(extensionName)").fixedSize().layoutPriority(1)
            }
        }
        .font(.system(size: 10)).foregroundStyle(.secondary)
    }

    private var removeButton: some View {
        Button(action: remove) {
            Image(systemName: "xmark")
                .font(.system(size: 8, weight: .semibold))
                .frame(width: 18, height: 18)
                .glassEffect(.regular.interactive(), in: Circle())
                #if os(iOS)
                .frame(width: 44, height: 44, alignment: .topTrailing)
                .contentShape(Rectangle())
                #else
                .contentShape(Circle())
                #endif
        }
        .buttonStyle(.plain)
        .padding(4)
        .accessibilityLabel("Remove \(file.url.lastPathComponent)")
    }

    private var symbol: String {
        if file.kind.conforms(to: .image) { return "photo" }
        if file.kind.conforms(to: .movie) { return "film" }
        if file.kind.conforms(to: .audio) { return "waveform" }
        if file.kind.conforms(to: .pdf) { return "doc.richtext" }
        if file.kind.conforms(to: .html) { return "globe" }
        if ["ics", "ical"].contains(file.url.pathExtension.lowercased()) { return "calendar" }
        if file.kind.conforms(to: .text) { return "doc.text" }
        return "doc"
    }

}

private struct DraftKey: Hashable {
    let url: URL
    let scale: CGFloat
}

private final class DraftImage {
    let image: CGImage?

    init(_ image: CGImage?) { self.image = image }
}

@MainActor
private enum DraftThumbnails {
    private static let cache: NSCache<NSString, DraftImage> = {
        let cache = NSCache<NSString, DraftImage>()
        cache.countLimit = 80
        cache.totalCostLimit = 16 * 1024 * 1024
        return cache
    }()

    static func image(file: DraftFile, side: CGFloat, scale: CGFloat) async -> CGImage? {
        guard file.url.isFileURL, !Task.isCancelled,
              let values = try? file.url.resourceValues(forKeys: [.isRegularFileKey, .fileSizeKey, .contentModificationDateKey]),
              values.isRegularFile == true else { return nil }
        let key = "\(file.url.absoluteString)|\(values.fileSize ?? 0)|\(values.contentModificationDate?.timeIntervalSince1970 ?? 0)|\(side)|\(scale)" as NSString
        if let cached = cache.object(forKey: key) { return cached.image }
        let request = QLThumbnailGenerator.Request(fileAt: file.url, size: CGSize(width: side, height: side),
                                                  scale: scale, representationTypes: [.thumbnail, .lowQualityThumbnail])
        request.contentType = file.kind
        let image = await DraftRequest(request).image()
        guard !Task.isCancelled else { return nil }
        let cost = image.map { $0.bytesPerRow * $0.height } ?? 1
        cache.setObject(DraftImage(image), forKey: key, cost: cost)
        return image
    }
}

private nonisolated final class DraftRequest: @unchecked Sendable {
    private let request: QLThumbnailGenerator.Request
    private let lock = NSLock()
    private var continuation: CheckedContinuation<CGImage?, Never>?
    private var finished = false

    init(_ request: QLThumbnailGenerator.Request) { self.request = request }

    func image() async -> CGImage? {
        await withTaskCancellationHandler {
            await withCheckedContinuation { continuation in
                lock.lock()
                guard !finished else {
                    lock.unlock()
                    continuation.resume(returning: nil)
                    return
                }
                self.continuation = continuation
                lock.unlock()
                QLThumbnailGenerator.shared.generateBestRepresentation(for: request) { @Sendable [weak self] representation, _ in
                    self?.finish(representation?.type == .icon ? nil : representation?.cgImage)
                }
                lock.lock()
                let cancelled = finished
                lock.unlock()
                if cancelled { QLThumbnailGenerator.shared.cancel(request) }
            }
        } onCancel: { cancel() }
    }

    private func cancel() {
        finish(nil)
        QLThumbnailGenerator.shared.cancel(request)
    }

    private func finish(_ image: CGImage?) {
        lock.lock()
        guard !finished else { lock.unlock(); return }
        finished = true
        let continuation = continuation
        self.continuation = nil
        lock.unlock()
        continuation?.resume(returning: image)
    }
}
