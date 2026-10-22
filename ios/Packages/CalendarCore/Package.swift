// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "CalendarCore",
    platforms: [.iOS("26.0")],
    products: [.library(name: "CalendarCore", targets: ["CalendarCore"])],
    dependencies: [
        .package(url: "https://github.com/groue/GRDB.swift", from: "7.0.0"),
    ],
    targets: [
        .target(
            name: "CalendarCore",
            dependencies: [.product(name: "GRDB", package: "GRDB.swift")]
        ),
        .testTarget(
            name: "CalendarCoreTests",
            dependencies: ["CalendarCore"],
            resources: [.copy("Fixtures")]
        ),
    ]
)
