import SwiftUI
import AvatarKit

extension ColorID {
    static let avatarColors = allCases.filter { $0 != .cream }

    var isAdaptive: Bool { self == .ink || self == .cream }
    var avatarID: ColorID { isAdaptive ? .ink : self }
    var avatarLabel: String { isAdaptive ? "Black / White" : label }

    func avatarColor(_ scheme: ColorScheme) -> Color {
        if isAdaptive { return scheme == .dark ? .white : .black }
        return Color(avatarHex: hex)
    }

    func eyeColor(_ scheme: ColorScheme) -> Color {
        if isAdaptive { return scheme == .dark ? .black : .white }
        let value = UInt64(hex.trimmingCharacters(in: CharacterSet(charactersIn: "#")), radix: 16) ?? 0
        let rgb = [16, 8, 0].map { Double((value >> $0) & 255) / 255 }
        let linear = rgb.map { $0 <= 0.04045 ? $0 / 12.92 : pow(($0 + 0.055) / 1.055, 2.4) }
        let luminance = zip(linear, [0.2126, 0.7152, 0.0722]).reduce(0) { $0 + $1.0 * $1.1 }
        return luminance > 0.179 ? .black : .white
    }
}

struct AvatarSwatch: View {
    let color: ColorID

    var body: some View {
        Circle()
            .fill(color.isAdaptive ? Color.black : Color(avatarHex: color.hex))
            .overlay {
                if color.isAdaptive { SplitSwatch().fill(.white) }
            }
            .clipShape(Circle())
    }
}

private struct SplitSwatch: Shape {
    func path(in rect: CGRect) -> Path {
        Path { path in
            path.move(to: CGPoint(x: rect.minX, y: rect.minY))
            path.addLine(to: CGPoint(x: rect.maxX, y: rect.minY))
            path.addLine(to: CGPoint(x: rect.minX, y: rect.maxY))
            path.closeSubpath()
        }
    }
}
