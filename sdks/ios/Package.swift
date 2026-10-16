// swift-tools-version:5.7
import PackageDescription

let package = Package(
    name: "MSeismo",
    platforms: [.iOS(.v13)],
    products: [
        .library(name: "MSeismo", targets: ["MSeismo"])
    ],
    targets: [
        .target(name: "MSeismo"),
        .testTarget(name: "MSeismoTests", dependencies: ["MSeismo"]),
    ]
)
