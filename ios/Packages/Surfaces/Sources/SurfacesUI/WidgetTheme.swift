import CalendarUI
import SwiftUI
import UIKit

/// Widget colours that are not on the app's `Theme` (spec "Colours (rev 2)": the widget reads like Home, one bright
/// block, everything else neutral). The container follows the system appearance (DECISIONS Q8).
public enum WidgetTheme {
    private static func dynamic(light: UInt32, dark: UInt32, lightAlpha: Double = 1, darkAlpha: Double = 1) -> Color {
        Color(uiColor: UIColor { t in
            let hex = t.userInterfaceStyle == .dark ? dark : light
            let a = t.userInterfaceStyle == .dark ? darkAlpha : lightAlpha
            return UIColor(red: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255,
                           blue: CGFloat(hex & 0xFF) / 255, alpha: a)
        })
    }

    /// `wgbg`: the widget container.
    public static var container: Color { dynamic(light: 0xFBFBF9, dark: 0x141416) }
    /// `wgev`: event fill (Day timeline).
    public static var event: Color { dynamic(light: 0xEFEFEB, dark: 0x202024) }
    /// `wl`: hairlines, progress track, skeleton blocks.
    public static var line: Color { dynamic(light: 0x000000, dark: 0xFFFFFF, lightAlpha: 0.10, darkAlpha: 0.11) }
    /// Amber `#F0B84A` in both themes: only for "needs you" (OVERLAP, REPLY, sign in), always as a filled shape.
    public static let amber = Color(red: 240 / 255, green: 184 / 255, blue: 74 / 255)
    /// Ink on the bright block and on amber.
    public static let blockInk = Color(red: 10 / 255, green: 10 / 255, blue: 11 / 255)
    /// Text on the black Join pill.
    public static let joinInk = Color(red: 244 / 255, green: 241 / 255, blue: 234 / 255)
}

extension View {
    /// Mono caps label (10 pt, +.14em).
    func widgetCaps(_ size: CGFloat = 10, tracking: CGFloat = 1.4) -> some View {
        font(CalFont.font(.mono, size: size, relativeTo: .caption2)).tracking(tracking).textCase(.uppercase)
    }
}
