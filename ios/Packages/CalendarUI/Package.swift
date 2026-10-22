// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "CalendarUI",
    platforms: [.iOS("26.0")],
    products: [.library(name: "CalendarUI", targets: ["CalendarUI"])],
    targets: [.target(name: "CalendarUI")]
)
