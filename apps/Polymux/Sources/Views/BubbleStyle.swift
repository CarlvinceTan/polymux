import SwiftUI

enum BubbleStyle {
    static let radius: CGFloat = 16
    static let width: CGFloat = 550
    static let cardHeight: CGFloat = 400

    static func incoming(_ scheme: ColorScheme) -> Color {
        scheme == .dark ? Color(white: 66.0 / 255) : .secondary.opacity(0.1)
    }

    static func reply(_ scheme: ColorScheme) -> Color {
        scheme == .dark ? Color(white: 82.0 / 255) : .primary.opacity(0.12)
    }
}
