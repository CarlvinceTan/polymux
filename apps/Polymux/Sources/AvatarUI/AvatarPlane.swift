import SwiftUI

struct AvatarSlot {
    let canvas: AvatarCanvasState
    let input: AvatarInput
    var rect: CGRect
    var group: UUID?
    var order: Int = 0
    var gap: CGFloat = 0
}

struct AvatarAnchor {
    let tile: AvatarSlot
    let bounds: Anchor<CGRect>
}

struct AvatarTiles: PreferenceKey {
    static let defaultValue: [AvatarAnchor] = []
    static func reduce(value: inout [AvatarAnchor], nextValue: () -> [AvatarAnchor]) {
        value += nextValue()
    }
}

struct AvatarGroup {
    let id: UUID
    let order: Int
}

extension EnvironmentValues {
    @Entry var avatarPlane = false
    @Entry var rasterAvatars = false
    @Entry var avatarGroup: AvatarGroup? = nil
}

private struct AvatarPlane: ViewModifier {
    @Environment(\.avatarPlane) private var inherited
    @Environment(\.rasterAvatars) private var raster
    @Environment(\.displayScale) private var scale

    @ViewBuilder
    func body(content: Content) -> some View {
        if inherited {
            content
        } else {
            content
                .environment(\.avatarPlane, true)
                .overlayPreferenceValue(AvatarTiles.self) { anchors in
                    GeometryReader { geometry in
                        let tiles = anchors.map { anchor in
                            var tile = anchor.tile
                            tile.rect = geometry[anchor.bounds]
                            return tile
                        }
                        surface(tiles)
                    }
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
                }
        }
    }

    @ViewBuilder
    private func surface(_ tiles: [AvatarSlot]) -> some View {
        let region = tiles.reduce(CGRect.null) { $0.union($1.rect) }
        if !region.isNull, !region.isEmpty {
            let tiles = tiles.map {
                var tile = $0
                tile.rect = tile.rect.offsetBy(dx: -region.minX, dy: -region.minY)
                return tile
            }
            Group {
                if raster {
                    if let image = AvatarSurfaceView.image(tiles, size: region.size, scale: scale) {
                        Image(decorative: image, scale: scale)
                    }
                } else { AvatarSurface(tiles: tiles) }
            }
            .frame(width: region.width, height: region.height)
            .position(x: region.midX, y: region.midY)
        }
    }
}

extension AvatarSurfaceView {
    // Drag previews cannot snapshot native layer-backed avatar surfaces.
    static func image(_ tiles: [AvatarSlot], size: CGSize, scale: CGFloat) -> CGImage? {
        guard let context = CGContext(data: nil, width: Int(ceil(size.width * scale)),
                                      height: Int(ceil(size.height * scale)), bitsPerComponent: 8, bytesPerRow: 0,
                                      space: CGColorSpaceCreateDeviceRGB(),
                                      bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return nil }
        let view = AvatarSurfaceView()
        view.frame = CGRect(origin: .zero, size: size)
        view.update(tiles)
        context.scaleBy(x: scale, y: scale)
        context.translateBy(x: 0, y: size.height)
        context.scaleBy(x: 1, y: -1)
        #if os(macOS)
        view.layer?.render(in: context)
        #else
        view.layer.render(in: context)
        #endif
        view.stop()
        return context.makeImage()
    }
}

extension View {
    func avatarPlane() -> some View { modifier(AvatarPlane()) }
}
