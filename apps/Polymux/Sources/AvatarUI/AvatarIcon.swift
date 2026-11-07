import SwiftUI
import AvatarKit

/// A create action drawn as an avatar: one round bot, or a group of the shapes.
enum AvatarIcon: String {
    case bot, group
}

#if os(iOS)
extension AvatarIcon {
    fileprivate struct Face {
        let shape: ShapeID
        let color: ColorID
        let center: CGPoint
        let size: CGFloat
        /// Moves the eyes, in face radii, to fit a shape narrower at the top.
        var gaze = CGPoint.zero
    }

    /// Back to front, in a 24pt square. A group sits like `AvatarStack`'s three members.
    fileprivate var faces: [Face] {
        switch self {
        case .bot:
            [Face(shape: .circle, color: .ink, center: CGPoint(x: 12, y: 12), size: 21)]
        case .group:
            [Face(shape: .square, color: .pink, center: CGPoint(x: 17.4, y: 17.2), size: 14.5),
             Face(shape: .triangle, color: .blue, center: CGPoint(x: 6.6, y: 16.2), size: 15,
                  gaze: CGPoint(x: -0.3, y: 0.55)),
             Face(shape: .circle, color: .ink, center: CGPoint(x: 12, y: 6.4), size: 12)]
        }
    }

    @MainActor private static var cache: [String: Image] = [:]

    /// Native menus show only images and don't tint custom ones, so it's drawn for the scheme.
    @MainActor func image(_ scheme: ColorScheme) -> Image {
        let key = "\(rawValue) \(scheme)"
        if let image = Self.cache[key] { return image }
        let renderer = ImageRenderer(content: AvatarGlyph(icon: self, scheme: scheme))
        renderer.scale = 3
        let image = Image(uiImage: renderer.uiImage ?? UIImage()).renderingMode(.original)
        Self.cache[key] = image
        return image
    }
}

private struct AvatarGlyph: View {
    let icon: AvatarIcon
    let scheme: ColorScheme

    /// As a resting avatar draws them, in face radii from its centre.
    private static let eyes: [(x: Double, y: Double, width: Double, height: Double)] =
        [(0.12, -0.36, 0.25, 0.55), (0.57, -0.54, 0.2, 0.4)]

    var body: some View {
        Canvas { context, _ in
            for face in icon.faces {
                let frame = CGRect(x: face.center.x - face.size / 2, y: face.center.y - face.size / 2,
                                   width: face.size, height: face.size)
                let outline = AvatarOutline(radii: face.shape.radii).path(in: frame)
                // The same gap a group's avatars keep where they overlap.
                context.blendMode = .destinationOut
                context.stroke(outline, with: .color(.black), lineWidth: 2)
                context.blendMode = .normal
                context.fill(outline, with: .color(face.color.avatarColor(scheme)))
                let radius = face.size / 2
                for eye in Self.eyes {
                    let bounds = CGRect(x: -eye.width * radius / 2, y: -eye.height * radius / 2,
                                        width: eye.width * radius, height: eye.height * radius)
                    var slanted = context
                    slanted.translateBy(x: frame.midX + (eye.x + face.gaze.x) * radius,
                                        y: frame.midY + (eye.y + face.gaze.y) * radius)
                    slanted.rotate(by: .degrees(-30))
                    slanted.fill(Path(ellipseIn: bounds), with: .color(face.color.eyeColor(scheme)))
                }
            }
        }
        .frame(width: 24, height: 24)
    }
}

/// An avatar's silhouette, from the radii its renderer uses.
private struct AvatarOutline: Shape {
    let radii: [Double]

    func path(in rect: CGRect) -> Path {
        let scale = min(rect.width, rect.height) / 2 / (radii.max() ?? 1)
        return Path { path in
            for (index, radius) in radii.enumerated() {
                let angle = Double(index) / Double(radii.count) * 2 * .pi
                let point = CGPoint(x: rect.midX + cos(angle) * radius * scale,
                                    y: rect.midY + sin(angle) * radius * scale)
                if index == 0 { path.move(to: point) } else { path.addLine(to: point) }
            }
            path.closeSubpath()
        }
    }
}
#endif
