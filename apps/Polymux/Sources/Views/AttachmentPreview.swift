import SwiftUI
import QuickLookThumbnailing
import UniformTypeIdentifiers
import PolymuxKit

struct AttachmentPreview: View {
    let attachment: Attachment
    let client: APIClient
    var showsName = true
    var square: CGFloat? = nil
    @Environment(\.displayScale) private var displayScale
    @State private var thumbnail: CGImage?
    @State private var loading = false

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Group {
                if let imageURL {
                    AsyncImage(url: imageURL) { phase in
                        switch phase {
                        case .success(let image): image.resizable().aspectRatio(contentMode: square == nil ? .fit : .fill)
                        case .empty: placeholder
                        case .failure: fallback
                        @unknown default: fallback
                        }
                    }
                } else if let thumbnail {
                    Image(decorative: thumbnail, scale: displayScale)
                        .resizable().aspectRatio(contentMode: square == nil ? .fit : .fill)
                } else if loading {
                    placeholder
                } else {
                    fallback
                }
            }
            .frame(width: square, height: square)
            .frame(maxWidth: square ?? 160, maxHeight: square ?? 140)
            .clipShape(.rect(cornerRadius: 8))
            if showsName {
                Text(attachment.name)
                    .font(.caption2).foregroundStyle(.secondary)
                    .lineLimit(1).truncationMode(.middle)
                    .frame(maxWidth: 160, alignment: .leading)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(attachment.name)
        .task(id: attachment.id) { await loadThumbnail() }
    }

    private var imageURL: URL? {
        if attachment.kind == "image" {
            return client.resolve(attachment.thumbnailURL ?? attachment.url)
        }
        if attachment.kind == "video", let poster = attachment.thumbnailURL, !poster.isEmpty {
            return client.resolve(poster)
        }
        return nil
    }

    private var placeholder: some View {
        ProgressView().controlSize(.small).frame(width: square ?? 100, height: square ?? 100)
    }

    private var fallback: some View {
        Image(systemName: symbol)
            .font(.system(size: 32, weight: .light)).foregroundStyle(.secondary)
            .frame(width: square ?? 100, height: square ?? 100)
            .background(.quaternary.opacity(0.45))
    }

    private var symbol: String {
        if attachment.kind == "image" { return "photo" }
        if attachment.kind == "video" { return "film" }
        if attachment.kind == "audio" { return "waveform" }
        let kind = UTType(mimeType: attachment.mimeType) ?? UTType(filenameExtension: (attachment.name as NSString).pathExtension)
        if kind?.conforms(to: .pdf) == true { return "doc.richtext" }
        if kind?.conforms(to: .html) == true { return "globe" }
        if ["ics", "ical"].contains((attachment.name as NSString).pathExtension.lowercased()) { return "calendar" }
        return "doc"
    }

    private func loadThumbnail() async {
        guard !Task.isCancelled else { return }
        thumbnail = nil
        loading = false
        guard imageURL == nil, let url = client.resolve(attachment.url) else { return }
        loading = true
        do {
            let file = try await MediaCache.shared.file(url: url, name: attachment.name)
            try Task.checkCancellation()
            let request = QLThumbnailGenerator.Request(fileAt: file, size: CGSize(width: 160, height: 140), scale: displayScale, representationTypes: .all)
            let representation = try await QLThumbnailGenerator.shared.generateBestRepresentation(for: request)
            try Task.checkCancellation()
            thumbnail = representation.cgImage
            loading = false
        } catch {
            guard !Task.isCancelled else { return }
            loading = false
        }
    }
}
