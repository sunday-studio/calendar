import CalendarCore
import CalendarUI
import SurfacesCore
import SwiftUI

/// Copy for the widget states (spec w-states).
enum WidgetCopy {
    static func card(_ c: WidgetEntryData.Content) -> (title: String, line: String, attention: Bool)? {
        switch c {
        case .noAccounts: ("Connect an account", "Tap to open Calendar", false)
        case .needsSignIn(let a): ("Showing saved events", "\(a) needs sign in. Open Calendar.", true)
        case .nothingPlanned: ("Nothing planned", "No events on your calendars", false)
        case .done(let next):
            ("Done for today", next.map { "Next: \($0.title), \($0.when)" } ?? "Nothing else is planned", false)
        case .event: nil
        }
    }
}

/// Small widget: next or in-progress event, or a state card. The app wraps it in the container background.
public struct NextSmallView: View {
    let entry: WidgetEntryData
    public init(entry: WidgetEntryData) { self.entry = entry }
    public var body: some View {
        switch entry.content {
        case .event(let e):
            WidgetNextSmall(time: e.time, title: e.title, relative: e.relative, color: AccountColor(key: e.color))
                .accessibilityLabel(e.accessibility)
        default:
            let c = WidgetCopy.card(entry.content)!
            WidgetStateCard(title: c.title, line: c.line, needsAttention: c.attention)
        }
    }
}

/// Medium widget: "TODAY / 3 LEFT" and up to three rows, each a link to its event.
public struct TodayMediumView: View {
    let entry: WidgetEntryData
    public init(entry: WidgetEntryData) { self.entry = entry }
    public var body: some View {
        switch entry.content {
        case .event(let e):
            VStack(alignment: .leading, spacing: 10) {
                HStack {
                    Text("Today").calLabel()
                    Spacer()
                    Text(entry.stale ?? "\(e.remaining) left").calLabel(entry.stale == nil ? .calInk2 : .calAmber)
                }
                VStack(spacing: 0) {
                    let rows = Array(e.rows.prefix(3))
                    ForEach(Array(rows.enumerated()), id: \.element.id) { i, r in
                        Link(destination: SurfaceLink.event(r.id).url) {
                            HStack(spacing: 12) {
                                Text(r.time).font(CalFont.font(.mono, size: 13, relativeTo: .footnote)).monospacedDigit()
                                    .foregroundStyle(Color.calInk2).frame(width: 42, alignment: .leading)
                                Text(r.title).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk).lineLimit(1)
                                Spacer(minLength: 4)
                                if let s = r.status { Text(s).font(CalFont.hint).foregroundStyle(Color.calAmber) }
                            }
                            .padding(.leading, 14)
                            .frame(height: 44)
                            .overlay(alignment: .leading) {
                                Rectangle().fill(AccountColor(key: r.color).color).frame(width: 3).padding(.vertical, 9)
                            }
                            .overlay(alignment: .bottom) { if i < rows.count - 1 { Rectangle().fill(Color.calLine).frame(height: 1) } }
                        }
                    }
                }
            }
            .padding(14)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        default:
            let c = WidgetCopy.card(entry.content)!
            WidgetStateCard(title: c.title, line: c.line, needsAttention: c.attention)
        }
    }
}

/// Small date widget: mono caps weekday and a 36pt day number. Opens Home on today.
public struct DateSmallView: View {
    let weekday: String
    let day: String
    public init(date: Date, tz: TimeZone, locale: Locale = .current) {
        let f = DateFormatter(); f.locale = locale; f.timeZone = tz
        f.setLocalizedDateFormatFromTemplate("EEEE"); weekday = f.string(from: date)
        f.setLocalizedDateFormatFromTemplate("d"); day = f.string(from: date)
    }
    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(weekday).calLabel().font(CalFont.font(.mono, size: 13, relativeTo: .footnote)).lineLimit(1)
            Text(day).font(CalFont.font(.sans, size: 36, relativeTo: .largeTitle)).foregroundStyle(Color.calInk)
            Spacer(minLength: 0)
        }
        .padding(14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityElement(children: .combine)
    }
}

// MARK: Lock-screen accessories

public struct AccessoryRectangularView: View {
    let entry: WidgetEntryData
    public init(entry: WidgetEntryData) { self.entry = entry }
    public var body: some View {
        if case .event(let e) = entry.content {
            VStack(alignment: .leading, spacing: 1) {
                Text(e.title).font(.headline).lineLimit(1).widgetAccentable()
                Text("\(e.time) \u{00B7} \(e.relative)").font(.caption).lineLimit(1)
                if e.rows.count > 1 { Text(e.rows[1].time + " " + e.rows[1].title).font(.caption2).lineLimit(1).opacity(0.7) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            .accessibilityLabel(e.accessibility)
        } else if let c = WidgetCopy.card(entry.content) {
            VStack(alignment: .leading, spacing: 1) {
                Text(c.title).font(.headline).lineLimit(1).widgetAccentable()
                Text(c.line).font(.caption).lineLimit(2)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

public struct AccessoryCircularView: View {
    let entry: WidgetEntryData
    public init(entry: WidgetEntryData) { self.entry = entry }
    public var body: some View {
        VStack(spacing: 0) {
            if case .event(let e) = entry.content {
                let mins = max(Int((e.live ? e.end.timeIntervalSince(entry.date) : e.start.timeIntervalSince(entry.date)) / 60), 0)
                Text(mins >= 60 ? "\(mins / 60)h" : "\(mins)").font(.title2.weight(.semibold)).monospacedDigit().widgetAccentable()
                Text(e.live ? "left" : "min").font(.caption2)
            } else {
                Image(systemName: "calendar").widgetAccentable()
            }
        }
    }
}

public struct AccessoryInlineView: View {
    let entry: WidgetEntryData
    public init(entry: WidgetEntryData) { self.entry = entry }
    public var body: some View {
        if case .event(let e) = entry.content { Text("\(e.time) \(e.title)") }
        else if let c = WidgetCopy.card(entry.content) { Text(c.title) }
    }
}
