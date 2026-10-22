import ActivityKit
import CalendarCore
import CalendarUI
import SurfacesCore
import SurfacesUI
import SwiftUI
import WidgetKit

// MARK: Data

struct SurfaceEntry: TimelineEntry {
    let date: Date
    let data: WidgetEntryData
}

private struct Completion: @unchecked Sendable { let call: (Timeline<SurfaceEntry>) -> Void }
private struct SnapshotCompletion: @unchecked Sendable { let call: (SurfaceEntry) -> Void }

/// Reads the saved agenda from the App Group database. Never touches the network.
enum SurfaceLoader {
    static func input(now: Date) async -> WidgetInput {
        let paired = HubConfigStore().load() != nil
        guard let db = try? AppDatabase.openShared() else {
            return WidgetInput(items: [], accounts: [], paired: paired, lastSyncAt: nil, tz: .current)
        }
        let tz = (try? db.userTimeZone()) ?? .current
        let today = DayKey(now, in: tz)
        let items = (try? await GRDBAgendaRepository(db: db).items(from: today.adding(days: -1), to: today.adding(days: 14))) ?? []
        let accounts = (try? await GRDBAccountRepository(db: db).accounts()) ?? []
        let last = try? await GRDBSyncMetaRepository(db: db).lastSyncAt()
        return WidgetInput(items: items, accounts: accounts, paired: paired, lastSyncAt: last, tz: tz)
    }
}

struct SurfaceProvider: TimelineProvider {
    func placeholder(in context: Context) -> SurfaceEntry {
        let now = Date()
        let e = WidgetEntryData.EventState(
            id: "p", title: "Roadmap review", start: now.addingTimeInterval(480), end: now.addingTimeInterval(3_180), time: "10:30",
            color: "cobalt", live: false, relative: "in 8 min",
            rows: [.init(id: "p", time: "10:30", title: "Roadmap review", status: "8 min", color: "cobalt", live: false)],
            remaining: 1, accessibility: "Roadmap review, 10:30, in 8 min")
        return SurfaceEntry(date: now, data: .init(date: now, content: .event(e), stale: nil))
    }

    func getSnapshot(in context: Context, completion: @escaping (SurfaceEntry) -> Void) {
        if context.isPreview { completion(placeholder(in: context)); return }
        let box = SnapshotCompletion(call: completion)
        Task {
            let now = Date()
            let tl = TimelineBuilder.timeline(await SurfaceLoader.input(now: now), now: now)
            box.call(SurfaceEntry(date: now, data: tl.entries[0]))
        }
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<SurfaceEntry>) -> Void) {
        let box = Completion(call: completion)
        Task {
            let now = Date()
            let tl = TimelineBuilder.timeline(await SurfaceLoader.input(now: now), now: now)
            box.call(Timeline(entries: tl.entries.map { SurfaceEntry(date: $0.date, data: $0) }, policy: .after(tl.reloadAt)))
        }
    }
}

// MARK: Widgets

struct NextEventWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "dev.casprine.calendar.next", provider: SurfaceProvider()) { entry in
            NextEntryView(entry: entry)
        }
        .configurationDisplayName("Next event")
        .description("Your next event and how long until it starts.")
        .supportedFamilies([.systemSmall, .accessoryRectangular, .accessoryCircular, .accessoryInline])
    }
}

struct NextEntryView: View {
    @Environment(\.widgetFamily) private var family
    let entry: SurfaceEntry
    var body: some View {
        Group {
            switch family {
            case .accessoryRectangular: AccessoryRectangularView(entry: entry.data)
            case .accessoryCircular: AccessoryCircularView(entry: entry.data)
            case .accessoryInline: AccessoryInlineView(entry: entry.data)
            default: NextSmallView(entry: entry.data)
            }
        }
        .widgetURL(entry.data.link.url)
        .containerBackground(for: .widget) { family.isAccessory ? Color.clear : Color.calBg }
    }
}

struct TodayWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "dev.casprine.calendar.today", provider: SurfaceProvider()) { entry in
            TodayMediumView(entry: entry.data)
                .widgetURL(entry.data.link.url)
                .containerBackground(for: .widget) { Color.calBg }
        }
        .configurationDisplayName("Today")
        .description("What is left of today.")
        .supportedFamilies([.systemMedium])
    }
}

struct DateWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "dev.casprine.calendar.date", provider: SurfaceProvider()) { entry in
            DateSmallView(date: entry.date, tz: (try? AppDatabase.openShared().userTimeZone()) ?? .current)
                .widgetURL(SurfaceLink.today.url)
                .containerBackground(for: .widget) { Color.calSurface }
        }
        .configurationDisplayName("Date")
        .description("Today's date. Opens your day.")
        .supportedFamilies([.systemSmall])
    }
}

private extension WidgetFamily {
    var isAccessory: Bool {
        switch self {
        case .accessoryRectangular, .accessoryCircular, .accessoryInline: true
        default: false
        }
    }
}

// MARK: Live Activity

struct MeetingActivityWidget: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: MeetingActivityAttributes.self) { context in
            MeetingLockView(attributes: context.attributes, state: context.state, isStale: context.isStale)
                .activityBackgroundTint(Color.calBg)
        } dynamicIsland: { context in
            let span: ClosedRange<Date> = context.state.running || context.isStale
                ? Date.now...max(context.state.end, Date.now.addingTimeInterval(1))
                : Date.now...max(context.state.start, Date.now.addingTimeInterval(1))
            let time = SurfaceFormat(tz: .current).flipTime(context.state.start)
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Text(time).font(CalFont.font(.monoMedium, size: 15, relativeTo: .body)).monospacedDigit()
                }
                DynamicIslandExpandedRegion(.trailing) {
                    Text(timerInterval: span, countsDown: true).font(CalFont.font(.mono, size: 15, relativeTo: .body))
                        .monospacedDigit().frame(width: 64, alignment: .trailing)
                }
                DynamicIslandExpandedRegion(.center) {
                    Text(context.state.title).font(CalFont.font(.sansMedium, size: 16, relativeTo: .headline)).lineLimit(1)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    if let url = context.attributes.joinURL.flatMap(URL.init(string:)) {
                        Link(destination: url) {
                            Text("Join").font(CalFont.font(.sansMedium, size: 15, relativeTo: .body))
                                .frame(maxWidth: .infinity).padding(.vertical, 8)
                                .background(AccountColor(key: context.attributes.color).bright)
                                .foregroundStyle(Theme.onAccountColor.color)
                        }
                    }
                }
            } compactLeading: {
                Text(time).font(CalFont.font(.mono, size: 12, relativeTo: .caption)).monospacedDigit()
            } compactTrailing: {
                Text(timerInterval: span, countsDown: true).font(CalFont.font(.mono, size: 12, relativeTo: .caption))
                    .monospacedDigit().frame(width: 44)
            } minimal: {
                Rectangle().fill(Color.calAccent).frame(width: 8, height: 8)
            }
            .widgetURL(SurfaceLink.event(context.attributes.occurrenceId).url)
        }
    }
}

@main
struct CalendarWidgetBundle: WidgetBundle {
    var body: some Widget {
        NextEventWidget()
        TodayWidget()
        DateWidget()
        MeetingActivityWidget()
    }
}
