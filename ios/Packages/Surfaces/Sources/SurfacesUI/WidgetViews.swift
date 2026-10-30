import CalendarUI
import SurfacesCore
import SwiftUI

/// Copy for the widget states (spec w-states).
enum WidgetCopy {
    static func card(_ c: WidgetEntryData.Content) -> (title: String, line: String, attention: Bool)? {
        switch c {
        case .noAccounts: ("Connect an account", "Tap to open Calendar", false)
        case .needsSignIn(let a): ("Showing saved events", "\(a) needs sign in", true)
        case .nothingPlanned: ("Nothing planned", "No events today or tomorrow.", false)
        case .done(let next):
            ("Done for today", next.map { "Next: \($0.title), \($0.when)" } ?? "Nothing else is planned", false)
        case .event: nil
        }
    }
}

// MARK: v2 Home Screen widgets (spec docs/specs/widgets-notifications.md, boards w2-small / w2-medium / w2-large / w2-states)

/// Grid constants of the spec: content inset 16, block inset 8, row 24, columns bar x16 / time x27 / title x78.
enum WidgetGrid {
    static let inset: CGFloat = 16
    static let blockInset: CGFloat = 8
    static let row: CGFloat = 24
    static let label: CGFloat = 20
}

private struct PrivacyTitle: ViewModifier {
    @Environment(\.redactionReasons) private var reasons
    let width: CGFloat
    func body(content: Content) -> some View {
        if reasons.contains(.privacy) {
            RoundedRectangle(cornerRadius: 2).fill(WidgetTheme.line).frame(width: width, height: 8)
        } else { content }
    }
}

private extension View {
    func privateTitle(_ width: CGFloat = 120) -> some View { modifier(PrivacyTitle(width: width)).privacySensitive() }
}

/// 3 x 14 account bar (r 2: the spec's 4 pt floor does not apply to a bar this thin).
private struct AccountBar: View {
    let color: String
    var height: CGFloat = 14
    var body: some View {
        RoundedRectangle(cornerRadius: 2).fill(AccountColor(key: color).color).frame(width: 3, height: height)
    }
}

/// Filled "needs you" tag: amber, ink text, r 4. Never amber text.
private struct NeedsTag: View {
    let text: String
    var body: some View {
        Text(text).font(CalFont.font(.monoMedium, size: 10, relativeTo: .caption2)).tracking(0.8)
            .foregroundStyle(WidgetTheme.blockInk)
            .padding(.horizontal, 6).padding(.vertical, 2)
            .background(RoundedRectangle(cornerRadius: 4).fill(WidgetTheme.amber))
    }
}

/// One 24 pt row on the shared grid, a link to its event. `first` drops the hairline above.
struct WidgetListRow: View {
    let row: WidgetRowData
    let first: Bool
    var link = true
    /// Medium B list column: bar at x8 of its own grid, 38 pt time column, needs-you as an 8 pt dot.
    var compact = false
    @Environment(\.redactionReasons) private var reasons

    var body: some View {
        let content = HStack(spacing: 8) {
            AccountBar(color: row.color)
            Text(row.time).font(CalFont.font(.mono, size: 12, relativeTo: .caption)).monospacedDigit()
                .foregroundStyle(Color.calInk2).frame(width: compact ? 38 : 43, alignment: .leading).lineLimit(1)
            Text(row.title).font(CalFont.font(.sans, size: 14, relativeTo: .body)).foregroundStyle(Color.calInk).lineLimit(1)
                .privateTitle()
            Spacer(minLength: 4)
            if let s = row.status {
                switch row.statusKind {
                case .needsYou where compact: Circle().fill(WidgetTheme.amber).frame(width: 8, height: 8)
                case .needsYou: NeedsTag(text: s)
                default: Text(s).font(CalFont.font(.mono, size: 10, relativeTo: .caption2)).tracking(0.6).foregroundStyle(Color.calInk2)
                }
            }
        }
        .padding(.leading, compact ? 8 : WidgetGrid.inset).padding(.trailing, WidgetGrid.inset)
        .frame(height: WidgetGrid.row)
        .opacity(row.faded ? 0.45 : 1)
        .overlay(alignment: .top) {
            if !first { Rectangle().fill(WidgetTheme.line).frame(height: 1).padding(.leading, compact ? 8 : WidgetGrid.inset).padding(.trailing, WidgetGrid.inset) }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(reasons.contains(.privacy) ? "Event, \(row.time)" : row.spoken)
        if link { Link(destination: SurfaceLink.event(row.id).url) { content } } else { content }
    }
}

/// `TOMORROW · WED 14`: text, then a hairline to the trailing edge.
struct WidgetGroupLabel: View {
    let text: String
    var body: some View {
        HStack(spacing: 8) {
            Text(text).widgetCaps().foregroundStyle(Color.calInk2).lineLimit(1).fixedSize()
            Rectangle().fill(WidgetTheme.line).frame(height: 1)
        }
        .padding(.horizontal, WidgetGrid.inset).frame(height: WidgetGrid.label)
        .accessibilityAddTraits(.isHeader)
    }
}

/// All-day events as one quiet line: dot in the account colour, name in dim 12 pt. Never a row.
struct AllDayLine: View {
    let chips: [AllDayChip]
    var body: some View {
        if let first = chips.first {
            HStack(spacing: 6) {
                Circle().fill(AccountColor(key: first.color).color).frame(width: 6, height: 6)
                Text(chips.count > 1 ? "\(first.title) +\(chips.count - 1)" : first.title)
                    .font(CalFont.font(.sans, size: 12, relativeTo: .caption)).foregroundStyle(Color.calInk2).lineLimit(1)
                    .privateTitle(80)
            }
            .accessibilityElement(children: .combine)
        }
    }
}

/// The bright next-event block (r concentric to the container, inset 8) with flip cells, title, mono line and Join / Reply.
struct NextBlock: View {
    let hero: DayLayout.Hero
    var long = false

    var body: some View {
        HStack(spacing: 8) {
            FlipCells(hero.time, size: .small, onBlock: true).frame(width: FlipCells.width(hero.time, size: .small), alignment: .leading)
            VStack(alignment: .leading, spacing: 1) {
                Text(hero.title).font(CalFont.font(.sansMedium, size: 15, relativeTo: .headline)).lineLimit(1).privateTitle(110)
                Text(sub).font(CalFont.font(.mono, size: 10, relativeTo: .caption2)).tracking(0.6).lineLimit(1).opacity(0.72)
            }
            .foregroundStyle(WidgetTheme.blockInk)
            Spacer(minLength: 4)
            action
        }
        .padding(.horizontal, WidgetGrid.blockInset)
        .frame(height: 48)
        .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(AccountColor(key: hero.color).bright))
        .padding(.horizontal, WidgetGrid.blockInset)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(hero.spoken)
    }

    private var sub: String {
        guard !hero.needsReply, let b = long ? hero.badgeLong : hero.badge else { return hero.sub }
        return "\(hero.sub) \u{00B7} \(b)"
    }

    @ViewBuilder private var action: some View {
        if hero.needsReply {
            Link(destination: SurfaceLink.event(hero.id).url) { pill("Reply") }
        } else if let url = hero.joinURL {
            Link(destination: url) { pill("Join") }
        }
    }

    private func pill(_ t: String) -> some View {
        Text(t).font(CalFont.font(.sansMedium, size: 14, relativeTo: .body)).foregroundStyle(WidgetTheme.joinInk)
            .padding(.horizontal, 14).frame(height: 32)
            .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(WidgetTheme.blockInk))   // r = container - 16 = 8
    }
}

/// Header text for the medium / large lists: the date and, on the trailing edge, a state word or `+N MORE`.
private struct ListFooterText: View {
    let text: String
    var body: some View { Text(text).widgetCaps().foregroundStyle(Color.calInk2).lineLimit(1) }
}

/// Medium widget, layout A: bright block, three rows, footer with the date, the all-day line and `+N MORE`.
public struct TodayMediumView: View {
    let entry: WidgetEntryData
    let layout: WidgetConfig.MediumLayout
    @Environment(\.redactionReasons) private var reasons
    public init(entry: WidgetEntryData, layout: WidgetConfig.MediumLayout = .list) { self.entry = entry; self.layout = layout }

    public var body: some View {
        if layout == .nextList, let l = entry.layout, l.hero != nil, entry.stale == nil, case .event = entry.content {
            MediumNextPanelView(entry: entry, layout: l)
        } else if let l = entry.layout, let c = ListShape(entry) {
            content(l, c)
        } else if let card = WidgetCopy.card(entry.content) {
            WidgetStateCardV2(title: card.title, line: card.line, needsAttention: card.attention)
        }
    }

    @ViewBuilder private func content(_ l: DayLayout, _ shape: ListShape) -> some View {
        let fill = WidgetFill.rows(l, slots: shape.mediumSlots)
        ZStack(alignment: .topLeading) {
            switch shape.kind {
            case .nothing: NothingPlanned(layout: l)
            case .list:
                VStack(spacing: 0) {
                    if shape.header { StateHeader(l: l, trailing: shape.trailing(reasons: reasons)) }
                    if shape.signIn, case .needsSignIn(let a) = entry.content { SignInBlock(account: a) }
                    if shape.showsBlock, let h = l.hero { NextBlock(hero: h).padding(.top, WidgetGrid.blockInset) }
                    VStack(spacing: 0) {
                        if shape.title != nil {
                            Text(shape.title!).font(CalFont.font(.sansMedium, size: 17, relativeTo: .headline)).foregroundStyle(Color.calInk)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(.horizontal, WidgetGrid.inset).padding(.bottom, 4)
                        }
                        RowStack(fill: fill, links: !shape.signIn)
                    }
                    .padding(.top, shape.showsBlock ? 2 : 0)
                    Spacer(minLength: 0)
                }
                if shape.footer { footer(l, fill: fill).frame(maxHeight: .infinity, alignment: .bottom) }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private func footer(_ l: DayLayout, fill: WidgetFill.Result) -> some View {
        HStack(spacing: 10) {
            ListFooterText(text: l.header)
            AllDayLine(chips: l.allDay)
            Spacer(minLength: 0)
            if fill.overflow > 0 { ListFooterText(text: "+\(fill.overflow) MORE") }
            else if fill.tomorrowShown, l.hero != nil { ListFooterText(text: "\(l.todayLeft) LEFT") }
        }
        .padding(.horizontal, WidgetGrid.inset).frame(height: 22).padding(.bottom, 7)
    }
}

/// Large widget (Agenda): header with the all-day line, the block, five rows today, TOMORROW, four rows tomorrow.
public struct TodayLargeView: View {
    let entry: WidgetEntryData
    let layout: WidgetConfig.LargeLayout
    @Environment(\.redactionReasons) private var reasons
    public init(entry: WidgetEntryData, layout: WidgetConfig.LargeLayout = .agenda) { self.entry = entry; self.layout = layout }

    public var body: some View {
        if layout == .day, let l = entry.layout, let tl = l.timeline, entry.stale == nil, case .event = entry.content {
            DayLargeView(entry: entry, layout: l, timeline: tl)
        } else if let l = entry.layout, let shape = ListShape(entry) {
            let fill = WidgetFill.rows(l, slots: shape.largeSlots)
            VStack(spacing: 0) {
                switch shape.kind {
                case .nothing: NothingPlanned(layout: l)
                case .list:
                    // Date header: two lines, the all-day line under the date (18 pt, never a row).
                    VStack(alignment: .leading, spacing: 3) {
                        HStack {
                            ListFooterText(text: l.header)
                            Spacer()
                            if let t = shape.trailing(reasons: reasons) { ListFooterText(text: t) }
                            else if fill.overflow > 0 { ListFooterText(text: "+\(fill.overflow) MORE") }
                        }
                        AllDayLine(chips: l.allDay)
                    }
                    .padding(.horizontal, WidgetGrid.inset).padding(.top, 14).padding(.bottom, 6)
                    if shape.signIn, case .needsSignIn(let a) = entry.content { SignInBlock(account: a) }
                    if shape.showsBlock, let h = l.hero { NextBlock(hero: h, long: true) }
                    if shape.title != nil {
                        Text(shape.title!).font(CalFont.font(.sansMedium, size: 17, relativeTo: .headline)).foregroundStyle(Color.calInk)
                            .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, WidgetGrid.inset).padding(.vertical, 6)
                    }
                    RowStack(fill: fill, links: !shape.signIn).padding(.top, 2)
                    Spacer(minLength: 0)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        } else if let card = WidgetCopy.card(entry.content) {
            WidgetStateCardV2(title: card.title, line: card.line, needsAttention: card.attention)
        }
    }
}

/// What a list widget shows for an entry, decided once (spec section 3: which state wins).
struct ListShape {
    enum Kind { case list, nothing }
    var kind: Kind = .list
    var showsBlock = true
    var signIn = false
    var header = false
    var footer = true
    var title: String?
    var stale: String?
    var done = false

    /// Slots: 24 pt rows that fit under the block (medium 3, large 10 incl. the tomorrow label); stale and sign-in drop the block.
    var mediumSlots: Int { stale != nil || done ? 5 - (signIn ? 1 : 0) - (title != nil ? 1 : 0) : (signIn ? 2 : 3) }
    var largeSlots: Int { stale != nil ? 12 : (done ? 11 : (signIn ? 8 : 10)) }

    func trailing(reasons: RedactionReasons) -> String? {
        if reasons.contains(.privacy) { return "LOCKED" }
        if let stale { return stale.uppercased() }
        if signIn { return "SAVED" }
        if done { return "DONE" }
        return nil
    }

    init?(_ e: WidgetEntryData) {
        stale = e.stale
        switch e.content {
        case .noAccounts: return nil
        case .nothingPlanned: kind = .nothing; showsBlock = false; footer = false
        case .needsSignIn: signIn = true; showsBlock = false; header = true; footer = false
        case .done: done = true; showsBlock = false; header = true; footer = false; title = "Done for today"
        case .event:
            if e.stale != nil { showsBlock = false; header = true; footer = false }
        }
    }
}

private struct RowStack: View {
    let fill: WidgetFill.Result
    var links = true
    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(fill.rows.enumerated()), id: \.offset) { n, r in
                switch r {
                case .event(let row):
                    let previousIsLabel: Bool = { if n > 0, case .label = fill.rows[n - 1] { return true } else { return false } }()
                    WidgetListRow(row: row, first: n == 0 || previousIsLabel, link: links)
                case .label(let t): WidgetGroupLabel(text: t)
                }
            }
        }
    }
}

private struct StateHeader: View {
    let l: DayLayout
    let trailing: String?
    var body: some View {
        HStack(spacing: 10) {
            ListFooterText(text: l.header)
            AllDayLine(chips: l.allDay)
            Spacer(minLength: 0)
            if let trailing { ListFooterText(text: trailing) }
        }
        .padding(.horizontal, WidgetGrid.inset).padding(.top, 12).padding(.bottom, 2).frame(height: 30)
    }
}

/// Amber block: "Work needs you to sign in", black Sign in button. Amber means it needs you.
private struct SignInBlock: View {
    let account: String
    var body: some View {
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 1) {
                Text("\(account) needs you to sign in").font(CalFont.font(.sansMedium, size: 14, relativeTo: .headline)).lineLimit(1)
                Text("SAVED EVENTS BELOW").font(CalFont.font(.mono, size: 10, relativeTo: .caption2)).tracking(0.6).opacity(0.72)
            }
            .foregroundStyle(WidgetTheme.blockInk)
            Spacer(minLength: 4)
            Link(destination: SurfaceLink.accounts.url) {
                Text("Sign in").font(CalFont.font(.sansMedium, size: 13, relativeTo: .body)).foregroundStyle(WidgetTheme.joinInk)
                    .padding(.horizontal, 12).frame(height: 28)
                    .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(WidgetTheme.blockInk))
            }
        }
        .padding(.horizontal, WidgetGrid.blockInset).frame(height: 44)
        .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(WidgetTheme.amber))
        .padding(.horizontal, WidgetGrid.blockInset).padding(.bottom, 2)
        .accessibilityElement(children: .combine)
    }
}

/// "Nothing planned": title, one line and a `+ NEW EVENT` row (link `calendar://new`).
private struct NothingPlanned: View {
    let layout: DayLayout
    var body: some View {
        ZStack(alignment: .topLeading) {
            VStack(alignment: .leading, spacing: 4) {
                Text(layout.header).widgetCaps().foregroundStyle(Color.calInk2).padding(.top, 12)
                Text("Nothing planned").font(CalFont.font(.sansMedium, size: 19, relativeTo: .headline)).foregroundStyle(Color.calInk).padding(.top, 18)
                Text("No events today or tomorrow.").font(CalFont.font(.sans, size: 14, relativeTo: .footnote)).foregroundStyle(Color.calInk2)
            }
            .padding(.horizontal, WidgetGrid.inset)
            VStack {
                Spacer()
                Link(destination: SurfaceLink.new.url) {
                    HStack { Text("+ NEW EVENT").widgetCaps().foregroundStyle(Color.calInk); Spacer() }
                        .padding(.horizontal, 0).frame(height: 24)
                        .overlay(alignment: .top) { Rectangle().fill(WidgetTheme.line).frame(height: 1) }
                }
                .padding(.horizontal, WidgetGrid.inset).padding(.bottom, 12)
            }
        }
    }
}

/// Small widget: bright block with flip cells, title and video tag; countdown (live timer) and place below.
public struct NextSmallView: View {
    let entry: WidgetEntryData
    @Environment(\.redactionReasons) private var reasons
    public init(entry: WidgetEntryData) { self.entry = entry }

    public var body: some View {
        if let h = entry.layout?.hero, case .event = entry.content, entry.stale == nil {
            VStack(alignment: .leading, spacing: 0) {
                ZStack(alignment: .topLeading) {
                    FlipCells(h.time, size: .small, onBlock: true).scaleEffect(22.0 / 18.0, anchor: .topLeading)
                        .frame(width: FlipCells.width(h.time, size: .small) * 22.0 / 18.0, height: 36, alignment: .topLeading).offset(x: 8, y: 8)
                    if let b = h.badge {
                        Text(b).font(CalFont.font(.mono, size: 9, relativeTo: .caption2)).tracking(0.9).foregroundStyle(WidgetTheme.blockInk)
                            .padding(.horizontal, 5).padding(.vertical, 1)
                            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(WidgetTheme.blockInk.opacity(0.45), lineWidth: 1))
                            .frame(maxWidth: .infinity, alignment: .topTrailing).padding(.top, 8).padding(.trailing, 8)
                    }
                    Text(h.title).font(CalFont.font(.sansMedium, size: 16, relativeTo: .headline)).lineSpacing(0)
                        .lineLimit(2).foregroundStyle(WidgetTheme.blockInk).privateTitle(100)
                        .padding(.horizontal, 8).padding(.top, 50)
                }
                .frame(height: 90, alignment: .topLeading).frame(maxWidth: .infinity, alignment: .topLeading)
                .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(AccountColor(key: h.color).bright))
                .padding(8)
                Spacer(minLength: 0)
                VStack(alignment: .leading, spacing: 0) {
                    countdown(h).font(CalFont.font(.monoMedium, size: 13, relativeTo: .footnote)).foregroundStyle(Color.calInk).frame(height: 16)
                    if let p = h.place, !reasons.contains(.privacy) {
                        Text(p).font(CalFont.font(.sans, size: 12, relativeTo: .caption)).foregroundStyle(Color.calInk2).lineLimit(1).frame(height: 16)
                    }
                }
                .padding(.horizontal, 16).padding(.bottom, 16)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .accessibilityElement(children: .ignore).accessibilityLabel(h.spoken)
        } else if let l = entry.layout, entry.stale != nil, entry.content.isEvent {
            WidgetStateCardV2(title: "Saved events", line: entry.stale ?? "", needsAttention: false)
        } else if let card = WidgetCopy.card(entry.content) {
            WidgetStateCardV2(title: card.title, line: card.line, needsAttention: card.attention)
        }
    }

    /// Words beyond an hour, a live `Text(timerInterval:)` inside it ("IN 7:52", "22:00 LEFT").
    @ViewBuilder private func countdown(_ h: DayLayout.Hero) -> some View {
        let now = entry.date
        if h.live, h.end > now {
            HStack(spacing: 4) { timer(now, h.end); Text("LEFT") }
        } else if h.start.timeIntervalSince(now) <= 3600, h.start > now {
            HStack(spacing: 4) { Text("IN"); timer(now, h.start) }
        } else {
            Text(h.sub)
        }
    }

    private func timer(_ from: Date, _ to: Date) -> some View {
        Text(timerInterval: from...max(to, from.addingTimeInterval(1)), countsDown: true, showsHours: to.timeIntervalSince(from) >= 3600)
            .monospacedDigit().multilineTextAlignment(.leading).frame(maxWidth: 64, alignment: .leading)
    }
}

private extension WidgetEntryData.Content {
    var isEvent: Bool { if case .event = self { true } else { false } }
}

/// Small date widget: mono caps weekday, day number 44, "6 LEFT TODAY". Opens Home on today.
public struct DateSmallView: View {
    let weekday: String
    let day: String
    let left: Int?
    public init(date: Date, tz: TimeZone, locale: Locale = .current, left: Int? = nil) {
        let f = DateFormatter(); f.locale = locale; f.timeZone = tz
        f.setLocalizedDateFormatFromTemplate("EEEE"); weekday = f.string(from: date)
        f.setLocalizedDateFormatFromTemplate("d"); day = f.string(from: date)
        self.left = left
    }
    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(weekday).widgetCaps(12, tracking: 1.7).foregroundStyle(Color.calInk2).lineLimit(1)
            Text(day).font(CalFont.font(.sansMedium, size: 44, relativeTo: .largeTitle)).foregroundStyle(Color.calInk).padding(.top, 4)
            Spacer(minLength: 0)
            if let left { Text("\(left) LEFT TODAY").widgetCaps(11, tracking: 1.1).foregroundStyle(Color.calInk2) }
        }
        .padding(16)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityElement(children: .combine)
    }
}

// MARK: Lock-screen accessories (rev 2: monochrome, text-led, no account colour)

/// Titles are hidden while the phone is locked unless the widget setting says otherwise.
private struct LockTitle: ViewModifier {
    let show: Bool
    @Environment(\.redactionReasons) private var reasons
    func body(content: Content) -> some View {
        if reasons.contains(.privacy) && !show { Text("Event") } else { content }
    }
}

private extension View {
    func lockTitle(show: Bool) -> some View { show ? AnyView(self) : AnyView(modifier(LockTitle(show: false)).privacySensitive()) }
}

/// Clock glyph: ring with two hands, 11 pt. A small dot replaces the hands' tip when an account needs sign in.
private struct ClockGlyph: View {
    var attention = false
    var body: some View {
        ZStack {
            Circle().strokeBorder(lineWidth: 1.5)
            Path { p in p.move(to: CGPoint(x: 5.5, y: 2.5)); p.addLine(to: CGPoint(x: 5.5, y: 5.5)); p.addLine(to: CGPoint(x: 8, y: 5.5)) }
                .stroke(style: StrokeStyle(lineWidth: 1.5, lineCap: .round, lineJoin: .round))
            if attention { Circle().frame(width: 4, height: 4).offset(x: 4, y: -4) }
        }
        .frame(width: 11, height: 11).widgetAccentable()
    }
}

public struct AccessoryRectangularView: View {
    let entry: WidgetEntryData
    let showTitles: Bool
    public init(entry: WidgetEntryData, showTitles: Bool = false) { self.entry = entry; self.showTitles = showTitles }

    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            switch entry.content {
            case .event where entry.stale == nil:
                if let h = entry.layout?.hero { event(h) } else { plain("TUE", "") }
            case .needsSignIn(let a):
                line1(attention: true, left: "ACCOUNT", right: nil); line2("Sign in to \(a)"); line3("Saved events shown")
            case .done(let n):
                line1(left: entry.layout?.header ?? "", right: entry.layout.map { "\($0.todayLeft == 0 ? 0 : $0.todayLeft) LEFT" } == "0 LEFT" ? "DONE" : nil)
                line2("Done for today"); line3(n.map { "Next \($0.when) \($0.title)" } ?? "Nothing else planned")
            case .nothingPlanned:
                line1(left: entry.layout?.header ?? "", right: nil); line2("Nothing planned"); line3("No events today")
            case .noAccounts:
                line1(attention: true, left: "CALENDAR", right: nil); line2("Connect an account"); line3("Open Calendar")
            case .event:
                if let r = entry.layout?.today.first ?? nil {
                    line1(left: r.time, right: entry.stale?.uppercased()); line2(r.title, redact: true); line3("")
                } else { line1(left: entry.layout?.header ?? "", right: entry.stale?.uppercased()); line2("Saved events"); line3("") }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(entry.layout?.hero?.spoken ?? WidgetCopy.card(entry.content)?.title ?? "Calendar")
    }

    private func event(_ h: DayLayout.Hero) -> some View {
        let right: String = h.live ? "NOW" : h.sub
        return Group {
            line1(left: h.live ? "NOW" : timeText(h), right: h.live ? h.sub : (h.sub == "NOW" ? nil : h.sub))
            line2(h.title, redact: true)
            line3(h.place ?? h.badgeLong.map { $0.capitalized } ?? "", redact: true)
        }
        .accessibilityLabel(right)
    }

    private func timeText(_ h: DayLayout.Hero) -> String { h.time }

    private func line1(attention: Bool = false, left: String, right: String?) -> some View {
        HStack(spacing: 6) {
            ClockGlyph(attention: attention)
            Text(left).font(CalFont.font(.monoMedium, size: 11, relativeTo: .caption2)).lineLimit(1)
            Spacer(minLength: 0)
            if let right { Text(right).font(CalFont.font(.mono, size: 10, relativeTo: .caption2)).tracking(0.6).lineLimit(1).opacity(0.62) }
        }
        .frame(height: 14)
    }

    private func line2(_ t: String, redact: Bool = false) -> some View {
        Text(t).font(CalFont.font(.sansMedium, size: 17, relativeTo: .headline)).lineLimit(1).frame(height: 20).padding(.top, 4)
            .lockTitle(show: !redact || showTitles)
    }

    private func line3(_ t: String, redact: Bool = false) -> some View {
        Text(t).font(CalFont.font(.sans, size: 12, relativeTo: .caption)).lineLimit(1).opacity(0.62).frame(height: 14).padding(.top, 4)
            .lockTitle(show: !redact || showTitles)
    }

    private func plain(_ a: String, _ b: String) -> some View { line1(left: a, right: nil) }
}

/// Circular: ring = elapsed share of the 30 minutes before the start, then of the event; minutes in the middle.
public struct AccessoryCircularView: View {
    let entry: WidgetEntryData
    public init(entry: WidgetEntryData) { self.entry = entry }

    public var body: some View {
        if case .event = entry.content, entry.stale == nil, let h = entry.layout?.hero {
            let now = entry.date
            let mins = max(Int(((h.live ? h.end : h.start).timeIntervalSince(now) / 60).rounded(.up)), 0)
            let total: Double = h.live ? max(h.end.timeIntervalSince(h.start), 1) : 1800
            let done: Double = h.live ? now.timeIntervalSince(h.start) : max(1800 - h.start.timeIntervalSince(now), 0)
            Gauge(value: min(max(done / total, 0), 1)) {
                EmptyView()
            } currentValueLabel: {
                VStack(spacing: 0) {
                    Text(mins >= 60 ? "\(mins / 60)" : "\(mins)").font(CalFont.font(.monoMedium, size: 26, relativeTo: .title2)).minimumScaleFactor(0.6)
                    Text(mins >= 60 ? "H" : (h.live ? "LEFT" : "MIN")).font(CalFont.font(.mono, size: 9, relativeTo: .caption2)).tracking(0.9)
                }
            }
            .gaugeStyle(.accessoryCircularCapacity)
            .accessibilityLabel(h.spoken)
        } else if let l = entry.layout {
            // Events left today: ring = share still to come.
            let left = l.todayLeft
            Gauge(value: left == 0 ? 0 : 1) {
                EmptyView()
            } currentValueLabel: {
                VStack(spacing: 0) {
                    Text("\(left)").font(CalFont.font(.monoMedium, size: 26, relativeTo: .title2))
                    Text("LEFT").font(CalFont.font(.mono, size: 9, relativeTo: .caption2)).tracking(0.9)
                }
            }
            .gaugeStyle(.accessoryCircularCapacity)
            .accessibilityLabel("\(left) events left today")
        } else {
            Image(systemName: "calendar")
        }
    }
}

/// Inline: glyph and one line, "10:00 Roadmap review · 8 min". The countdown goes first when it does not fit.
public struct AccessoryInlineView: View {
    let entry: WidgetEntryData
    let showTitles: Bool
    public init(entry: WidgetEntryData, showTitles: Bool = false) { self.entry = entry; self.showTitles = showTitles }
    public var body: some View {
        if case .event = entry.content, entry.stale == nil, let h = entry.layout?.hero {
            ViewThatFits {
                Text("\(h.time) \(h.title) \u{00B7} \(short(h))").lockTitle(show: showTitles)
                Text("\(h.time) \(h.title)").lockTitle(show: showTitles)
            }
        } else if case .done = entry.content { Text("Done for today") }
        else if case .needsSignIn = entry.content { Text("Calendar: sign in") }
        else if case .noAccounts = entry.content { Text("Calendar: connect an account") }
        else if let t = WidgetCopy.card(entry.content)?.title { Text(t) }
    }
    private func short(_ h: DayLayout.Hero) -> String { h.sub.lowercased().replacingOccurrences(of: "in ", with: "") }
}

// MARK: Medium B and Large Day (options of the widget settings)

/// Medium layout B: bright "NEXT" panel on the left, four rows on the right.
struct MediumNextPanelView: View {
    let entry: WidgetEntryData
    let layout: DayLayout
    @Environment(\.redactionReasons) private var reasons

    var body: some View {
        let h = layout.hero!
        let fill = WidgetFill.rows(DayLayout(header: layout.header, tomorrowHeader: layout.tomorrowHeader, allDay: layout.allDay, hero: h,
                                             today: layout.today, tomorrow: layout.tomorrow, later: layout.later, todayLeft: layout.todayLeft), slots: 4)
        ZStack(alignment: .topLeading) {
            panel(h)
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 8) {
                    Text(layout.header).widgetCaps().foregroundStyle(Color.calInk2)
                    AllDayLine(chips: layout.allDay)
                    Spacer(minLength: 0)
                }
                .padding(.leading, 8).padding(.trailing, 16).padding(.top, 12).frame(height: 28, alignment: .top)
                ForEach(Array(fill.rows.enumerated()), id: \.offset) { n, r in
                    switch r {
                    case .event(let row): WidgetListRow(row: row, first: n == 0, compact: true)
                    case .label(let t): WidgetGroupLabel(text: t).padding(.leading, -8)
                    }
                }
                Spacer(minLength: 0)
                if fill.overflow > 0 {
                    Text("+\(fill.overflow) MORE").widgetCaps().foregroundStyle(Color.calInk2)
                        .frame(maxWidth: .infinity, alignment: .trailing).padding(.trailing, 16).frame(height: 22).padding(.bottom, 7)
                }
            }
            .padding(.leading, 148).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
    }

    private func panel(_ h: DayLayout.Hero) -> some View {
        ZStack(alignment: .topLeading) {
            Text("NEXT").font(CalFont.font(.mono, size: 10, relativeTo: .caption2)).tracking(1.4).opacity(0.7)
                .offset(x: 8, y: 8)
            FlipCells(h.time, size: .small, onBlock: true).offset(x: 8, y: 26)
            Text(h.title).font(CalFont.font(.sansMedium, size: 15, relativeTo: .headline)).lineLimit(2).frame(width: 116, alignment: .leading)
                .privateTitle(90).offset(x: 8, y: 64)
            HStack {
                Text(h.sub.replacingOccurrences(of: "REPLY NEEDED \u{00B7} ", with: "")).font(CalFont.font(.monoMedium, size: 11, relativeTo: .caption2)).lineLimit(1)
                Spacer(minLength: 0)
                if let url = h.joinURL {
                    Link(destination: url) {
                        Text("Join").font(CalFont.font(.sansMedium, size: 13, relativeTo: .body)).foregroundStyle(WidgetTheme.joinInk)
                            .padding(.horizontal, 12).frame(height: 28).background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(WidgetTheme.blockInk))
                    }
                }
            }
            .padding(.horizontal, 8).frame(width: 132, height: 28).offset(y: 142 - 8 - 28)
        }
        .foregroundStyle(WidgetTheme.blockInk)
        .frame(width: 132, height: 142, alignment: .topLeading)
        .background(RoundedRectangle(cornerRadius: 16, style: .continuous).fill(AccountColor(key: h.color).bright))
        .padding(8)
        .accessibilityElement(children: .ignore).accessibilityLabel(h.spoken)
    }
}

/// Large layout Day: timeline on the left (33 pt per hour from now), "UP NEXT" on the right.
struct DayLargeView: View {
    let entry: WidgetEntryData
    let layout: DayLayout
    let timeline: DayTimeline
    @Environment(\.redactionReasons) private var reasons
    private let pph: CGFloat = 33
    private let areaX: CGFloat = 26, areaW: CGFloat = 150

    private func y(_ d: Date) -> CGFloat { CGFloat(d.timeIntervalSince(timeline.windowStart) / 3600) * pph }

    var body: some View {
        ZStack(alignment: .topLeading) {
            VStack(alignment: .leading, spacing: 3) {
                Text(layout.header).widgetCaps().foregroundStyle(Color.calInk2)
                AllDayLine(chips: layout.allDay)
            }
            .padding(.leading, 16).padding(.top, 14)
            timelineView.frame(width: 176, height: pph * CGFloat(DayTimeline.hours), alignment: .topLeading).offset(x: 16, y: 56)
            Rectangle().fill(WidgetTheme.line).frame(width: 1, height: 280).offset(x: 204, y: 56)
            VStack(alignment: .leading, spacing: 0) {
                Text("UP NEXT").widgetCaps().foregroundStyle(Color.calInk2).frame(height: 20, alignment: .leading)
                ForEach(Array(layout.today.prefix(5).enumerated()), id: \.element.id) { n, r in
                    Link(destination: SurfaceLink.event(r.id).url) {
                        HStack(alignment: .top, spacing: 8) {
                            AccountBar(color: r.color, height: 32)
                            VStack(alignment: .leading, spacing: 2) {
                                HStack(spacing: 4) {
                                    Text(r.time).font(CalFont.font(.mono, size: 10, relativeTo: .caption2)).foregroundStyle(Color.calInk2)
                                    if let s = r.status {
                                        if r.statusKind == .needsYou { Text(s).font(CalFont.font(.monoMedium, size: 9, relativeTo: .caption2)).foregroundStyle(WidgetTheme.blockInk)
                                            .padding(.horizontal, 4).padding(.vertical, 1).background(RoundedRectangle(cornerRadius: 4).fill(WidgetTheme.amber)) }
                                        else { Text(s).font(CalFont.font(.mono, size: 9, relativeTo: .caption2)).foregroundStyle(Color.calInk2) }
                                    }
                                }
                                Text(r.title).font(CalFont.font(.sans, size: 13, relativeTo: .body)).lineLimit(1).foregroundStyle(Color.calInk).privateTitle(70)
                            }
                            Spacer(minLength: 0)
                        }
                        .frame(height: 50, alignment: .top).padding(.top, n == 0 ? 0 : 7)
                        .overlay(alignment: .top) { if n > 0 { Rectangle().fill(WidgetTheme.line).frame(height: 1) } }
                        .opacity(r.faded ? 0.45 : 1)
                    }
                }
            }
            .padding(.leading, 216).padding(.trailing, 16).padding(.top, 52)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    private var timelineView: some View {
        ZStack(alignment: .topLeading) {
            // Hour lines and labels.
            ForEach(Array(timeline.marks.enumerated()), id: \.offset) { _, m in
                let t = m.at
                ZStack(alignment: .topLeading) {
                    Rectangle().fill(WidgetTheme.line).frame(height: 1).padding(.leading, 0)
                    Text(m.label)
                        .font(CalFont.font(.mono, size: 9, relativeTo: .caption2)).foregroundStyle(Color.calInk2)
                        .frame(width: 20, alignment: .leading).background(WidgetTheme.container).offset(y: -6)
                }
                .offset(y: y(t))
            }
            // Past time, hatched.
            if entry.date > timeline.windowStart {
                Hatch().frame(width: areaW, height: max(y(entry.date), 0)).offset(x: areaX)
            }
            ForEach(timeline.blocks) { b in
                let colW = (areaW - CGFloat(b.columns - 1) * 2) / CGFloat(b.columns)
                let top = max(y(b.start), 0), bottom = min(y(b.end), pph * CGFloat(DayTimeline.hours))
                eventBlock(b, height: max(bottom - top, 14))
                    .frame(width: colW, height: max(bottom - top, 14), alignment: .topLeading)
                    .offset(x: areaX + CGFloat(b.column) * (colW + 2), y: top)
            }
            if entry.date >= timeline.windowStart, entry.date <= timeline.windowEnd {
                ZStack(alignment: .leading) {
                    Rectangle().fill(WidgetTheme.amber).frame(height: 1)
                    Circle().fill(WidgetTheme.amber).frame(width: 7, height: 7).offset(x: -3)
                }
                .frame(width: areaW).offset(x: areaX, y: y(entry.date))
            }
        }
    }

    @ViewBuilder private func eventBlock(_ b: DayTimeline.Block, height: CGFloat) -> some View {
        Link(destination: SurfaceLink.event(b.id).url) {
            VStack(alignment: .leading, spacing: 0) {
                Text(b.title).font(CalFont.font(.sans, size: 11, relativeTo: .caption)).lineLimit(height > 30 && b.columns > 1 ? 2 : 1).privateTitle(50)
                if height >= 30, b.columns == 1 {
                    Text(b.time).font(CalFont.font(.sans, size: 10, relativeTo: .caption2)).foregroundStyle(b.isNext ? WidgetTheme.blockInk.opacity(0.7) : Color.calInk2)
                }
            }
            .foregroundStyle(b.isNext ? WidgetTheme.blockInk : Color.calInk)
            .padding(.leading, b.isNext ? 6 : 13).padding(.trailing, 6).padding(.top, 2)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .background(RoundedRectangle(cornerRadius: 4).fill(b.isNext ? AccountColor(key: b.color).bright : WidgetTheme.event))
            .overlay(alignment: .leading) {
                if !b.isNext { RoundedRectangle(cornerRadius: 2).fill(AccountColor(key: b.color).color).frame(width: 3).padding(.vertical, 4).padding(.leading, 4) }
            }
        }
    }
}

private struct Hatch: View {
    var body: some View {
        Canvas { ctx, size in
            var p = Path()
            var x: CGFloat = -size.height
            while x < size.width { p.move(to: CGPoint(x: x, y: size.height)); p.addLine(to: CGPoint(x: x + size.height, y: 0)); x += 6 }
            ctx.stroke(p, with: .color(WidgetTheme.line), lineWidth: 1)
        }
    }
}

/// State card on the v2 grid (inset 16): title 19 Medium, one dim line; "needs you" becomes a filled amber tag.
struct WidgetStateCardV2: View {
    let title: String
    let line: String
    let needsAttention: Bool
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(needsAttention ? "Showing saved events" : title)
                .font(CalFont.font(.sansMedium, size: needsAttention ? 17 : 19, relativeTo: .headline)).foregroundStyle(Color.calInk)
            if needsAttention { NeedsTag(text: line) }
            else { Text(line).font(CalFont.font(.sans, size: 14, relativeTo: .footnote)).foregroundStyle(Color.calInk2) }
        }
        .padding(WidgetGrid.inset)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .accessibilityElement(children: .combine)
    }
}
