// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Polymux",
    platforms: [.iOS("27.0"), .macOS("27.0")],
    products: [
        .library(name: "PolymuxKit", targets: ["PolymuxKit"]),
        .library(name: "AvatarKit", targets: ["AvatarKit"]),
        .library(name: "AvatarKitC", type: .dynamic, targets: ["AvatarKitC"]),
        .library(name: "GraphKit", targets: ["GraphKit"]),
        .library(name: "PolymuxCall", targets: ["PolymuxCall"]),
        .library(name: "SpeechKit", targets: ["SpeechKit"]),
        .library(name: "PolymuxVoice", targets: ["PolymuxVoice"]),
        .library(name: "PolymuxUpdates", targets: ["PolymuxUpdates"]),
        .executable(name: "media-text", targets: ["MediaText"])
    ],
    dependencies: [
        .package(url: "https://github.com/n0-computer/iroh-ffi", exact: "1.2.0"),
        .package(url: "https://github.com/sparkle-project/Sparkle", exact: "2.10.1")
    ],
    targets: [
        .systemLibrary(name: "CSQLite", path: "packages/CSQLite"),
        .target(name: "PolymuxKit", dependencies: ["CSQLite", .product(name: "IrohLib", package: "iroh-ffi")], path: "packages/PolymuxKit/Sources/PolymuxKit"),
        // Avatars draw every frame; unoptimized, a few animal avatars saturate a debug build's main thread.
        .target(name: "AvatarKit", path: "packages/AvatarKit/Sources/AvatarKit", resources: [.copy("Resources/Bloub-LICENSE.txt"), .copy("Resources/Characters-LICENSE.txt"), .copy("Resources/DSBot-LICENSE.txt")],
                swiftSettings: [.unsafeFlags(["-O"], .when(configuration: .debug))]),
        .target(name: "AvatarKitCTypes", path: "packages/AvatarKitC/Sources/AvatarKitCTypes", publicHeadersPath: "include"),
        .target(name: "AvatarKitC", dependencies: ["AvatarKit", "AvatarKitCTypes"], path: "packages/AvatarKitC/Sources/AvatarKitC"),
        .target(name: "PolymuxAvatar", dependencies: ["PolymuxKit", "AvatarKit"], path: "apps/Polymux/Sources/Avatar"),
        // Linked by the app and its Live Activity extension, so it carries no other dependencies.
        .target(name: "PolymuxCall", path: "packages/PolymuxCall/Sources/PolymuxCall"),
        // Speech-to-text: microphone capture, Apple recognition and dictation post-processing.
        .target(name: "SpeechKit", path: "packages/SpeechKit/Sources/SpeechKit"),
        // Speech-to-speech live calls; the app supplies the agent consult through VoiceReply.
        .target(name: "PolymuxVoice", dependencies: ["PolymuxKit", "PolymuxCall", "SpeechKit"], path: "packages/PolymuxVoice/Sources/PolymuxVoice"),
        .target(name: "PolymuxUpdates", dependencies: [.product(name: "Sparkle", package: "Sparkle", condition: .when(platforms: [.macOS]))], path: "packages/PolymuxUpdates/Sources/PolymuxUpdates"),
        .executableTarget(name: "MediaText", path: "packages/MediaText/Sources/MediaText"),
        .target(name: "GraphKit", path: "packages/GraphKit/Sources/GraphKit"),
        .testTarget(name: "PolymuxKitTests", dependencies: ["PolymuxKit", "PolymuxAvatar", "AvatarKit"], path: "packages/PolymuxKit/Tests/PolymuxKitTests"),
        .testTarget(name: "SpeechKitTests", dependencies: ["SpeechKit"], path: "packages/SpeechKit/Tests/SpeechKitTests"),
        .testTarget(name: "PolymuxVoiceTests", dependencies: ["PolymuxVoice", "PolymuxKit", "SpeechKit"], path: "packages/PolymuxVoice/Tests/PolymuxVoiceTests"),
        .testTarget(name: "AvatarKitTests", dependencies: ["AvatarKit"], path: "packages/AvatarKit/Tests/AvatarKitTests", resources: [.copy("Fixtures")]),
        .testTarget(name: "AvatarKitCTests", dependencies: ["AvatarKit", "AvatarKitC", "AvatarKitCTypes"], path: "packages/AvatarKitC/Tests/AvatarKitCTests"),
        .testTarget(name: "GraphKitTests", dependencies: ["GraphKit"], path: "packages/GraphKit/Tests/GraphKitTests", resources: [.copy("Fixtures")])
    ]
)
