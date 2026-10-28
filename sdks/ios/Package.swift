// swift-tools-version:5.7
import PackageDescription

let package = Package(
    name: "MSeismo",
    // macOS is listed only so `swift build`/`swift test` on a Mac host can
    // resolve Combine/SwiftUI availability; this package still only ships
    // for iOS (see the iOS-only files guarded by #if os(iOS)).
    platforms: [.iOS(.v13), .macOS(.v10_15)],
    products: [
        .library(name: "MSeismo", targets: ["MSeismo"])
    ],
    targets: [
        .target(name: "MSeismo"),
        .testTarget(name: "MSeismoTests", dependencies: ["MSeismo"]),
    ]
)
