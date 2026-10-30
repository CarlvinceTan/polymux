import Foundation
import SwiftUI
import Testing
import AvatarKit
@testable import PolymuxAvatar

struct SenderColorTests {
    @Test @MainActor func blockedEyesContrastWithEveryAvatar() {
        func luminance(_ color: Color.Resolved) -> Double {
            let rgb = [Double(color.red), Double(color.green), Double(color.blue)]
            let linear = rgb.map { $0 <= 0.04045 ? $0 / 12.92 : pow(($0 + 0.055) / 1.055, 2.4) }
            return zip(linear, [0.2126, 0.7152, 0.0722]).reduce(0) { $0 + $1.0 * $1.1 }
        }
        for color in ColorID.allCases {
            for scheme in [ColorScheme.light, .dark] {
                let fill = luminance(color.avatarColor(scheme).resolve(in: EnvironmentValues()))
                let eye = luminance(color.eyeColor(scheme).resolve(in: EnvironmentValues()))
                #expect((max(fill, eye) + 0.05) / (min(fill, eye) + 0.05) >= 4.5, "\(color)/\(scheme)")
            }
        }
    }
}
