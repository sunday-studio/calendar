import SwiftUI

/// One event on a timeline column. Minutes are counted from midnight in the viewing time zone.
public struct TimelineItem: Identifiable, Sendable, Equatable {
    public enum State: Sendable { case normal, declined, tentative, lifted, ghost }

    public let id: String
    public var title: String
    public var subtitle: String?
    public var startMinute: Int
    public var endMinute: Int
    public var color: AccountColor
    public var state: State

    public init(id: String, title: String, subtitle: String? = nil, startMinute: Int, endMinute: Int,
                color: AccountColor, state: State = .normal) {
        self.id = id; self.title = title; self.subtitle = subtitle
        self.startMinute = startMinute; self.endMinute = max(endMinute, startMinute + 1)
        self.color = color; self.state = state
    }
}

/// Lane assignment: overlapping events share the column width side by side (canvas v-day: two lanes = 152 + 6 + 152).
public enum TimelineLayout {
    public struct Lane: Equatable, Sendable {
        public var index: Int
        public var count: Int
        public init(index: Int, count: Int) { self.index = index; self.count = count }
    }

    public static func lanes(for items: [TimelineItem]) -> [String: Lane] {
        let sorted = items.sorted { ($0.startMinute, $0.endMinute) < ($1.startMinute, $1.endMinute) }
        var result: [String: Lane] = [:]
        var cluster: [(id: String, lane: Int)] = []
        var laneEnds: [Int] = []
        var clusterEnd = Int.min

        func flush() {
            let count = max(1, (cluster.map(\.lane).max() ?? 0) + 1)
            for c in cluster { result[c.id] = Lane(index: c.lane, count: count) }
            cluster = []; laneEnds = []; clusterEnd = Int.min
        }

        for item in sorted {
            if item.startMinute >= clusterEnd { flush() }
            if let free = laneEnds.firstIndex(where: { $0 <= item.startMinute }) {
                laneEnds[free] = item.endMinute
                cluster.append((item.id, free))
            } else {
                laneEnds.append(item.endMinute)
                cluster.append((item.id, laneEnds.count - 1))
            }
            clusterEnd = max(clusterEnd, item.endMinute)
        }
        flush()
        return result
    }

    /// Canvas geometry: `top = (start - gridStart) * hourHeight / 60 + 2`, `height = duration * hourHeight / 60 - 2`, min 30.
    public static func frame(for item: TimelineItem, firstHour: Int, hourHeight: CGFloat = 64) -> (top: CGFloat, height: CGFloat) {
        let top = CGFloat(item.startMinute - firstHour * 60) * hourHeight / 60 + 2
        let h = max(30, CGFloat(item.endMinute - item.startMinute) * hourHeight / 60 - 2)
        return (top, h)
    }
}

/// One event block (canvas `.ev`): surface fill, 3pt account-colour left border, 14pt title, 12pt dim second line.
public struct TimelineEventBlock: View {
    let item: TimelineItem
    let height: CGFloat
    let singleLine: Bool

    /// `singleLine`: 3-day columns: 14pt title on one line with a tail ellipsis,
    /// no second line (canvas v-three shows "Roadmap review" truncated, never wrapped mid-word).
    public init(_ item: TimelineItem, height: CGFloat, singleLine: Bool = false) {
        self.item = item; self.height = height; self.singleLine = singleLine
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(item.title).font(CalFont.font(.sans, size: 14, relativeTo: .footnote))
                .foregroundStyle(Color.calInk)
                .strikethrough(item.state == .declined)
                .lineLimit(height < 46 || singleLine ? 1 : 2)
                .truncationMode(.tail)
            if let sub = item.subtitle, !singleLine, height >= 46 {
                Text(sub).font(CalFont.font(.sans, size: 12, relativeTo: .caption)).foregroundStyle(Color.calInk2).lineLimit(1)
            }
        }
        .padding(.horizontal, 10).padding(.vertical, 6)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background {
            switch item.state {
            case .tentative: ZStack { Color.calSurface; HatchedShade(spacing: 6, lineWidth: 1) }
            case .ghost: Color.clear
            default: Color.calSurface
            }
        }
        .overlay(alignment: .leading) { if item.state != .ghost { Rectangle().fill(item.color.color).frame(width: Spacing.colorBar) } }
        .overlay {
            if item.state == .ghost { Rectangle().strokeBorder(Color.calBorder, style: StrokeStyle(lineWidth: 1, dash: [4, 3])) }
            if item.state == .lifted { Rectangle().strokeBorder(Color.calInk, lineWidth: 1) }
        }
        .opacity(item.state == .declined ? 0.5 : 1)
        .frame(height: height)
        .clipped()
        .accessibilityElement(children: .combine)
    }
}

/// Hour rows (canvas `.hr`): 64pt pitch, 1pt hairline, mono 11 dim label on a bg plate at x=`labelX`.
public struct HourGrid: View {
    let firstHour: Int
    let hours: Int
    let hourHeight: CGFloat
    let labelX: CGFloat
    let twoDigit: Bool
    /// Optional second zone: hour offset (e.g. -4) and its label x (canvas 62), drawn in `faint`.
    let secondary: (offset: Int, x: CGFloat)?

    public init(firstHour: Int = 9, hours: Int = 11, hourHeight: CGFloat = 64, labelX: CGFloat = 16,
                twoDigit: Bool = false, secondary: (offset: Int, x: CGFloat)? = nil) {
        self.firstHour = firstHour; self.hours = hours; self.hourHeight = hourHeight
        self.labelX = labelX; self.twoDigit = twoDigit; self.secondary = secondary
    }

    private func label(_ h: Int) -> String {
        let hh = ((h % 24) + 24) % 24
        return twoDigit ? String(format: "%02d", hh) : String(format: "%02d:00", hh)
    }

    public var body: some View {
        ZStack(alignment: .topLeading) {
            ForEach(0..<hours, id: \.self) { i in
                Rectangle().fill(Color.calLine).frame(height: 1).offset(y: CGFloat(i) * hourHeight)
                plate(label(firstHour + i), color: .calInk2, x: labelX).offset(y: CGFloat(i) * hourHeight - 9)
                if let s = secondary {
                    plate(label(firstHour + i + s.offset), color: .calInk3, x: s.x).offset(y: CGFloat(i) * hourHeight - 9)
                }
            }
        }
        .frame(maxWidth: .infinity, minHeight: CGFloat(hours) * hourHeight, alignment: .topLeading)
        .accessibilityHidden(true)
    }

    private func plate(_ text: String, color: Color, x: CGFloat) -> some View {
        Text(text).font(CalFont.label).foregroundStyle(color)
            .padding(.horizontal, 4).background(Color.calBg)
            .offset(x: x)
    }
}

/// A single timeline column of events with lane layout. `now` (minutes from midnight) draws the hatch and the amber rule
/// for today's column; pass nil for other days.
public struct TimelineColumn: View {
    let items: [TimelineItem]
    let firstHour: Int
    let hours: Int
    let hourHeight: CGFloat
    let now: Int?
    let nowLabel: String?
    let laneGap: CGFloat
    let singleLine: Bool

    public init(_ items: [TimelineItem], firstHour: Int = 9, hours: Int = 11, hourHeight: CGFloat = 64,
                now: Int? = nil, nowLabel: String? = nil, laneGap: CGFloat = 6,
                singleLine: Bool = false) {
        self.items = items; self.firstHour = firstHour; self.hours = hours; self.hourHeight = hourHeight
        self.now = now; self.nowLabel = nowLabel; self.laneGap = laneGap; self.singleLine = singleLine
    }

    public var body: some View {
        GeometryReader { geo in
            let lanes = TimelineLayout.lanes(for: items)
            ZStack(alignment: .topLeading) {
                if let now {
                    let y = CGFloat(now - firstHour * 60) * hourHeight / 60
                    if y > 0 { HatchedShade().frame(height: min(y, CGFloat(hours) * hourHeight)) }
                }
                ForEach(items) { item in
                    let f = TimelineLayout.frame(for: item, firstHour: firstHour, hourHeight: hourHeight)
                    let lane = lanes[item.id] ?? .init(index: 0, count: 1)
                    let w = (geo.size.width - laneGap * CGFloat(lane.count - 1)) / CGFloat(lane.count)
                    TimelineEventBlock(item, height: f.height, singleLine: singleLine)
                        .frame(width: w)
                        .offset(x: CGFloat(lane.index) * (w + laneGap), y: f.top)
                }
                if let now {
                    BrassLine(label: nowLabel)
                        .offset(y: CGFloat(now - firstHour * 60) * hourHeight / 60 - (nowLabel == nil ? 0 : 9))
                }
            }
        }
        .frame(height: CGFloat(hours) * hourHeight)
    }
}
