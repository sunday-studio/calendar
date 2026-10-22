import CalendarCore
import CalendarUI
import SwiftUI

/// Find free time: ask (q-ask), slots (q-slots), book (q-book), no gap (q-none).
struct FreeTimeFlow: View {
    @State var model: FreeTimeViewModel
    let workingHours: WorkingHours?
    let busyBlocks: (Date) async -> [DayGlance.Block]
    let close: () -> Void
    let done: (String, (@MainActor () async -> Void)?) -> Void
    @State private var blocks: [DayGlance.Block] = []
    @FocusState private var focused: Bool

    var body: some View {
        Group {
            switch model.phase {
            case .ask, .searching: ask
            case .slots(let r): slots(r)
            case .none(let r): none(r)
            case .failed(let m): failed(m)
            case .booking(let s): book(s)
            case .booked: Color.calBg
            }
        }
        .task { if !model.text.isEmpty, model.phase == .ask { await model.search() } }
        .onChange(of: model.phase) { _, p in
            if p == .booked { done(model.bookedToast, { [model] in _ = await model.undoBooking() }) }
        }
    }

    // q-ask
    private var ask: some View {
        VStack(spacing: 0) {
            ScreenNav("Find free time", leading: .close, onLeading: close)
            if model.phase == .searching { ProgressRule().accessibilityLabel("Searching") }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    LabeledField("Ask", isFocused: focused) {
                        TextField("When am I free for 2 hours this week?", text: $model.text, axis: .vertical)
                            .lineLimit(1...3).focused($focused).submitLabel(.search)
                            .onSubmit { Task { await model.search() } }
                            .accessibilityIdentifier("freetime.field")
                    }
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
                    if model.phase == .searching {
                        Text("Looking across your accounts, inside your working hours.").font(CalFont.hint).foregroundStyle(Color.calInk2)
                            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
                    }
                    Text("Try").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 28).padding(.bottom, 4)
                    ForEach(Array(FreeTimeViewModel.suggestions.enumerated()), id: \.offset) { n, s in
                        Button { model.use(suggestion: s) } label: {
                            Text(s).font(CalFont.body).foregroundStyle(Color.calInk2)
                                .padding(.horizontal, Spacing.gutter).frame(maxWidth: .infinity, minHeight: 52, alignment: .leading)
                                .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar { CalButton("Find time") { Task { await model.search() } }.disabled(!model.canSearch).accessibilityIdentifier("freetime.find") }
        }
        .onAppear { focused = model.text.isEmpty }
    }

    // q-slots
    private func slots(_ r: FreeTimeResult) -> some View {
        let best = r.slots.first(where: \.best) ?? r.slots[0]
        let chosen = model.selected ?? best
        return VStack(spacing: 0) {
            ScreenNav(model.ask?.title ?? "Free time", leading: .back, onLeading: { model.backToAsk() })
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    Text(model.subLine(workingHours: workingHours)).font(CalFont.font(.sans, size: 16, relativeTo: .body))
                        .foregroundStyle(Color.calInk2).padding(.horizontal, Spacing.gutter).padding(.bottom, Spacing.l)
                    highlight(best, r)
                    let others = r.slots.filter { $0.id != best.id }
                    if !others.isEmpty {
                        Text("Other times").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 28).padding(.bottom, 4)
                        ForEach(Array(others.enumerated()), id: \.element.id) { n, s in
                            Button { model.select(s) } label: { slotRow(s, selected: chosen.id == s.id, first: n == 0) }
                                .buttonStyle(.plain)
                        }
                    }
                    let day = DraftFormat.formatter("EEEd", model.tz, .current).string(from: chosen.start)
                    Text("\(day) at a glance").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 28).padding(.bottom, 10)
                    DayGlance(blocks: blocks, proposed: hours(chosen)).padding(.horizontal, Spacing.gutter)
                        .task(id: chosen.id) { blocks = await busyBlocks(chosen.start) }
                }
            }
        }
    }

    private func hours(_ s: FreeSlot) -> (start: Double, end: Double) {
        let cal = DayKey.calendar(model.tz)
        func h(_ d: Date) -> Double { Double(cal.component(.hour, from: d)) + Double(cal.component(.minute, from: d)) / 60 }
        return (h(s.start), h(s.end))
    }

    private func highlight(_ s: FreeSlot, _ r: FreeTimeResult) -> some View {
        let day = DraftFormat.formatter("EEEd", model.tz, .current), time = DraftFormat.formatter("jmm", model.tz, .current)
        return NextEventBlock(time: nil, title: "\(day.string(from: s.start)) \u{00B7} \(time.string(from: s.start)) to \(time.string(from: s.end))",
                              footer: s.best ? "Best fit, \(s.reason.lowercased())" : s.reason, color: .cobalt,
                              action: .join("Book"), onAction: { model.startBooking(s) })
            .accessibilityIdentifier("freetime.best")
    }

    private func slotRow(_ s: FreeSlot, selected: Bool, first: Bool) -> some View {
        ListRow(model.slotTitle(s), sub: s.reason, height: Spacing.rowHeightTall, isFirst: first) {
            Button { model.startBooking(s) } label: {
                Text("Book").font(CalFont.bodyMedium).foregroundStyle(Color.calAmber).frame(minWidth: Spacing.hit, minHeight: Spacing.hit)
            }
            .buttonStyle(.plain).accessibilityIdentifier("freetime.book")
        }
        .background(selected ? Color.calSurface : Color.clear)
    }

    // q-none
    private func none(_ r: FreeTimeResult) -> some View {
        VStack(spacing: 0) {
            ScreenNav(model.ask?.title ?? "Free time", leading: .back, onLeading: { model.backToAsk() })
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    QuietCard(title: "No \(FreeTimeAsk.durationWords(r.durationMin).replacingOccurrences(of: "hours", with: "hour").replacingOccurrences(of: "minutes", with: "minute")) gap",
                              footer: "Not \(model.ask?.windowWords.lowercased() ?? "then"), \(model.ignoredWorkingHours ? "at any time of day" : "inside your working hours").")
                        .accessibilityIdentifier("freetime.none")
                    if !r.closest.isEmpty {
                        Text("Closest").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 28).padding(.bottom, 4)
                        ForEach(Array(r.closest.enumerated()), id: \.element.id) { n, c in
                            ListRow(model.slotTitle(FreeSlot(start: c.start, end: c.end, reason: "")),
                                    sub: c.kind == .later ? "\(c.reason) \u{00B7} later" : "Only \(FreeTimeAsk.durationWords(c.durationMin)), \(c.reason)",
                                    height: Spacing.rowHeightTall, isFirst: n == 0) {
                                Button { model.startBooking(FreeSlot(start: c.start, end: c.end, reason: c.reason)) } label: {
                                    Text("Book").font(CalFont.bodyMedium).foregroundStyle(Color.calAmber).frame(minWidth: Spacing.hit, minHeight: Spacing.hit)
                                }.buttonStyle(.plain)
                            }
                        }
                    }
                    if !model.ignoredWorkingHours {
                        CalButton("Ignore working hours", kind: .outline) { Task { await model.ignoreWorkingHours() } }
                            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.xl).accessibilityIdentifier("freetime.ignore")
                    }
                }
            }
        }
    }

    private func failed(_ m: String) -> some View {
        VStack(spacing: 0) {
            ScreenNav("Find free time", leading: .back, onLeading: { model.backToAsk() })
            AmberBanner("Couldn\u{2019}t look.", message: m, actionTitle: "Try again", action: { Task { await model.search() } }, kind: .error)
            Spacer()
        }
    }

    // q-book
    private func book(_ s: FreeSlot) -> some View {
        VStack(spacing: 0) {
            ScreenNav("Book the slot", leading: .back, onLeading: { model.backToAsk(); Task { await model.search() } })
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    let day = DraftFormat.formatter("EEEd", model.tz, .current), time = DraftFormat.formatter("jmm", model.tz, .current)
                    NextEventBlock(time: nil, title: "\(day.string(from: s.start)) \u{00B7} \(time.string(from: s.start)) to \(time.string(from: s.end))",
                                   footer: FreeTimeAsk.durationWords(Int(s.end.timeIntervalSince(s.start) / 60)), color: .cobalt)
                    LabeledField("Title", isFocused: focused) {
                        TextField("Focus time", text: $model.bookTitle).focused($focused).accessibilityIdentifier("book.title")
                    }
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.xl)
                    if let e = model.bookError {
                        AmberBanner("Not booked.", message: e, kind: .error).padding(.top, Spacing.l).accessibilityIdentifier("book.error")
                    }
                    Text("Calendar").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 28).padding(.bottom, 4)
                    ForEach(Array(model.calendars.enumerated()), id: \.element.id) { n, c in
                        SheetRow(c.name, radio: model.bookCalendarId == c.id, isFirst: n == 0) { model.bookCalendarId = c.id }
                    }
                    KeyValueRow("Show me as", "Busy").padding(.top, Spacing.l)
                }
            }
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                CalButton("Book it") { Task { await model.book() } }.disabled(!model.canBook).accessibilityIdentifier("book.confirm")
                CalButton("Back to results", kind: .outline) { model.backToAsk(); Task { await model.search() } }
            }
        }
    }
}

/// Find free time on its own (from the search empty state).
public struct FreeTimeScreen: View {
    let model: FreeTimeViewModel
    let workingHours: WorkingHours?
    let busyBlocks: (Date) async -> [DayGlance.Block]
    let close: () -> Void
    let done: (String, (@MainActor () async -> Void)?) -> Void

    public init(model: FreeTimeViewModel, workingHours: WorkingHours?, busyBlocks: @escaping (Date) async -> [DayGlance.Block],
                close: @escaping () -> Void, done: @escaping (String, (@MainActor () async -> Void)?) -> Void) {
        self.model = model; self.workingHours = workingHours; self.busyBlocks = busyBlocks; self.close = close; self.done = done
    }

    public var body: some View {
        FreeTimeFlow(model: model, workingHours: workingHours, busyBlocks: busyBlocks, close: close, done: done)
            .background(Color.calBg)
    }
}
