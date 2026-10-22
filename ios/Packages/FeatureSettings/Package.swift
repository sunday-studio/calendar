// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "FeatureSettings",
    platforms: [.iOS("26.0")],
    products: [.library(name: "FeatureSettings", targets: ["FeatureSettings"])],
    dependencies: [
        .package(path: "../CalendarCore"),
        .package(path: "../CalendarUI"),
    ],
    targets: [
        .target(
            name: "FeatureSettings",
            dependencies: ["CalendarCore", "CalendarUI"]
        ),
    ]
)
