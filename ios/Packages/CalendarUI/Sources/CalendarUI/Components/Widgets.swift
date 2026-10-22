import SwiftUI

/// Chip shown while dragging or resizing (canvas `.chip`): ink fill, bg text, mono 12.
public struct DragChip: View {
    let text: String
    public init(_ text: String) { self.text = text }
    public var body: some View {
        Text(text).font(CalFont.font(.mono, size: 12, relativeTo: .caption)).monospacedDigit()
            .foregroundStyle(Color.calBg)
            .padding(.horizontal, 10).padding(.vertical, 5)
            .background(Color.calInk)
            .accessibilityLabel(text)
    }
}

/// Small widget, next event (canvas w-home): flip cells, bar, title 19 Medium, amber relative line.
public struct WidgetNextSmall: View {
    let time: String
    let title: String
    let relative: String
    let color: AccountColor

    public init(time: String, title: String, relative: String, color: AccountColor) {
        self.time = time; self.title = title; self.relative = relative; self.color = color
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            FlipCells(time, size: .small)
            Text(title).font(CalFont.font(.sansMedium, size: 19, relativeTo: .headline)).foregroundStyle(Color.calInk)
                .lineSpacing(0).lineLimit(2).padding(.top, 12)
            Text(relative).font(CalFont.font(.sans, size: 14, relativeTo: .footnote)).foregroundStyle(Color.calAmber).padding(.top, 6)
            Spacer(minLength: 0)
        }
        .padding(14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .overlay(alignment: .topLeading) { Rectangle().fill(color.color).frame(width: 3, height: 30).padding(.top, 14) }
        .accessibilityElement(children: .combine)
    }
}

/// Widget state card (canvas w-states): title and one line; the line is amber when it needs attention.
public struct WidgetStateCard: View {
    let title: String
    let line: String
    let needsAttention: Bool
    public init(title: String, line: String, needsAttention: Bool = false) {
        self.title = title; self.line = line; self.needsAttention = needsAttention
    }
    public var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(CalFont.font(.sansMedium, size: 19, relativeTo: .headline)).foregroundStyle(Color.calInk)
            Text(line).font(CalFont.font(.sans, size: 14, relativeTo: .footnote))
                .foregroundStyle(needsAttention ? Color.calAmber : Color.calInk2)
        }
        .padding(14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityElement(children: .combine)
    }
}

