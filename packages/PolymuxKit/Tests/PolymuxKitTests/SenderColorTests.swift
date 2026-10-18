import Foundation
import SwiftUI
import Testing
import AvatarKit
@testable import PolymuxAvatar

struct SenderColorTests {
    @Test func paletteContrast() {
        for color in ColorID.allCases {
            for scheme in [ColorScheme.light, .dark] {
                let rgb = color.nameRGB(scheme)
                let linear = rgb.map { $0 <= 0.04045 ? $0 / 12.92 : pow(($0 + 0.055) / 1.055, 2.4) }
                let luminance = zip(linear, [0.2126, 0.7152, 0.0722]).reduce(0) { $0 + $1.0 * $1.1 }
                // Bound the transcript surfaces, including the lighter dark macOS surface.
                let background = scheme == .dark ? 0.05 : 0.85
                let ratio = (max(luminance, background) + 0.05) / (min(luminance, background) + 0.05)
                #expect(ratio >= 4.5, "\(color) in \(scheme): \(ratio)")
            }
        }
    }

    @Test func adaptiveNames() {
        for color in [ColorID.ink, .cream] {
            #expect(color.nameRGB(.light) == [0, 0, 0])
            #expect(color.nameRGB(.dark) == [1, 1, 1])
        }
        #expect(ColorID.red.nameRGB(.light) != ColorID.blue.nameRGB(.light))
        #expect(ColorID.red.nameRGB(.dark) != ColorID.blue.nameRGB(.dark))
    }
}
