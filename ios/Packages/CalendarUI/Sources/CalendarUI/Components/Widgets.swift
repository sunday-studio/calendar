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

/// One row of the medium widget (h44): bar, mono 13 time, 16pt title, 13pt status.
public struct WidgetRow: Identifiable, Sendable {
    public let id: String
    public let time: String
    public let title: String
    public let status: String?
    public let color: AccountColor
    public init(id: String = UUID().uuidString, time: String, title: String, status: String? = nil, color: AccountColor) {
        self.id = id; self.time = time; self.title = title; self.status = status; self.color = color
    }
}

/// Medium widget, today (canvas w-home): "TODAY" / "3 LEFT" header and up to three rows.
public struct WidgetTodayMedium: View {
    let remaining: Int
    let rows: [WidgetRow]
    public init(remaining: Int, rows: [WidgetRow]) { self.remaining = remaining; self.rows = Array(rows.prefix(3)) }

    public var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text("Today").calLabel()
                Spacer()
                Text("\(remaining) left").calLabel()
            }
            VStack(spacing: 0) {
                ForEach(Array(rows.enumerated()), id: \.element.id) { i, r in
                    HStack(spacing: 12) {
                        Text(r.time).font(CalFont.font(.mono, size: 13, relativeTo: .footnote)).monospacedDigit()
                            .foregroundStyle(Color.calInk2).frame(width: 42, alignment: .leading)
                        Text(r.title).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk).lineLimit(1)
                        Spacer(minLength: 4)
                        if let s = r.status { Text(s).font(CalFont.hint).foregroundStyle(Color.calAmber) }
                    }
                    .padding(.leading, 14)
                    .frame(height: 44)
                    .overlay(alignment: .leading) { Rectangle().fill(r.color.color).frame(width: 3).padding(.vertical, 9) }
                    .overlay(alignment: .bottom) { if i < rows.count - 1 { Rectangle().fill(Color.calLine).frame(height: 1) } }
                }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
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

/// Lock-screen Live Activity (canvas w-live): bright account block with flip cells, title, Join, 3pt progress line.
public struct LiveActivityBlock: View {
    let time: String
    let title: String
    let footer: String
    let color: AccountColor
    let progress: Double
    let joinTitle: String?

    public init(time: String, title: String, footer: String, color: AccountColor, progress: Double, joinTitle: String? = "Join") {
        self.time = time; self.title = title; self.footer = footer; self.color = color
        self.progress = min(max(progress, 0), 1); self.joinTitle = joinTitle
    }

    public var body: some View {
        VStack(spacing: 0) {
            HStack(alignment: .top, spacing: 14) {
                FlipCells(time, size: .small, onBlock: true)
                VStack(alignment: .leading, spacing: 4) {
                    Text(title).font(CalFont.font(.sansMedium, size: 20, relativeTo: .headline)).lineLimit(2)
                    Text(footer).font(CalFont.font(.sans, size: 14, relativeTo: .footnote))
                }
                .foregroundStyle(Theme.onAccountColor.color)
                Spacer(minLength: 0)
                if let joinTitle {
                    Text(joinTitle).font(CalFont.font(.sans, size: 15, relativeTo: .body)).foregroundStyle(Theme.flipInk.color)
                        .padding(.horizontal, 18).padding(.vertical, 8)
                        .background(Theme.onAccountColor.color)
                }
            }
            .padding(16)
            .background(color.bright)
            GeometryReader { geo in
                Rectangle().fill(Color(uiColor: UIColor(hex: 0x222225)))
                    .overlay(alignment: .leading) { Rectangle().fill(Color.calAccent).frame(width: geo.size.width * progress) }
            }
            .frame(height: 3)
        }
        .overlay(Rectangle().strokeBorder(Color(uiColor: UIColor(hex: 0x232326)), lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}
