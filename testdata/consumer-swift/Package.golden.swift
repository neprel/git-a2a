// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Consumer",
    dependencies: [
        .package(url: "https://github.com/apple/swift-argument-parser.git", from: "1.5.0"), // keep
        // git-a2a:begin AcmeLibUtils
        .package(url: "https://github.com/acme/lib-utils.git", revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
        // git-a2a:end AcmeLibUtils
    ],
    targets: [.target(name: "Consumer")]
)
