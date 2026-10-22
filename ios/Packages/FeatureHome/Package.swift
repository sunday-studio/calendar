// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "FeatureHome",
    platforms: [.iOS("26.0")],
    products: [.library(name: "FeatureHome", targets: ["FeatureHome"])],
    dependencies: [
        .package(path: "../CalendarCore"),
        .package(path: "../CalendarUI"),
    ],
    targets: [
        .target(
            name: "FeatureHome",
            dependencies: ["CalendarCore", "CalendarUI"]
        ),
    ]
)
