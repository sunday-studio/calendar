// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "FeatureEvent",
    platforms: [.iOS("26.0")],
    products: [.library(name: "FeatureEvent", targets: ["FeatureEvent"])],
    dependencies: [
        .package(path: "../CalendarCore"),
        .package(path: "../CalendarUI"),
    ],
    targets: [
        .target(
            name: "FeatureEvent",
            dependencies: ["CalendarCore", "CalendarUI"]
        ),
    ]
)
