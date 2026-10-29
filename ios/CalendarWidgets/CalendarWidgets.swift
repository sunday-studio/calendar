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
    var config: WidgetConfig = .default
}

/// Reads the saved agenda from the App Group database. Never touches the network.
enum SurfaceLoader {
    static func input(now: Date, config: WidgetConfig = .default) async -> WidgetInput {
        let paired = HubConfigStore().load() != nil
        guard let db = try? AppDatabase.openShared() else {
            return WidgetInput(items: [], accounts: [], paired: paired, lastSyncAt: nil, tz: .current, config: config)
        }
        let tz = (try? db.userTimeZone()) ?? .current
        let today = DayKey(now, in: tz)
        let items = (try? await GRDBAgendaRepository(db: db).items(from: today.adding(days: -1), to: today.adding(days: 14))) ?? []
        let accounts = (try? await GRDBAccountRepository(db: db).accounts()) ?? []
        let last = try? await GRDBSyncMetaRepository(db: db).lastSyncAt()
        return WidgetInput(items: items, accounts: accounts, paired: paired, lastSyncAt: last, tz: tz, config: config)
    }

    static func timeline(config: WidgetConfig) async -> Timeline<SurfaceEntry> {
        let now = Date()
        let tl = TimelineBuilder.timeline(await input(now: now, config: config), now: now)
        return Timeline(entries: tl.entries.map { SurfaceEntry(date: $0.date, data: $0, config: config) }, policy: .after(tl.reloadAt))
    }

    static func placeholder() -> SurfaceEntry {
        let now = Date()
        let hero = DayLayout.Hero(id: "p", title: "Roadmap review", time: "10:00", color: "rose", live: false, start: now.addingTimeInterval(480),
                                  end: now.addingTimeInterval(2280), sub: "IN 8 MIN", badge: "MEET", badgeLong: "GOOGLE MEET", joinURL: nil,
                                  needsReply: false, place: nil, spoken: "Roadmap review, in 8 minutes")
        let rows = [WidgetRowData(id: "p1", time: "11:45", title: "Dentist", status: nil, color: "cobalt", live: false),
                    WidgetRowData(id: "p2", time: "12:30", title: "Lunch with Sam", status: nil, color: "cobalt", live: false)]
        let layout = DayLayout(header: "TODAY", tomorrowHeader: "TOMORROW", allDay: [], hero: hero, today: rows, tomorrow: [], todayLeft: 3)
        let e = WidgetEntryData.EventState(id: "p", title: hero.title, start: hero.start, end: hero.end, time: hero.time, color: hero.color,
                                           live: false, relative: "in 8 min", rows: rows, remaining: 3, accessibility: hero.spoken)
        return SurfaceEntry(date: now, data: .init(date: now, content: .event(e), stale: nil, layout: layout))
    }
}

/// One provider for every widget: the intent carries layout, calendars, declined and lock-screen titles.
struct SurfaceProvider: AppIntentTimelineProvider {
    typealias Intent = CalendarWidgetIntent
    func placeholder(in context: Context) -> SurfaceEntry { SurfaceLoader.placeholder() }

    func snapshot(for configuration: CalendarWidgetIntent, in context: Context) async -> SurfaceEntry {
        if context.isPreview { return SurfaceLoader.placeholder() }
        let tl = await SurfaceLoader.timeline(config: configuration.config)
        return tl.entries.first ?? SurfaceLoader.placeholder()
    }

    func timeline(for configuration: CalendarWidgetIntent, in context: Context) async -> Timeline<SurfaceEntry> {
        await SurfaceLoader.timeline(config: configuration.config)
    }
}

// MARK: Widgets

struct NextEventWidget: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "dev.casprine.calendar.next", intent: CalendarWidgetIntent.self, provider: SurfaceProvider()) { entry in
            NextEntryView(entry: entry)
        }
        .configurationDisplayName("Next event")
        .description("Your next event and how long until it starts.")
        .supportedFamilies([.systemSmall, .accessoryRectangular, .accessoryCircular, .accessoryInline])
    }
}

/// Accented and vibrant modes: the system flattens colour. Accentable parts are marked in the views; the container goes clear.
struct NextEntryView: View {
    @Environment(\.widgetFamily) private var family
    @Environment(\.widgetRenderingMode) private var mode
    let entry: SurfaceEntry
    var body: some View {
        Group {
            switch family {
            case .accessoryRectangular: AccessoryRectangularView(entry: entry.data, showTitles: entry.config.showLockTitles)
            case .accessoryCircular: AccessoryCircularView(entry: entry.data)
            case .accessoryInline: AccessoryInlineView(entry: entry.data, showTitles: entry.config.showLockTitles)
            default: NextSmallView(entry: entry.data)
            }
        }
        .widgetURL(entry.data.link.url)
        .containerBackground(for: .widget) { widgetBackground(family, mode) }
    }
}

private func widgetBackground(_ family: WidgetFamily, _ mode: WidgetRenderingMode) -> Color {
    family.isAccessory || mode != .fullColor ? Color.clear : WidgetTheme.container
}

struct TodayWidget: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "dev.casprine.calendar.today", intent: CalendarWidgetIntent.self, provider: SurfaceProvider()) { entry in
            TodayEntryView(entry: entry)
        }
        .configurationDisplayName("Today")
        .description("What is left of today.")
        .supportedFamilies([.systemMedium, .systemLarge])
    }
}

struct TodayEntryView: View {
    @Environment(\.widgetFamily) private var family
    @Environment(\.widgetRenderingMode) private var mode
    let entry: SurfaceEntry
    var body: some View {
        Group {
            if family == .systemLarge { TodayLargeView(entry: entry.data, layout: entry.config.largeLayout) }
            else { TodayMediumView(entry: entry.data, layout: entry.config.mediumLayout) }
        }
        .widgetURL(entry.data.link.url)
        .containerBackground(for: .widget) { widgetBackground(family, mode) }
    }
}

struct DateWidget: Widget {
    var body: some WidgetConfiguration {
        AppIntentConfiguration(kind: "dev.casprine.calendar.date", intent: CalendarWidgetIntent.self, provider: SurfaceProvider()) { entry in
            DateEntryView(entry: entry)
        }
        .configurationDisplayName("Date")
        .description("Today's date, or how many events are left.")
        .supportedFamilies([.systemSmall, .accessoryCircular])
    }
}

struct DateEntryView: View {
    @Environment(\.widgetFamily) private var family
    @Environment(\.widgetRenderingMode) private var mode
    let entry: SurfaceEntry
    var body: some View {
        Group {
            if family == .accessoryCircular { AccessoryCircularView(entry: WidgetEntryData(date: entry.date, content: .nothingPlanned, stale: nil, layout: entry.data.layout)) }
            else {
                DateSmallView(date: entry.date, tz: (try? AppDatabase.openShared().userTimeZone()) ?? .current, left: entry.data.layout?.todayLeft)
            }
        }
        .widgetURL(SurfaceLink.today.url)
        .containerBackground(for: .widget) { widgetBackground(family, mode) }
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
                .activityBackgroundTint(WidgetTheme.container)
        } dynamicIsland: { context in
            let span: ClosedRange<Date> = context.state.running || context.isStale
                ? Date.now...max(context.state.end, Date.now.addingTimeInterval(1))
                : Date.now...max(context.state.start, Date.now.addingTimeInterval(1))
            let time = SurfaceFormat(tz: .current).flipTime(context.state.start)
            let bright = AccountColor(key: context.attributes.color).bright
            let joinURL = context.attributes.joinURL.flatMap(URL.init(string:))
            return DynamicIsland {
                // Expanded (about 371 x 132): bar + flip cells, big timer, title and place, 72 x 36 Join capsule at inset 24.
                DynamicIslandExpandedRegion(.leading) {
                    HStack(spacing: 8) {
                        RoundedRectangle(cornerRadius: 2).fill(bright).frame(width: 3, height: 30)
                        FlipCells(time, size: .small)
                    }
                }
                DynamicIslandExpandedRegion(.trailing) {
                    Text(timerInterval: span, countsDown: true).font(CalFont.font(.mono, size: 20, relativeTo: .title3))
                        .monospacedDigit().frame(width: 72, alignment: .trailing)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    HStack(spacing: 10) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(context.state.title).font(CalFont.font(.sansMedium, size: 17, relativeTo: .headline)).lineLimit(1)
                            Text(joinURL != nil ? SurfaceFormat.joinLabel(context.attributes.joinKind) : (context.state.location ?? ""))
                                .font(CalFont.font(.sans, size: 13, relativeTo: .footnote)).foregroundStyle(.secondary).lineLimit(1)
                        }
                        Spacer(minLength: 4)
                        if let joinURL {
                            Link(destination: joinURL) {
                                Text("Join").font(CalFont.font(.sansMedium, size: 15, relativeTo: .body)).foregroundStyle(WidgetTheme.blockInk)
                                    .frame(width: 72, height: 36).background(Capsule().fill(bright))
                            }
                        }
                    }
                    .padding(.horizontal, 8)
                }
            } compactLeading: {
                HStack(spacing: 4) {
                    RoundedRectangle(cornerRadius: 2).fill(bright).frame(width: 3, height: 16)
                    Text(time).font(CalFont.font(.monoMedium, size: 12, relativeTo: .caption)).monospacedDigit()
                }
            } compactTrailing: {
                Text(timerInterval: span, countsDown: true).font(CalFont.font(.monoMedium, size: 12, relativeTo: .caption))
                    .monospacedDigit().frame(width: 44)
            } minimal: {
                Circle().fill(bright).frame(width: 9, height: 9)
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
