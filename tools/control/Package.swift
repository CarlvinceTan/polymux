// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "Control",
    platforms: [.macOS(.v15)],
    products: [.executable(name: "control", targets: ["Control"])],
    targets: [
        .executableTarget(name: "Control"),
        .testTarget(name: "ControlTests", dependencies: ["Control"])
    ]
)
