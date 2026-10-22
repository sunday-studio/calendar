import SwiftUI

/// Split-flap cells, the canvas' signature element (`.fl`). Every character, colon included, gets its own cell:
/// mono Medium glyph on a dark cell with a 2pt split line across the middle and a 2pt gap between cells.
/// Used for the Next-block start time, time pickers and the Welcome / Paired wordmarks.
public struct FlipCells: View {
    public enum Size: Sendable {
        case small, regular, xl

        var fontSize: CGFloat { switch self { case .small: 18; case .regular: 26; case .xl: 44 } }
        var height: CGFloat { switch self { case .small: 30; case .regular: 44; case .xl: 60 } }
        /// DM Mono advance is 0.6 em; the canvas uses `1ch` per cell, gap included.
        var cellWidth: CGFloat { (fontSize * 0.6).rounded(.up) }
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
        HStack(spacing: 0) {
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
        }
        .frame(width: size.cellWidth - 2, height: size.height)
        .padding(.trailing, 2)
    }
}
