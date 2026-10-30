import SwiftUI

/// Split-flap cells, the canvas' signature element (`.fl`). Every character, colon included, gets its own cell:
/// mono Medium glyph on a dark cell with a 2pt split line across the middle and a 2pt gap between cells.
/// Used for the Next-block start time, time pickers and the Welcome / Paired wordmarks.
public struct FlipCells: View {
    public enum Size: Sendable {
        case small, regular, xl

        var fontSize: CGFloat { switch self { case .small: 18; case .regular: 26; case .xl: 44 } }
        var height: CGFloat { switch self { case .small: 30; case .regular: 44; case .xl: 60 } }
        /// DM Mono advance is 0.6 em. Every cell face is the advance rounded up plus 1 pt of air each side, so a glyph
        /// (zero, colon, any digit) never touches or crosses its container; the 2 pt gap comes on top.
        var faceWidth: CGFloat { (fontSize * 0.6).rounded(.up) + 2 }
        /// Face plus gap: the distance from one cell to the next.
        var pitch: CGFloat { faceWidth + FlipCells.gap }
    }

    static let gap: CGFloat = 2

    /// Width of `text` drawn in cells. Always `pitch * count - gap`, so layouts can reserve it up front.
    public static func width(_ text: String, size: Size = .regular) -> CGFloat {
        guard !text.isEmpty else { return 0 }
        return size.pitch * CGFloat(text.count) - gap
    }

    let text: String
    let size: Size
    let onBlock: Bool
    let dimmed: Bool

    /// `onBlock`: inside the Next block the cell face is `#1B1B1E` instead of `#26262A`.
    public init(_ text: String, size: Size = .regular, onBlock: Bool = false, dimmed: Bool = false) {
        self.text = text
        self.size = size
        self.onBlock = onBlock
        self.dimmed = dimmed
    }

    public var body: some View {
        HStack(spacing: FlipCells.gap) {
            ForEach(Array(text.enumerated()), id: \.offset) { _, ch in cell(ch) }
        }
        .opacity(dimmed ? 0.35 : 1)
        .fixedSize()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(text)
    }

    private func cell(_ ch: Character) -> some View {
        ZStack {
            Rectangle().fill((onBlock ? Theme.flipCellOnBlock : Theme.flipCell).color)
            Rectangle().fill(Theme.flipSplit.color).frame(height: 2)
            Text(String(ch)).font(CalFont.flip(size: size.fontSize)).foregroundStyle(Theme.flipInk.color)
                .lineLimit(1).fixedSize()
        }
        .frame(width: size.faceWidth, height: size.height)
        .clipped()
    }
}
