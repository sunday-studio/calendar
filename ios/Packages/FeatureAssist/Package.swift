// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "FeatureAssist",
    platforms: [.iOS("26.0")],
    products: [.library(name: "FeatureAssist", targets: ["FeatureAssist"])],
    dependencies: [
        .package(path: "../CalendarCore"),
        .package(path: "../CalendarUI"),
    ],
    targets: [
        .target(name: "FeatureAssist", dependencies: ["CalendarCore", "CalendarUI"]),
    ]
)
