import CalendarUI
import UIKit
import XCTest

final class DesignSystemTests: XCTestCase {
    private func rgb(_ c: UIColor) -> [Int] {
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        c.getRed(&r, green: &g, blue: &b, alpha: &a)
        return [r, g, b].map { Int(($0 * 255).rounded()) }
    }

    /// Values from the Gnomon canvas `gnomon.css` (`.dk` / `.lt`).
    func testSemanticColoursResolveForLightAndDark() {
        let cases: [(String, TokenColor, [Int], [Int])] = [
            ("bg", Theme.bg, [0xF1, 0xF1, 0xEE], [0x0A, 0x0A, 0x0B]),
            ("surface", Theme.surface, [0xFF, 0xFF, 0xFF], [0x14, 0x14, 0x16]),
            ("sunk", Theme.sunk, [0xF7, 0xF7, 0xF5], [0x1B, 0x1B, 0x1E]),
            ("line", Theme.line, [0xDE, 0xDE, 0xDA], [0x1D, 0x1D, 0x20]),
            ("border", Theme.border, [0xDC, 0xDC, 0xD6], [0x2A, 0x2A, 0x2D]),
            ("ink", Theme.ink, [0x0A, 0x0A, 0x0B], [0xF4, 0xF1, 0xEA]),
            ("ink2", Theme.ink2, [0x6B, 0x6B, 0x66], [0x8B, 0x8A, 0x84]),
            ("ink3", Theme.ink3, [0xB4, 0xB4, 0xAD], [0x4D, 0x4D, 0x4A]),
            ("amber", Theme.accent, [0x8A, 0x5A, 0x00], [0xF0, 0xB8, 0x4A]),
            ("strike", Theme.strike, [0x9A, 0x9A, 0x94], [0x6B, 0x6A, 0x65]),
            ("err", Theme.red, [0xB3, 0x26, 0x1E], [0xFF, 0x8F, 0x8F]),
        ]
        for (name, token, light, dark) in cases {
            XCTAssertEqual(rgb(token.resolved(.light)), light, "\(name) light")
            XCTAssertEqual(rgb(token.resolved(.dark)), dark, "\(name) dark")
        }
        // One amber for text and fills.
        XCTAssertEqual(Theme.accent, Theme.accentInk)
        XCTAssertEqual(Theme.accent, Theme.amber)
    }

    /// WCAG 1.4.11: control boundaries need >= 3:1 against the page background.
    func testControlBorderContrast() {
        func lum(_ c: UIColor) -> Double {
            let v = rgb(c).map { Double($0) / 255 }.map { $0 <= 0.03928 ? $0 / 12.92 : pow(($0 + 0.055) / 1.055, 2.4) }
            return 0.2126 * v[0] + 0.7152 * v[1] + 0.0722 * v[2]
        }
        func ratio(_ a: UIColor, _ b: UIColor) -> Double {
            let (x, y) = (lum(a), lum(b)); return (max(x, y) + 0.05) / (min(x, y) + 0.05)
        }
        for style in [UIUserInterfaceStyle.light, .dark] {
            XCTAssertGreaterThanOrEqual(ratio(Theme.controlBorder.resolved(style), Theme.bg.resolved(style)), 3.0, "\(style)")
        }
    }

    func testNextBlockUsesBrightColourAndInkInBothThemes() {
        // The block always draws the dark-set value with ink #0A0A0B text (canvas a1b/a2b/a3b).
        XCTAssertEqual(Theme.onAccountColor.light, 0x0A0A0B)
        XCTAssertEqual(Theme.onAccountColor.dark, 0x0A0A0B)
        XCTAssertEqual(AccountColor.rose.token.dark, 0xEE86AC)
    }

    func testTimelineLanesAndGeometry() {
        let a = TimelineItem(id: "a", title: "Roadmap", startMinute: 630, endMinute: 690, color: .rose)
        let b = TimelineItem(id: "b", title: "Dentist", startMinute: 660, endMinute: 720, color: .cobalt)
        let c = TimelineItem(id: "c", title: "1:1", startMinute: 900, endMinute: 945, color: .rose)
        let lanes = TimelineLayout.lanes(for: [b, a, c])
        XCTAssertEqual(lanes["a"], .init(index: 0, count: 2))
        XCTAssertEqual(lanes["b"], .init(index: 1, count: 2))
        XCTAssertEqual(lanes["c"], .init(index: 0, count: 1))
        // Canvas v-day: grid from 09:00, 64pt/h: Roadmap 10:30-11:30 -> top 98, height 62; Standup 09:00-09:30 -> top 2, height 30.
        let f = TimelineLayout.frame(for: a, firstHour: 9)
        XCTAssertEqual(f.top, 98); XCTAssertEqual(f.height, 62)
        let s = TimelineLayout.frame(for: .init(id: "s", title: "Standup", startMinute: 540, endMinute: 570, color: .rose), firstHour: 9)
        XCTAssertEqual(s.top, 2); XCTAssertEqual(s.height, 30)
    }

    func testStatusWords() {
        func word(_ s: EventRowModel.Status, _ d: String? = nil) -> String? {
            EventRowModel(title: "x", start: "10:00", color: .cobalt, status: s, statusDetail: d).statusWord
        }
        XCTAssertEqual(word(.moved, "11:45"), "Moved 11:45")
        XCTAssertEqual(word(.needsReply), "Reply needed")
        XCTAssertEqual(word(.maybe), "Maybe")
        XCTAssertEqual(word(.declined), "Declined")
        XCTAssertEqual(word(.cancelled), "Cancelled")
        XCTAssertEqual(word(.stale), "Stale")
        XCTAssertNil(EventRowModel(title: "x", start: "10:00", color: .cobalt).statusWord)
        XCTAssertEqual(EventRowModel(title: "x", start: "10:00", color: .cobalt, overlapWith: "Roadmap").overlapLine, "Overlaps with Roadmap")
    }

    func testAccountPaletteResolvesPerStyle() {
        XCTAssertEqual(AccountColor.allCases.count, 8)
        XCTAssertEqual(rgb(AccountColor.cobalt.token.resolved(.light)), [0x3E, 0x63, 0xDD])
        XCTAssertEqual(rgb(AccountColor.cobalt.token.resolved(.dark)), [0x7C, 0x98, 0xF2])
        for c in AccountColor.allCases { XCTAssertNotEqual(c.token.light, c.token.dark, "\(c)") }
        XCTAssertEqual(AccountColor(key: "Rose"), .rose)
        XCTAssertEqual(AccountColor(key: "nope"), .slate)
    }

    func testFontsAreRegistered() {
        for face in CalFont.Face.allCases {
            XCTAssertNotNil(UIFont(name: face.postScriptName, size: 16), "missing font \(face.postScriptName)")
        }
    }

    func testEventRowAccessibilityLabel() {
        XCTAssertEqual(
            EventRowFormatter.accessibilityLabel(title: "Design review", start: "10:00", end: "10:45", location: "Room 4",
                                                 status: "moved", isAllDay: false, overlaps: true),
            "Design review, 10:00 to 10:45, Room 4, moved, overlaps another event")
        XCTAssertEqual(
            EventRowFormatter.accessibilityLabel(title: "Offsite", start: "", end: nil, location: nil,
                                                 status: nil, isAllDay: true, overlaps: false),
            "Offsite, all day")
        XCTAssertEqual(
            EventRowFormatter.accessibilityLabel(title: "Call", start: "15:00", end: nil, location: "", status: nil,
                                                 isAllDay: false, overlaps: false, isPast: true),
            "Call, 15:00, finished")
    }
}
