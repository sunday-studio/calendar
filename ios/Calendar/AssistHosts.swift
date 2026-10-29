import CalendarCore
import CalendarUI
import FeatureAssist
import FeatureEvent
import FeatureHome
import SwiftUI

/// The FAB sheet: quick add first (typing, voice, free time), the full form on request. A long-press on an empty
/// hour (`start` set) goes straight to the form, as before.
struct NewEntryHost: View {
    enum Mode { case quick, form(Date?), prefill(EventDraft) }
    let env: AppEnvironment
    let writer: WriteCoordinator
    let api: APIClient
    let onClose: () -> Void
    @State private var mode: Mode
    @State private var quick: QuickAddViewModel?
    @State private var voice: VoiceViewModel?
    @State private var hours: WorkingHours?
    @State private var weekStart = 1
    @State private var cals: [HubCalendar] = []
    @State private var defaultCal: String?

    init(entry: NewEntry, env: AppEnvironment, writer: WriteCoordinator, api: APIClient, onClose: @escaping () -> Void) {
        self.env = env; self.writer = writer; self.api = api; self.onClose = onClose
        switch entry {
        case .blank: _mode = State(initialValue: .quick)
        case .at(let d): _mode = State(initialValue: .form(d))
        case .draft: _mode = State(initialValue: EventEditorViewModel.loadDraft().map { .prefill($0) } ?? .form(nil))
        }
    }

    private var tz: TimeZone { (try? env.db.userTimeZone()) ?? .current }

    var body: some View {
        content
            // Set here, on the sheet's own root, so the sheet is square and coloured from its first frame
            // (inside the loaded form they landed late: rounded and see-through while rising).
            .background(Color.calBg.ignoresSafeArea())
            .presentationBackground(Color.calBg)
            .presentationCornerRadius(0)
            .presentationDragIndicator(.hidden)
    }

    @ViewBuilder private var content: some View {
        switch mode {
        case .form(let start):
            EditorHost(kind: .new(start), env: env, writer: writer) { _ in onClose() }
        case .prefill(let draft):
            EditorHost(kind: .prefill(draft), env: env, writer: writer) { _ in onClose() }
        case .quick:
            Group {
                if let quick, let voice {
                    NewEventFlow(
                        model: quick, voice: voice, workingHours: hours,
                        makeFreeTime: { text in
                            FreeTimeViewModel(api: api, writer: writer, calendars: cals, defaultCalendarId: defaultCal,
                                              tz: tz, weekStart: weekStart, clock: env.clock, text: text)
                        },
                        busyBlocks: { await AssistData.busyBlocks(env: env, on: $0, tz: tz) },
                        actions: NewEventActions(
                            close: onClose,
                            fullForm: { mode = .form(nil) },
                            editDetails: { mode = .prefill($0) },
                            done: { message, undo in
                                onClose()
                                ToastCenter.shared.show(message, actionTitle: undo == nil ? nil : "Undo") {
                                    if let undo { Task { await undo() } }
                                }
                            }))
                } else {
                    Color.calBg.ignoresSafeArea()
                }
            }
            .task {
                guard quick == nil else { return }
                cals = (try? await env.calendars.calendars()) ?? []
                let s = try? await env.settings.settings()
                defaultCal = s?.defaultCalendarId; hours = s?.workingHours; weekStart = s?.weekStart ?? 1
                quick = QuickAddViewModel(api: api, writer: writer, tz: tz, clock: env.clock)
                voice = VoiceViewModel(engine: SystemSpeechTranscriber())
            }
        }
    }
}

/// Search pushed from Home's header.
struct SearchHost: View {
    let env: AppEnvironment
    let api: APIClient
    let writer: WriteCoordinator
    let openEvent: (String) -> Void
    let close: () -> Void
    @State private var model: SearchViewModel
    @State private var freeTime: FreeTimeViewModel?
    @State private var cals: [HubCalendar] = []
    @State private var defaultCal: String?

    init(env: AppEnvironment, api: APIClient, writer: WriteCoordinator, openEvent: @escaping (String) -> Void, close: @escaping () -> Void) {
        self.env = env; self.api = api; self.writer = writer; self.openEvent = openEvent; self.close = close
        let tz = (try? env.db.userTimeZone()) ?? .current
        _model = State(initialValue: SearchViewModel(api: api, tz: tz, clock: env.clock,
                                                     local: { q in await AssistData.savedMatches(env: env, q) }))
    }

    var body: some View {
        SearchScreen(model: model, openEvent: openEvent, findFreeTime: { text in
            freeTime = FreeTimeViewModel(api: api, writer: writer, calendars: cals, defaultCalendarId: defaultCal,
                                         tz: (try? env.db.userTimeZone()) ?? .current, clock: env.clock, text: text)
        }, close: close)
        .task {
            cals = (try? await env.calendars.calendars()) ?? []
            defaultCal = (try? await env.settings.settings())?.defaultCalendarId
        }
        .fullScreenCover(item: Binding(get: { freeTime.map(FreeTimeBox.init) }, set: { if $0 == nil { freeTime = nil } })) { box in
            FreeTimeOnly(model: box.model, env: env) { freeTime = nil }
        }
    }

    struct FreeTimeBox: Identifiable { let model: FreeTimeViewModel; var id: ObjectIdentifier { ObjectIdentifier(model) } }
}

/// Find free time started from the search empty state.
struct FreeTimeOnly: View {
    let model: FreeTimeViewModel
    let env: AppEnvironment
    let close: () -> Void
    var body: some View {
        FreeTimeScreen(model: model, workingHours: nil,
                       busyBlocks: { await AssistData.busyBlocks(env: env, on: $0, tz: model.tz) }, close: close) { message, undo in
            close()
            ToastCenter.shared.show(message, actionTitle: undo == nil ? nil : "Undo") { if let undo { Task { await undo() } } }
        }
    }
}

enum AssistData {
    /// Busy blocks of one day for the "at a glance" bar (09 to 18).
    static func busyBlocks(env: AppEnvironment, on date: Date, tz: TimeZone) async -> [DayGlance.Block] {
        let key = DayKey(date, in: tz)
        let cal = DayKey.calendar(tz)
        let items = (try? await env.agenda.items(from: key, to: key)) ?? []
        func h(_ d: Date) -> Double { Double(cal.component(.hour, from: d)) + Double(cal.component(.minute, from: d)) / 60 }
        return items.filter { !$0.allDay && $0.busy && $0.status != "cancelled" && $0.myResponse != "declined" }.compactMap { i in
            guard let s = i.start, let e = i.end else { return nil }
            return DayGlance.Block(startHour: h(s), endHour: min(h(e), 24), color: AccountColor(key: i.color))
        }
    }

    /// Offline search over the saved agenda (title, place).
    static func savedMatches(env: AppEnvironment, _ q: String) async -> [AgendaItem] {
        let tz = (try? env.db.userTimeZone()) ?? .current
        let today = DayKey(env.clock.now(), in: tz)
        let items = (try? await env.agenda.items(from: today.adding(days: -90), to: today.adding(days: 365))) ?? []
        let words = q.lowercased().split(separator: " ").map(String.init)
        return items.filter { i in
            let hay = (i.title + " " + (i.location ?? "")).lowercased()
            return words.allSatisfy { w in hay.split(whereSeparator: { !$0.isLetter && !$0.isNumber }).contains { $0.hasPrefix(w) } }
        }
    }
}
