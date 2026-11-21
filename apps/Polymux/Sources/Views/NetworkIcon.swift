import SwiftUI
import ImageIO
import UniformTypeIdentifiers

/// A network's icon: its chosen preset or picture, else the default for where it runs.
struct NetworkIcon: View {
    let icon: String?
    let fallback: String
    var size: CGFloat = 26

    static let presets = ["network", "house.fill", "building.2.fill", "briefcase.fill", "laptopcomputer", "server.rack",
                          "cloud.fill", "globe", "bolt.fill", "star.fill", "heart.fill", "leaf.fill",
                          "flame.fill", "gamecontroller.fill", "book.fill", "antenna.radiowaves.left.and.right"]

    var body: some View {
        if let picture = Self.picture(icon) {
            Image(decorative: picture, scale: 1)
                .resizable()
                .scaledToFill()
                .frame(width: size, height: size)
                .clipShape(.rect(cornerRadius: size / 4))
        } else {
            Image(systemName: icon.flatMap { $0.isEmpty || $0.hasPrefix("data:") ? nil : $0 } ?? fallback)
                .font(.system(size: size * 0.75))
        }
    }

    /// The image inside a data URL, if the icon is one.
    private static func picture(_ icon: String?) -> CGImage? {
        guard let icon, icon.hasPrefix("data:"), let comma = icon.firstIndex(of: ","),
              let data = Data(base64Encoded: String(icon[icon.index(after: comma)...])),
              let source = CGImageSourceCreateWithData(data as CFData, nil) else { return nil }
        return CGImageSourceCreateImageAtIndex(source, 0, nil)
    }

    /// A 128 px square PNG data URL from a picture file, cropped to its centre.
    nonisolated static func dataURL(_ url: URL) -> String? {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        let side = 128
        guard let source = CGImageSourceCreateWithURL(url as CFURL, nil),
              let thumb = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                  kCGImageSourceCreateThumbnailFromImageAlways: true,
                  kCGImageSourceCreateThumbnailWithTransform: true,
                  kCGImageSourceThumbnailMaxPixelSize: side * 4] as CFDictionary) else { return nil }
        let edge = min(thumb.width, thumb.height)
        guard let square = thumb.cropping(to: CGRect(x: (thumb.width - edge) / 2, y: (thumb.height - edge) / 2, width: edge, height: edge)),
              let context = CGContext(data: nil, width: side, height: side, bitsPerComponent: 8, bytesPerRow: 0,
                                      space: CGColorSpace(name: CGColorSpace.sRGB)!,
                                      bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return nil }
        context.interpolationQuality = .high
        context.draw(square, in: CGRect(x: 0, y: 0, width: side, height: side))
        guard let image = context.makeImage() else { return nil }
        let output = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(output, UTType.png.identifier as CFString, 1, nil) else { return nil }
        CGImageDestinationAddImage(destination, image, nil)
        guard CGImageDestinationFinalize(destination) else { return nil }
        return "data:image/png;base64," + (output as Data).base64EncodedString()
    }
}

/// Preset icons and a picture upload for the network.
struct NetworkIconPicker: View {
    let current: String?
    let fallback: String
    let choose: (String) -> Void
    @State private var picking = false
    @State private var failed = false

    private let columns = [GridItem(.adaptive(minimum: 40), spacing: 8)]

    var body: some View {
        Section {
            HStack(spacing: PanelStyle.headingInset) {
                NetworkIcon(icon: current, fallback: fallback).frame(width: 28)
                Spacer()
                Button("Upload picture") { picking = true }
                    .buttonStyle(.bordered)
                    .buttonBorderShape(.roundedRectangle)
                    .accessibilityLabel("Upload network icon")
                if current?.isEmpty == false {
                    Button("Reset") { choose("") }
                        .buttonStyle(.bordered)
                        .buttonBorderShape(.roundedRectangle)
                        .accessibilityLabel("Reset network icon")
                }
            }
            LazyVGrid(columns: columns, spacing: 8) {
                ForEach(NetworkIcon.presets, id: \.self) { symbol in
                    Button { choose(symbol) } label: {
                        Image(systemName: symbol)
                            .font(.title3)
                            .frame(width: 40, height: 40)
                            .background(current == symbol ? Color.accentColor.opacity(0.25) : Color.clear, in: .rect(cornerRadius: 8))
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(symbol)
                    .accessibilityAddTraits(current == symbol ? .isSelected : [])
                }
            }
        } header: {
            Text("Icon").foregroundStyle(Color.primary)
        } footer: {
            if failed { Text("Couldn’t use that picture.").font(.footnote).foregroundStyle(.muted) }
        }
        .panelRow()
        .fileImporter(isPresented: $picking, allowedContentTypes: [.image]) { result in
            guard case .success(let url) = result else { return }
            if let icon = NetworkIcon.dataURL(url) { failed = false; choose(icon) } else { failed = true }
        }
    }
}
