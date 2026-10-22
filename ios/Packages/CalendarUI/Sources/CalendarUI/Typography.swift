import SwiftUI

/// Type system from the Gnomon canvas: one sans (Regular, Medium at most) plus mono for times and labels.
/// The canvas is set in GT America / GT America Mono. Until licensed files are bundled (DECISIONS Q4) the
/// stand-ins are Schibsted Grotesk and DM Mono (OFL). To swap, change the PostScript names in `Face`.
/// No serif, no italics anywhere. "Nothing heavier than Medium. Size carries the hierarchy."
public enum CalFont {
    public enum Face: CaseIterable, Sendable {
        case sans, sansMedium, mono, monoMedium

        /// PostScript name (the variable files expose these named instances).
        public var postScriptName: String {
            switch self {
            case .sans: "SchibstedGrotesk-Regular"
            case .sansMedium: "SchibstedGrotesk-Regular_Medium"
            case .mono: "DMMono-Regular"
            case .monoMedium: "DMMono-Medium"
            }
        }
    }

    public static func font(_ face: Face, size: CGFloat, relativeTo style: Font.TextStyle) -> Font {
        .custom(face.postScriptName, size: size, relativeTo: style)
    }

    // Canvas scale (pt).
    /// Next-event title: 30 Medium.
    public static let nextTitle = font(.sansMedium, size: 30, relativeTo: .largeTitle)
    /// Pairing / welcome heading: 30 Medium (welcome uses `welcome`).
    public static let display = font(.sansMedium, size: 30, relativeTo: .largeTitle)
    public static let welcome = font(.sansMedium, size: 36, relativeTo: .largeTitle)
    /// Screen title ("October 2026"): 22 Medium.
    public static let title = font(.sansMedium, size: 22, relativeTo: .title2)
    /// Nav bar title: 18 Medium.
    public static let navTitle = font(.sansMedium, size: 18, relativeTo: .headline)
    /// List title: 19 Regular.
    public static let heading = font(.sans, size: 19, relativeTo: .title3)
    /// Body: 17 Regular. Buttons: 17 Medium.
    public static let body = font(.sans, size: 17, relativeTo: .body)
    public static let bodyMedium = font(.sansMedium, size: 17, relativeTo: .body)
    /// Status word: 15 Regular (amber, or dim).
    public static let status = font(.sans, size: 15, relativeTo: .subheadline)
    /// Second line of a row and hints: 14 / 13.
    public static let sub = font(.sans, size: 14, relativeTo: .footnote)
    public static let hint = font(.sans, size: 13, relativeTo: .footnote)
    /// Uppercase mono label, 11 (apply `.calLabel()` for tracking + uppercase).
    public static let label = font(.mono, size: 11, relativeTo: .caption2)
    /// Times: mono 15. Tabular by construction.
    public static let time = font(.mono, size: 15, relativeTo: .subheadline).monospacedDigit()
    public static let timeMedium = font(.monoMedium, size: 15, relativeTo: .subheadline).monospacedDigit()
    public static let timeSmall = font(.mono, size: 11, relativeTo: .caption2).monospacedDigit()
    /// Next-block footer ("in 8 min"): 18 Regular.
    public static let footer = font(.sans, size: 18, relativeTo: .body)
    /// Join / Map button inside the Next block: 16 Regular.
    public static let blockButton = font(.sans, size: 16, relativeTo: .body)
    /// Flip-cell glyphs: Mono Medium. Sizes 18 / 26 / 44 (cell heights 30 / 44 / 60).
    public static func flip(size: CGFloat) -> Font { .custom(Face.monoMedium.postScriptName, fixedSize: size) }
}

public extension View {
    /// Uppercase tracked mono label: 11pt, +0.14em, dim. Section labels, field labels, key/value keys.
    func calLabel(_ color: Color = .calInk2) -> some View {
        font(CalFont.label)
            .tracking(1.54)
            .textCase(.uppercase)
            .foregroundStyle(color)
    }
}
