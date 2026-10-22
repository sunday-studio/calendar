// swift-tools-version: 6.0
import PackageDescription

// Widgets, notifications, push and Live Activity logic shared by the app and its extensions.
// SurfacesCore: pure logic over CalendarCore (no SwiftUI), safe in the notification service.
// SurfacesUI: SwiftUI views for the widget extension and the app's widget gallery (uses CalendarUI).
let package = Package(
    name: "Surfaces",
    platforms: [.iOS("26.0")],
    products: [
        .library(name: "SurfacesCore", targets: ["SurfacesCore"]),
        .library(name: "SurfacesUI", targets: ["SurfacesUI"]),
    ],
    dependencies: [
        .package(path: "../CalendarCore"),
        .package(path: "../CalendarUI"),
    ],
    targets: [
        .target(name: "SurfacesCore", dependencies: ["CalendarCore"]),
        .target(name: "SurfacesUI", dependencies: ["SurfacesCore", "CalendarCore", "CalendarUI"]),
    ]
)
