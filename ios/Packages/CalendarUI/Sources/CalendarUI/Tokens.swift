import SwiftUI
import UIKit

/// Design tokens, hand-written from the Gnomon canvas colour system (LIGHT / DARK blocks).
/// Keep values in sync with that file; DECISIONS D (T03).

/// A colour with a light and a dark value, resolved dynamically.
public struct TokenColor: Sendable, Equatable {
    public let light: UInt32
    public let dark: UInt32
    public init(light: UInt32, dark: UInt32) {
        self.light = light
        self.dark = dark
    }

    public func hex(for style: UIUserInterfaceStyle) -> UInt32 {
        style == .dark ? dark : light
    }

    public var uiColor: UIColor {
        let l = light, d = dark
        return UIColor { trait in UIColor(hex: trait.userInterfaceStyle == .dark ? d : l) }
    }

    public var color: Color { Color(uiColor: uiColor) }

    /// Resolve for a given style (tests, non-SwiftUI call sites).
    public func resolved(_ style: UIUserInterfaceStyle) -> UIColor {
        uiColor.resolvedColor(with: UITraitCollection(userInterfaceStyle: style))
    }
}

extension UIColor {
    convenience init(hex: UInt32, alpha: CGFloat = 1) {
        self.init(
            red: CGFloat((hex >> 16) & 0xFF) / 255,
            green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255,
            alpha: alpha
        )
    }
}

/// Semantic colours. Use these, never raw hex in views.
/// Source: the Gnomon canvas (`gnomon.css` `.dk` / `.lt`). One accent: amber. Neutral near-black / off-white ground.
public enum Theme {
    public static let bg = TokenColor(light: 0xF1F1EE, dark: 0x0A0A0B)
    /// Cards, inputs, banners, sheets (canvas `p1`).
    public static let surface = TokenColor(light: 0xFFFFFF, dark: 0x141416)
    /// Skeleton bars, range fill (canvas `p2`).
    public static let sunk = TokenColor(light: 0xF7F7F5, dark: 0x1B1B1E)
    /// Row hairlines (canvas `line`).
    public static let line = TokenColor(light: 0xDEDEDA, dark: 0x1D1D20)
    /// Outline of inputs, outline buttons, segments (canvas `pl`).
    public static let border = TokenColor(light: 0xDCDCD6, dark: 0x2A2A2D)
    /// Boundary of interactive controls (inputs, switches, checkboxes, segments, outline buttons): >= 3:1 on `bg`
    /// (WCAG 1.4.11). The canvas draws these with `border` (1.2-1.4:1); DECISIONS Q10 adopts this stronger value.
    public static let controlBorder = TokenColor(light: 0x8A8A84, dark: 0x6F6E69)
    public static let ink = TokenColor(light: 0x0A0A0B, dark: 0xF4F1EA)
    public static let ink2 = TokenColor(light: 0x6B6B66, dark: 0x8B8A84)
    /// Placeholder, other-month days (canvas `faint`).
    public static let ink3 = TokenColor(light: 0xB4B4AD, dark: 0x4D4D4A)
    /// Cancelled / declined text (canvas `strike`).
    public static let strike = TokenColor(light: 0x9A9A94, dark: 0x6B6A65)

    /// Amber: the one accent. Now line, status words, links, banner bar, today, progress.
    public static let accent = TokenColor(light: 0x8A5A00, dark: 0xF0B84A)
    /// Same token as `accent` (text and fills share one amber in the canvas).
    public static let accentInk = accent
    /// "Needs you". Same amber.
    public static let amber = accent
    /// Retired tints: the canvas has no tinted fills. Kept as aliases so callers compile; do not use in new code.
    public static let amberTint = surface
    public static let redTint = surface
    public static let greenTint = surface

    public static let red = TokenColor(light: 0xB3261E, dark: 0xFF8F8F)
    /// OK status square / check: the moss account colour (canvas `a3`).
    public static let green = TokenColor(light: 0x6A8A2E, dark: 0xA3C25E)

    /// Toast: ink fill, bg text.
    public static let inverse = ink
    public static let inverseInk = bg
    public static let inverseDim = ink2

    /// Ink drawn on a bright account-colour block. The block always uses the bright value, so this is constant.
    public static let onAccountColor = TokenColor(light: 0x0A0A0B, dark: 0x0A0A0B)
    /// Ink used on amber fills (now-line label).
    public static let onBrass = TokenColor(light: 0x0A0A0B, dark: 0x0A0A0B)
    /// Text on amber fills: light amber (#8A5A00) needs a light ink (5.3:1); canvas draws #0A0A0B (3.3:1). DECISIONS Q10.
    public static let onAmber = TokenColor(light: 0xF1F1EE, dark: 0x0A0A0B)

    public static let scrim = TokenColor(light: 0x0A0A0B, dark: 0x000000)
    public static let scrimAlpha: (light: Double, dark: Double) = (0.34, 0.62)
    /// Past-time hatch ink (canvas `.past` uses `line`).
    public static let shadeInk = line
    public static let shadeAlpha: (light: Double, dark: Double) = (0.8, 0.8)

    /// Flip-cell faces (canvas `.fl`): cell, cell inside the Next block, split line, glyph.
    public static let flipCell = TokenColor(light: 0x26262A, dark: 0x26262A)
    public static let flipCellOnBlock = TokenColor(light: 0x1B1B1E, dark: 0x1B1B1E)
    public static let flipSplit = TokenColor(light: 0x050505, dark: 0x050505)
    public static let flipInk = TokenColor(light: 0xF4F1EA, dark: 0xF4F1EA)
}

public extension Color {
    static var calBg: Color { Theme.bg.color }
    static var calSurface: Color { Theme.surface.color }
    static var calSunk: Color { Theme.sunk.color }
    static var calLine: Color { Theme.line.color }
    static var calBorder: Color { Theme.border.color }
    static var calControl: Color { Theme.controlBorder.color }
    static var calStrike: Color { Theme.strike.color }
    static var calInk: Color { Theme.ink.color }
    static var calInk2: Color { Theme.ink2.color }
    static var calInk3: Color { Theme.ink3.color }
    static var calAccent: Color { Theme.accent.color }
    static var calAccentInk: Color { Theme.accentInk.color }
    static var calAmber: Color { Theme.amber.color }
    static var calAmberTint: Color { Theme.amberTint.color }
    static var calRed: Color { Theme.red.color }
    static var calRedTint: Color { Theme.redTint.color }
    static var calGreen: Color { Theme.green.color }
    static var calGreenTint: Color { Theme.greenTint.color }
}

/// The eight account colours (design: "cobalt, rose, ochre, mauve, verdigris, slate, moss, clay").
public enum AccountColor: String, CaseIterable, Sendable, Identifiable {
    case cobalt, rose, ochre, mauve, verdigris, slate, moss, clay

    public var id: String { rawValue }

    public var token: TokenColor {
        switch self {
        case .cobalt: TokenColor(light: 0x3E63DD, dark: 0x7C98F2)
        case .rose: TokenColor(light: 0xC9507F, dark: 0xEE86AC)
        case .ochre: TokenColor(light: 0xB9801A, dark: 0xE0A94A)
        case .mauve: TokenColor(light: 0x8458B3, dark: 0xB28CE0)
        case .verdigris: TokenColor(light: 0x1F8A74, dark: 0x4FC4AB)
        case .slate: TokenColor(light: 0x5F7284, dark: 0x8FA3B5)
        case .moss: TokenColor(light: 0x6A8A2E, dark: 0xA3C25E)
        case .clay: TokenColor(light: 0xC4553A, dark: 0xF0866B)
        }
    }

    public var color: Color { token.color }

    /// The bright (dark-set) value, used by the Next block in both themes (canvas `a1b/a2b/a3b`).
    public var bright: Color { Color(uiColor: UIColor(hex: token.dark)) }

    /// Map an arbitrary key (from the hub, e.g. an account's `colorKey`) to a palette entry.
    public init(key: String?) {
        self = AccountColor(rawValue: (key ?? "").lowercased()) ?? .slate
    }
}

/// 4-pt spacing scale. `gutter` is the screen edge inset used by every full-width row.
public enum Spacing {
    public static let xxs: CGFloat = 2
    public static let xs: CGFloat = 4
    public static let s: CGFloat = 8
    public static let m: CGFloat = 12
    public static let l: CGFloat = 16
    public static let gutter: CGFloat = 24
    public static let xl: CGFloat = 24
    public static let xxl: CGFloat = 32
    /// Minimum hit target.
    public static let hit: CGFloat = 44
    public static let timeColumn: CGFloat = 48
    public static let colorBar: CGFloat = 3
    /// Canvas sizes: list row, two-line row, account row, button, field, nav bar, FAB, Today button.
    public static let rowHeight: CGFloat = 58
    public static let rowHeightTall: CGFloat = 72
    public static let accountRowHeight: CGFloat = 84
    public static let buttonHeight: CGFloat = 52
    public static let fieldHeight: CGFloat = 52
    public static let navHeight: CGFloat = 56
    public static let fab: CGFloat = 52
    public static let todayButton: CGFloat = 38
}
