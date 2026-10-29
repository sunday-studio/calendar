import CalendarCore
import CalendarUI
import SurfacesCore
import SwiftUI

/// Everything the long look draws, resolved from the saved agenda (pure, so it is testable).
public struct NotificationContentModel: Sendable {
    public struct Block: Identifiable, Sendable {
        public var id: String
        public var title: String
        public var start: Date
        public var end: Date
        public var color: String
        public var notified: Bool
    }

    public var meta: String
    public var title: String
    public var subline: String
    public var color: String
    public var start: Date?
    public var end: Date?
    public var live: Bool
    public var joinURL: URL?
    public var joinLabel: String?
    public var windowStart: Date
    public var blocks: [Block]
    public var now: Date

    /// 3 hours from now at 44 pt per hour.
    public static let hours: Double = 3

    public static func fallback(title: String, subtitle: String, body: String) -> NotificationContentModel {
        NotificationContentModel(meta: "CALENDAR", title: title, subline: [subtitle, body].filter { !$0.isEmpty }.joined(separator: " \u{00B7} "),
                                 color: "cobalt", start: nil, end: nil, live: false, joinURL: nil, joinLabel: nil,
                                 windowStart: Date(), blocks: [], now: Date())
    }

    public static func make(items: [AgendaItem], eventId: String?, now: Date, tz: TimeZone) -> NotificationContentModel? {
        guard let id = eventId, let item = items.first(where: { $0.id == id || $0.eventId == id }), let s = item.start, let e = item.end else { return nil }
        let fmt = SurfaceFormat(tz: tz)
        let windowStart = Date(timeIntervalSince1970: (now.timeIntervalSince1970 / 900).rounded(.down) * 900)
        let windowEnd = windowStart.addingTimeInterval(hours * 3600)
        let blocks = items.filter { !$0.allDay && $0.status != "cancelled" && $0.myResponse != "declined" }
            .compactMap { i -> Block? in
                guard let bs = i.start, let be = i.end, bs < windowEnd, be > windowStart else { return nil }
                return Block(id: i.id, title: i.title, start: bs, end: be, color: i.color, notified: i.id == item.id)
            }
        let link = item.join.flatMap { URL(string: $0.url) != nil ? $0 : nil }
        let sub = [link.map { SurfaceFormat.joinLabel($0.kind) } ?? item.location, fmt.time(s) + " to " + fmt.time(e)].compactMap { $0 }.joined(separator: " \u{00B7} ")
        return NotificationContentModel(
            meta: "\(fmt.headerDate(now)) \u{00B7} \(fmt.time(now))", title: item.title, subline: sub, color: item.color, start: s, end: e,
            live: s <= now, joinURL: link.flatMap { URL(string: $0.url) }, joinLabel: link.map { SurfaceFormat.joinLabel($0.kind) },
            windowStart: windowStart, blocks: blocks, now: now)
    }
}

/// Long look content (366 pt wide, padding 16): meta line, title row with the countdown, a 3-hour timeline, a compact Join.
public struct NotificationContentView: View {
    let model: NotificationContentModel
    let showsJoin: Bool
    private let pph: CGFloat = 44

    public init(model: NotificationContentModel, showsJoin: Bool) { self.model = model; self.showsJoin = showsJoin }

    public var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(model.meta).font(CalFont.font(.mono, size: 10, relativeTo: .caption2)).tracking(1.4).foregroundStyle(Color.calInk2)
            HStack(alignment: .top, spacing: 10) {
                RoundedRectangle(cornerRadius: 2).fill(AccountColor(key: model.color).color).frame(width: 3, height: 42)
                VStack(alignment: .leading, spacing: 2) {
                    Text(model.title).font(CalFont.font(.sansMedium, size: 20, relativeTo: .headline)).foregroundStyle(Color.calInk).lineLimit(2)
                    Text(model.subline).font(CalFont.font(.sans, size: 13, relativeTo: .footnote)).foregroundStyle(Color.calInk2).lineLimit(1)
                }
                Spacer(minLength: 4)
                countdown
            }
            if !model.blocks.isEmpty { timeline.frame(height: pph * CGFloat(NotificationContentModel.hours)) }
            if showsJoin, let url = model.joinURL {
                HStack {
                    if let l = model.joinLabel { Text("Opens \(l)").font(CalFont.font(.sans, size: 13, relativeTo: .footnote)).foregroundStyle(Color.calInk2) }
                    Spacer()
                    Link(destination: url) {
                        // 36 high at inset 16 in the r 34 frame: r 18 is a capsule, concentric.
                        Text("Join").font(CalFont.font(.sansMedium, size: 15, relativeTo: .body)).foregroundStyle(Color.calBg)
                            .padding(.horizontal, 22).frame(height: 36).background(Capsule().fill(Color.calInk))
                    }
                }
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private var countdown: some View {
        if let s = model.start, let e = model.end {
            let target = model.live ? e : s
            Text(timerInterval: model.now...max(target, model.now.addingTimeInterval(1)), countsDown: true)
                .font(CalFont.font(.mono, size: 20, relativeTo: .title3)).monospacedDigit().foregroundStyle(Color.calInk)
                .frame(maxWidth: 84, alignment: .trailing)
        }
    }

    private func y(_ d: Date) -> CGFloat { CGFloat(d.timeIntervalSince(model.windowStart) / 3600) * pph }

    private var timeline: some View {
        ZStack(alignment: .topLeading) {
            ForEach(0..<Int(NotificationContentModel.hours), id: \.self) { h in
                ZStack(alignment: .topLeading) {
                    Rectangle().fill(WidgetTheme.line).frame(height: 1)
                    Text(hourLabel(h)).font(CalFont.font(.mono, size: 9, relativeTo: .caption2)).foregroundStyle(Color.calInk2)
                        .frame(width: 24, alignment: .leading).offset(y: 3)
                }
                .offset(y: CGFloat(h) * pph)
            }
            ForEach(model.blocks) { b in
                let top = max(y(b.start), 0), bottom = min(y(b.end), pph * CGFloat(NotificationContentModel.hours))
                Text(b.title).font(CalFont.font(b.notified ? .sansMedium : .sans, size: 13, relativeTo: .footnote)).lineLimit(1)
                    .foregroundStyle(b.notified ? WidgetTheme.blockInk : Color.calInk)
                    .padding(.horizontal, 8).frame(maxWidth: .infinity, alignment: .leading)
                    .frame(height: max(bottom - top, 12), alignment: .top)
                    // Far from every corner: the 4 pt floor, not a concentric radius.
                    .background(RoundedRectangle(cornerRadius: 4).fill(b.notified ? AccountColor(key: b.color).bright : WidgetTheme.event))
                    .padding(.leading, 28).offset(y: top)
            }
            if model.now >= model.windowStart {
                Rectangle().fill(WidgetTheme.amber).frame(height: 1).padding(.leading, 28).offset(y: y(model.now))
            }
        }
    }

    private func hourLabel(_ h: Int) -> String {
        let t = model.windowStart.addingTimeInterval(Double(h) * 3600)
        let f = DateFormatter(); f.dateFormat = "HH"
        return f.string(from: t)
    }
}
