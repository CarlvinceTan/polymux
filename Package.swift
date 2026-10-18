// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Polymux",
    platforms: [.iOS("27.0"), .macOS("27.0")],
    products: [
        .library(name: "PolymuxKit", targets: ["PolymuxKit"]),
        .library(name: "AvatarKit", targets: ["AvatarKit"]),
        .executable(name: "media-text", targets: ["MediaText"])
    ],
    dependencies: [
        .package(url: "https://github.com/n0-computer/iroh-ffi", exact: "1.1.0")
    ],
    targets: [
        .systemLibrary(name: "CSQLite", path: "packages/CSQLite"),
        .target(name: "PolymuxKit", dependencies: ["CSQLite", .product(name: "IrohLib", package: "iroh-ffi")], path: "packages/PolymuxKit/Sources/PolymuxKit"),
        .target(name: "AvatarKit", path: "packages/AvatarKit/Sources/AvatarKit", resources: [.copy("Resources/Bloub-LICENSE.txt")]),
        .target(name: "PolymuxAvatar", dependencies: ["PolymuxKit", "AvatarKit"], path: "apps/Polymux/Sources/Avatar"),
        .target(name: "PolymuxVoice", dependencies: ["PolymuxKit"], path: "apps/Polymux/Sources/Voice"),
        .executableTarget(name: "MediaText", path: "packages/MediaText/Sources/MediaText"),
        .testTarget(name: "PolymuxKitTests", dependencies: ["PolymuxKit", "PolymuxAvatar", "AvatarKit"], path: "packages/PolymuxKit/Tests/PolymuxKitTests"),
        .testTarget(name: "PolymuxVoiceTests", dependencies: ["PolymuxVoice", "PolymuxKit"], path: "apps/Polymux/Tests/VoiceTests"),
        .testTarget(name: "AvatarKitTests", dependencies: ["AvatarKit"], path: "packages/AvatarKit/Tests/AvatarKitTests", resources: [.copy("Fixtures")])
    ]
)
