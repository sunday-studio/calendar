import CalendarCore
import CalendarUI
import SwiftUI

/// Day and 3 days (boards v-day, v-three): hour grid at a 64pt pitch, event blocks in lanes,
/// hatched past and the amber now line for today.
enum BoardLayout: Equatable {
    case day, three

    var gutter: CGFloat { switch self { case .day: 64; case .three: 40 } }
    var trailing: CGFloat { self == .day ? 16 : 0 }
    var laneGap: CGFloat { switch self { case .day: 6; case .three: 3 } }
    var labelX: CGFloat { self == .day ? 16 : 2 }
    var dayCount: Int { switch self { case .day: 1; case .three: 3 } }
}

struct TimelineBoard: View {
    let model: DayViewModel
    let layout: BoardLayout
    let days: [DayKey]
    let openEvent: (String) -> Void
    /// Long-press on an empty hour.
    let newAt: (Date) -> Void
    let shift: (Int) -> Void
    /// Drag / resize: false while offline (then lifting says "Can't edit while offline").
    var canWrite = true
    /// Called on drop with the new times (the host writes, optimistically).
    var onCommit: (AgendaItem, Date, Date) -> Void = { _, _, _ in }
    var onBlocked: (String) -> Void = { _ in }
    var zoomNS: Namespace.ID?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    static let hourHeight: CGFloat = 64

    struct DragSession: Equatable {
        enum Mode { case move, resize }
        var mode: Mode
        var block: DayBlock
        var col: Int
        var originCol: Int
        var origStart: Date
        var origEnd: Date
        var start: Date
        var end: Date
        var top: CGFloat
        var height: CGFloat
        var originTop: CGFloat
        var moved = false
        var chip = ""
        var id: String { block.item.id }
    }
    @State private var drag: DragSession?
    /// "Move..." custom action: the block whose nudge sheet is open.
    @State private var nudging: DayBlock?
    @State private var position = ScrollPosition(y: 0)
    @State private var placedFor: DayKey?
    @State private var allDayExpanded = false

    private var tz: TimeZone { model.tz }
    private var first: DayKey { days[0] }
    private var grid: DayGrid { DayGrid(day: first, tz: tz, hourHeight: Self.hourHeight) }

    var body: some View {
        gridBody
    }

    private var gridBody: some View {
        VStack(spacing: 0) {
            if layout == .day { allDayStrip }
            if layout == .day, let z = model.secondaryTz { zoneLabels(z) }
            if layout != .day { columnHeader }
            GeometryReader { geo in
                ScrollView {
                    board(width: geo.size.width)
                        .frame(height: grid.height + 24)
                        .onAppear { boardWidth = geo.size.width }
                }
                .scrollPosition($position)
                .scrollDisabled(drag != nil)
                .onAppear { place() }
                .onChange(of: first) { _, _ in place() }
                .scrollIndicators(.hidden)
            }
        }
        .simultaneousGesture(
            DragGesture(minimumDistance: 40).onEnded { v in
                guard drag == nil, abs(v.translation.width) > abs(v.translation.height) * 1.6 else { return }   // never page while dragging an event
                shift(v.translation.width < 0 ? layout.dayCount : -layout.dayCount)
            }
        )
        .accessibilityIdentifier("board.\(layout)")
        .calSheet(isPresented: Binding(get: { nudging != nil }, set: { if !$0 { nudging = nil } })) { nudgeSheet }
    }

    @ViewBuilder private var nudgeSheet: some View {
        if let b = nudging, let s = b.item.start, let e = b.item.end {
            CalSheet(title: "Move \(b.item.title.isEmpty ? "event" : b.item.title)") {
                ForEach(Array(DragLogic.Nudge.allCases.enumerated()), id: \.element) { n, nudge in
                    if let r = DragLogic.nudged(start: s, end: e, by: nudge, tz: tz) {
                        SheetRow(nudge.title, isFirst: n == 0) { nudging = nil; onCommit(b.item, r.start, r.end) }
                    }
                }
            }
        }
    }

    /// VoiceOver / Switch Control: "Move..." replaces the long-press drag. Same rules (read-only, recurring pieces, offline).
    private func startNudge(_ b: DayBlock) {
        if let reason = DragLogic.blockReason(b.item, continues: b.continuesBefore || b.continuesAfter) { onBlocked(reason); return }
        guard canWrite else { onBlocked("Can\u{2019}t edit while offline"); return }
        nudging = b
    }

    // MARK: Header pieces

    @ViewBuilder private var allDayStrip: some View {
        let items = model.content(for: first).allDay
        if !items.isEmpty {
            // Two rows at most until asked: five all-day events used to push the hours off the screen.
            let shown = allDayExpanded || items.count <= 3 ? items : Array(items.prefix(2))
            VStack(spacing: 0) {
                ForEach(Array(shown.enumerated()), id: \.element.id) { n, i in
                    Button { openEvent(i.id) } label: {
                        HStack(spacing: 0) {
                            Text("All day").font(CalFont.time).foregroundStyle(Color.calInk2).lineLimit(1)
                                .frame(width: 72, alignment: .leading)
                            Text(i.title).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk).lineLimit(1)
                            Spacer(minLength: 0)
                        }
                        .padding(.horizontal, Spacing.gutter)
                        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                        .overlay(alignment: .leading) {
                            Rectangle().fill(AccountColor(key: i.color).color).frame(width: Spacing.colorBar).padding(.vertical, 10)
                        }
                        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
                        .overlay(alignment: .top) { if n == 0 { Rectangle().fill(Color.calLine).frame(height: 1) } }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(i.title), all day")
                }
                if items.count > 3 {
                    Button { withAnimation(HomeMotion.spring) { allDayExpanded.toggle() } } label: {
                        Text(allDayExpanded ? "Show fewer" : "\(items.count - 2) more all day")
                            .font(CalFont.font(.sans, size: 14, relativeTo: .footnote)).foregroundStyle(Color.calInk2)
                            .padding(.horizontal, Spacing.gutter).frame(maxWidth: .infinity, minHeight: 40, alignment: .leading)
                            .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier("board.allDayMore")
                }
            }
            .padding(.top, 14)
        }
    }

    private func zoneLabels(_ z: TimeZone) -> some View {
        ZStack(alignment: .topLeading) {
            Text(Self.short(tz)).calLabel().offset(x: 16)
            Text(Self.short(z)).calLabel().offset(x: 62)
        }
        .frame(maxWidth: .infinity, minHeight: 14, alignment: .topLeading)
        .padding(.top, 14).padding(.bottom, 8)
        .accessibilityLabel("\(Self.city(tz)) and \(Self.city(z))")
    }

    /// Short column label: the zone's abbreviation ("CEST", "EDT"), else its first letters.
    static func short(_ tz: TimeZone) -> String {
        let a = tz.abbreviation() ?? ""
        if !a.isEmpty, !a.hasPrefix("GMT+"), !a.hasPrefix("GMT-") { return a }
        return String(city(tz).prefix(3)).uppercased()
    }

    static func city(_ tz: TimeZone) -> String {
        (tz.identifier.split(separator: "/").last.map(String.init) ?? tz.identifier).replacingOccurrences(of: "_", with: " ")
    }

    private var columnHeader: some View {
        HStack(spacing: 0) {
            Color.clear.frame(width: layout.gutter, height: 52)
            ForEach(days, id: \.self) { d in
                let isToday = d == model.today
                Button { } label: {
                    VStack(spacing: 2) {
                        Text(String(d.rawValue.suffix(2).drop { $0 == "0" }))
                            .font(CalFont.font(.sansMedium, size: 17, relativeTo: .body).monospacedDigit())
                            .foregroundStyle(isToday ? Color.calAmber : Color.calInk)
                        Text(weekdayLabel(d)).font(CalFont.font(.sans, size: 12, relativeTo: .caption)).foregroundStyle(Color.calInk2)
                    }
                    .frame(maxWidth: .infinity, minHeight: 52)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("\(model.format.weekday(d)) \(model.format.dayMonth(d))")
            }
        }
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
    }

    private func weekdayLabel(_ d: DayKey) -> String {
        let names = MonthGrid.weekdayNames(weekStart: 0)   // Sun-first by weekday index
        let name = names[(d.weekday % 7 + 7) % 7]
        return name
    }

    // MARK: Board

    private func board(width: CGFloat) -> some View {
        let g = grid
        let colArea = width - layout.gutter - layout.trailing
        let colW = colArea / CGFloat(days.count)
        let zone = layout == .day ? model.secondaryTz : nil
        let blockX: CGFloat = zone != nil ? 116 : layout.gutter
        let dayW = width - blockX - layout.trailing
        return ZStack(alignment: .topLeading) {
            hourRows(g, width: width, zone: zone)
            Color.clear.contentShape(Rectangle()).frame(width: width, height: g.height)
                .onTapGesture { drag = nil }
                .gesture(longPress(colW: colW))
            if layout == .day, days.contains(model.today) { pastHatch(g, width: width) }
            ForEach(Array(days.enumerated()), id: \.element) { i, d in
                column(d, col: i, width: layout == .day ? dayW : colW - 4)
                    .offset(x: layout == .day ? blockX : layout.gutter + CGFloat(i) * colW + 2)
            }
            if layout == .day, days.contains(model.today) { nowRule(g, width: width) }
            if let s = drag { liftedOverlay(s, width: width) }
            if layout == .day, model.content(for: first).isEmpty, model.loaded { emptyHint }
        }
        .frame(width: width, height: g.height, alignment: .topLeading)
        .coordinateSpace(name: "board")
        .padding(.top, 10)
    }

    private func hourRows(_ g: DayGrid, width: CGFloat, zone: TimeZone?) -> some View {
        ZStack(alignment: .topLeading) {
            ForEach(g.hourMarks()) { m in
                Rectangle().fill(Color.calLine).frame(width: width, height: 1).offset(y: m.y)
                plate(DayLogic.hourLabel(m.hour, time24: model.prefs.time24), Color.calInk2, x: layout.labelX).offset(y: m.y - 9)
                if let zone {
                    plate(DayLogic.gutterLabel(m.date, zone: zone, locale: model.format.locale), Color.calInk3, x: 62).offset(y: m.y - 9)
                }
            }
        }
        .accessibilityHidden(true)
    }

    private func plate(_ s: String, _ c: Color, x: CGFloat) -> some View {
        Text(s).font(CalFont.label).foregroundStyle(c).padding(.horizontal, 4).background(Color.calBg).offset(x: x)
    }

    private func column(_ d: DayKey, col: Int, width: CGFloat) -> some View {
        let content = model.content(for: d)
        let g = DayGrid(day: d, tz: tz, hourHeight: Self.hourHeight)
        let isToday = d == model.today
        return ZStack(alignment: .topLeading) {
            if layout != .day, isToday {
                let y = g.y(for: model.now)
                HatchedShade().frame(width: width, height: min(max(y, 0), g.height))
                if layout == .three {
                    BrassLine(label: model.format.time(model.now)).frame(width: width + 4).offset(x: -2, y: y - 9)
                } else {
                    Rectangle().fill(Color.calAmber).frame(width: width + 4, height: 1).offset(x: -2, y: y)
                }
            }
            ForEach(content.blocks) { b in blockView(b, g: g, width: width, col: col) }
        }
        .frame(width: width, height: g.height, alignment: .topLeading)
    }

    private func blockView(_ b: DayBlock, g: DayGrid, width: CGFloat, col: Int) -> some View {
        let top = g.y(for: b.start) + 2
        let h = max(30, CGFloat(b.end.timeIntervalSince(b.start)) / 3600 * Self.hourHeight - 2)
        let count = max(b.columnCount, 1)
        let w = (width - layout.laneGap * CGFloat(count - 1)) / CGFloat(count)
        var item = timelineItem(b)
        let isGhost = drag?.id == b.item.id
        if isGhost { item.state = .ghost; item.subtitle = nil }
        return TimelineEventBlock(item, height: h, singleLine: layout == .three)
            .frame(width: w, height: h)
            .contentShape(Rectangle())
            .onTapGesture { if drag != nil { drag = nil } else { openEvent(b.item.id) } }
            .gesture(liftGesture(b, col: col, top: top, height: h))
            .modifier(BlockZoomSource(id: b.item.id, ns: zoomNS, reduceMotion: reduceMotion))
            .offset(x: CGFloat(b.column) * (w + layout.laneGap), y: top)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(DayLogic.accessibilityLabel(b, format: model.format))
            .accessibilityAddTraits(.isButton)
            .accessibilityAction(named: "Open event") { openEvent(b.item.id) }
            .accessibilityAction(named: "Move\u{2026}") { startNudge(b) }
            .accessibilityAction(named: "Edit time") { openEvent(b.item.id) }
    }

    // MARK: Drag and resize

    private func metrics(width: CGFloat) -> (x: (Int) -> CGFloat, w: CGFloat, colW: CGFloat) {
        let colW = (width - layout.gutter - layout.trailing) / CGFloat(days.count)
        let zone = layout == .day ? model.secondaryTz : nil
        let blockX: CGFloat = zone != nil ? 116 : layout.gutter
        let dayW = width - blockX - layout.trailing
        return ({ i in layout == .day ? blockX : layout.gutter + CGFloat(i) * colW + 2 }, layout == .day ? dayW : colW - 4, colW)
    }

    private func gridFor(_ col: Int) -> DayGrid { DayGrid(day: days[min(max(col, 0), days.count - 1)], tz: tz, hourHeight: Self.hourHeight) }

    private func liftGesture(_ b: DayBlock, col: Int, top: CGFloat, height: CGFloat) -> some Gesture {
        LongPressGesture(minimumDuration: 0.4)
            .sequenced(before: DragGesture(minimumDistance: 0, coordinateSpace: .named("board")))
            .onChanged { value in
                if case .first = value { Haptics.shared.prepare(for: .lift) }
                guard case .second(true, let d) = value else { return }
                if drag?.id != b.item.id {
                    if let reason = DragLogic.blockReason(b.item, continues: b.continuesBefore || b.continuesAfter) {
                        Haptics.shared.play(.blocked); onBlocked(reason); return
                    }
                    guard canWrite else { Haptics.shared.play(.blocked); onBlocked("Can\u{2019}t edit while offline"); return }
                    Haptics.shared.play(.lift)
                    drag = DragSession(mode: .move, block: b, col: col, originCol: col, origStart: b.item.start ?? b.start,
                                       origEnd: b.item.end ?? b.end, start: b.start, end: b.end, top: top, height: height, originTop: top,
                                       chip: DragLogic.moveChip(start: b.start, tz: tz, locale: model.format.locale))
                }
                if let d, var s = drag, s.mode == .move, s.id == b.item.id { updateMove(&s, drag: d); drag = s }
            }
            .onEnded { value in
                guard case .second(true, let d?) = value, var s = drag, s.id == b.item.id, s.mode == .move else { return }   // lifted, not moved: stay lifted
                updateMove(&s, drag: d)
                finish(s)
            }
    }

    private func updateMove(_ s: inout DragSession, drag d: DragGesture.Value) {
        let dy = d.location.y - d.startLocation.y
        if abs(dy) > 3 || abs(d.location.x - d.startLocation.x) > 3 { s.moved = true }
        guard s.moved else { return }
        let width = boardWidth
        let m = metrics(width: width)
        let col = layout == .day ? 0 : min(max(Int((d.location.x - layout.gutter) / max(m.colW, 1)), 0), days.count - 1)
        let g = gridFor(col)
        let duration = s.origEnd.timeIntervalSince(s.origStart)
        let r = DragLogic.moved(duration: duration, topY: s.originTop - 2 + dy, grid: g)
        if r.start != s.start || col != s.col { Haptics.shared.play(.snap) }
        s.col = col; s.start = r.start; s.end = r.end
        s.top = g.y(for: r.start) + 2
        s.chip = DragLogic.moveChip(start: r.start, tz: tz, locale: model.format.locale)
    }

    private func finish(_ s: DragSession) {
        withAnimation(HomeMotion.spring) { drag = nil }
        guard s.moved, s.start != s.origStart || s.col != s.originCol else { return }
        Haptics.shared.play(.drop)
        onCommit(s.block.item, s.start, s.end)
    }

    private func resizeGesture(_ s: DragSession) -> some Gesture {
        DragGesture(minimumDistance: 0, coordinateSpace: .named("board"))
            .onChanged { d in
                guard var cur = drag, cur.id == s.id else { return }
                let g = gridFor(cur.col)
                let bottom = s.top + s.height + d.translation.height
                let end = DragLogic.resizedEnd(start: cur.start, bottomY: bottom, grid: g)
                if end != cur.end { Haptics.shared.play(.snap) }
                cur.mode = .resize; cur.moved = true; cur.end = end
                cur.height = max(30, CGFloat(end.timeIntervalSince(cur.start)) / 3600 * Self.hourHeight - 2)
                cur.chip = DragLogic.resizeChip(start: cur.start, end: end, tz: tz, locale: model.format.locale)
                drag = cur
            }
            .onEnded { _ in
                guard let cur = drag else { return }
                withAnimation(HomeMotion.spring) { drag = nil }
                if cur.end != cur.origEnd { Haptics.shared.play(.drop); onCommit(cur.block.item, cur.start, cur.end) }
            }
    }

    @State private var boardWidth: CGFloat = 390

    private func liftedOverlay(_ s: DragSession, width: CGFloat) -> some View {
        let m = metrics(width: width)
        var item = timelineItem(s.block)
        item.state = .lifted
        let x = m.x(s.col)
        let chipAbove = s.mode == .move && s.top - 32 >= 0
        return ZStack(alignment: .topLeading) {
            TimelineEventBlock(item, height: s.height, singleLine: layout == .three)
                .frame(width: m.w, height: s.height)
                .shadow(color: .black.opacity(0.4), radius: 12, y: 8)
            if s.mode == .resize || s.moved == false || s.mode == .move {
                Rectangle().fill(Color.calInk).frame(width: 28, height: 3)
                    .frame(width: 44, height: 44)
                    .contentShape(Rectangle())
                    .gesture(resizeGesture(s))
                    .offset(x: (m.w - 44) / 2, y: s.height - 6 - 22)
                    .accessibilityLabel("Change length")
            }
        }
        .frame(width: m.w, height: s.height, alignment: .topLeading)
        .overlay(alignment: .topLeading) {
            DragChip(s.chip)
                .offset(x: 2, y: chipAbove ? -32 : (s.mode == .resize ? s.height + 6 : s.height + 6))
                .fixedSize()
        }
        .offset(x: x, y: s.top)
        .zIndex(10)
    }

    private func timelineItem(_ b: DayBlock) -> TimelineItem {
        let i = b.item
        var sub = layout == .day ? (i.start.map { model.format.time($0) } ?? "") : ""
        if layout == .day, let word = HomeLogic.statusWord(i), word == .needsReply { sub += " \u{00B7} reply needed" }
        let state: TimelineItem.State = i.myResponse == "declined" ? .declined
            : (i.myResponse == "tentative" || i.status == "tentative") ? .tentative : .normal
        return TimelineItem(id: i.id, title: i.title.isEmpty ? "(No title)" : i.title, subtitle: sub,
                            startMinute: 0, endMinute: 1, color: AccountColor(key: i.color), state: state)
    }

    private func pastHatch(_ g: DayGrid, width: CGFloat) -> some View {
        let y = g.y(for: model.now)
        return HatchedShade().frame(width: width, height: min(max(y, 0), g.height)).allowsHitTesting(false)
    }

    private func nowRule(_ g: DayGrid, width: CGFloat) -> some View {
        BrassLine(label: model.format.time(model.now)).frame(width: width)
            .offset(y: g.y(for: model.now) - 9).allowsHitTesting(false)
    }

    private var emptyHint: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Nothing planned").font(CalFont.title).foregroundStyle(Color.calInk)
            Text("Tap and hold an empty hour to add an event there.")
                .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
        }
        .padding(.horizontal, Spacing.gutter).padding(.top, 220)
        .frame(maxWidth: .infinity, alignment: .leading)
        .allowsHitTesting(false)
    }

    // MARK: Gestures + scroll

    private func longPress(colW: CGFloat) -> some Gesture {
        LongPressGesture(minimumDuration: 0.4).sequenced(before: DragGesture(minimumDistance: 0))
            .onEnded { value in
                guard case .second(true, let drag?) = value else { return }
                let col = layout == .day ? 0 : min(max(Int((drag.location.x - layout.gutter) / max(colW, 1)), 0), days.count - 1)
                let dg = DayGrid(day: days[col], tz: tz, hourHeight: Self.hourHeight)
                newAt(TimeSnap.snap(dg.date(atY: drag.location.y), step: 3600, mode: .down))
            }
    }

    private func place() {
        guard placedFor != first else { return }
        placedFor = first
        let isToday = days.contains(model.today)
        let ref = isToday ? model.today : first
        let y = DayLogic.initialScrollY(day: ref, isToday: isToday, content: model.content(for: ref), now: model.now, grid: grid)
        // The scroll view has no content size on the first frame, so a scroll issued now is clamped to the top
        // (the day opened at 00:00 on a phone at 18:46). Scroll once it has laid out.
        position.scrollTo(y: y)
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(60))
            position.scrollTo(y: y)
        }
    }
}


private struct BlockZoomSource: ViewModifier {
    let id: String
    let ns: Namespace.ID?
    let reduceMotion: Bool
    func body(content: Content) -> some View {
        if let ns { content.zoomSource(id, in: ns, enabled: !reduceMotion) } else { content }
    }
}
