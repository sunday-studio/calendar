// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "FeatureAccounts",
    platforms: [.iOS("26.0")],
    products: [.library(name: "FeatureAccounts", targets: ["FeatureAccounts"])],
    dependencies: [
        .package(path: "../CalendarCore"),
        .package(path: "../CalendarUI"),
    ],
    targets: [
        .target(
            name: "FeatureAccounts",
            dependencies: ["CalendarCore", "CalendarUI"]
        ),
    ]
)
