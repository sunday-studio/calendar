import CalendarCore
import CalendarUI
import SwiftUI

/// What the host (the app) does when the flow ends or hands over to the full form.
public struct NewEventActions {
    public var close: () -> Void
    public var fullForm: () -> Void
    public var editDetails: (EventDraft) -> Void
    /// Created or booked: close, then show the toast with Undo.
    public var done: (_ message: String, _ undo: (@MainActor () async -> Void)?) -> Void

    public init(close: @escaping () -> Void, fullForm: @escaping () -> Void, editDetails: @escaping (EventDraft) -> Void,
                done: @escaping (String, (@MainActor () async -> Void)?) -> Void) {
        self.close = close; self.fullForm = fullForm; self.editDetails = editDetails; self.done = done
    }
}

/// The new-event sheet (canvas h-new) and everything it leads to: typing, voice, preview, choices, created,
/// "alfather isn't answering", Find free time and Book the slot.
public struct NewEventFlow: View {
    @State private var model: QuickAddViewModel
    @State private var voice: VoiceViewModel
    @State private var freeTime: FreeTimeViewModel?
    @State private var showVoice = false
    @State private var pickTime = false
    @State private var pickedTime = Date()
    @FocusState private var focused: Bool
    @Environment(\.openURL) private var openURL
    private let makeFreeTime: (String) -> FreeTimeViewModel
    private let busyBlocks: (Date) async -> [DayGlance.Block]
    private let workingHours: WorkingHours?
    private let actions: NewEventActions

    public init(model: QuickAddViewModel, voice: VoiceViewModel, workingHours: WorkingHours?,
                makeFreeTime: @escaping (String) -> FreeTimeViewModel,
                busyBlocks: @escaping (Date) async -> [DayGlance.Block], actions: NewEventActions) {
        _model = State(initialValue: model); _voice = State(initialValue: voice)
        self.makeFreeTime = makeFreeTime; self.busyBlocks = busyBlocks; self.workingHours = workingHours; self.actions = actions
    }

    public var body: some View {
        Group {
            if let ft = freeTime {
                FreeTimeFlow(model: ft, workingHours: workingHours, busyBlocks: busyBlocks,
                             close: { freeTime = nil },
                             done: { actions.done($0, $1) })
            } else {
                quick
            }
        }
        .background(Color.calBg)
        .presentationBackground(Color.calBg)
        .presentationCornerRadius(0)
        .presentationDragIndicator(.hidden)
        .calSheet(isPresented: $showVoice) { voiceSheet }
        .onChange(of: model.stage) { _, s in
            if s == .created { actions.done(model.createdToast, { [model] in _ = await model.undoCreate() }) }
        }
    }

    // MARK: Quick add screens

    @ViewBuilder private var quick: some View {
        switch model.stage {
        case .entry: entry
        case .preview, .creating: preview
        case .ambiguity: ambiguity
        case .unreachable: unreachable
        case .created: Color.calBg
        }
    }

    private var echo: AttributedString { UnderlineSpan.attributed(model.text, spans: model.underlines) }

    // q-type
    private var entry: some View {
        VStack(spacing: 0) {
            ScreenNav("New event", leading: .close, onLeading: actions.close)
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    HStack(alignment: .top, spacing: 0) {
                        TextField("Add an event", text: $model.text, axis: .vertical)
                            .lineLimit(1...4)
                            .font(CalFont.body).foregroundStyle(Color.calInk).tint(Color.calAmber)
                            .focused($focused).submitLabel(.go)
                            .onSubmit { Task { await model.submit() } }
                            .padding(.leading, Spacing.l).padding(.vertical, 16)
                            .accessibilityIdentifier("quickadd.field")
                        Button { Task { showVoice = true; await voice.begin() } } label: {
                            Image(systemName: "mic").font(.system(size: 18, weight: .regular)).foregroundStyle(Color.calInk)
                                .frame(width: Spacing.hit, height: Spacing.hit)
                        }
                        .buttonStyle(.plain).accessibilityLabel("Say it").accessibilityIdentifier("quickadd.mic")
                    }
                    .frame(minHeight: 56)
                    .background(Color.calSurface)
                    .overlay(Rectangle().strokeBorder(Color.calInk, lineWidth: 1))
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)

                    HStack {
                        Spacer()
                        Text(model.isParsing ? "Reading\u{2026}" : "Reads as you type").calLabel()
                    }
                    .padding(.horizontal, Spacing.gutter).padding(.top, 14)

                    if model.text.trimmingCharacters(in: .whitespaces).isEmpty {
                        VStack(spacing: 0) {
                            entryRow("New event", "Full form", first: true, id: "quickadd.form") { actions.fullForm() }
                            entryRow("Find free time", "Ask in words", id: "quickadd.freetime") { freeTime = makeFreeTime("") }
                            entryRow("Block focus time", "2 hours, today", id: "quickadd.focus") { freeTime = makeFreeTime("2 hours today") }
                        }
                        .padding(.top, Spacing.l)
                    } else {
                        livePreview
                    }
                }
            }
            .scrollDismissesKeyboard(.interactively)
            if !model.text.trimmingCharacters(in: .whitespaces).isEmpty {
                FooterBar {
                    CalButton("Preview") { Task { await model.submit() } }.disabled(!model.canGo).accessibilityIdentifier("quickadd.preview")
                }
            }
        }
        .onAppear { focused = true }
    }

    private func entryRow(_ title: String, _ detail: String, first: Bool = false, id: String, _ action: @escaping () -> Void) -> some View {
        SheetRow(title, detail: detail, isFirst: first, action: action).accessibilityIdentifier(id)
    }

    /// Echo of the words with their coloured underlines plus what was understood so far.
    @ViewBuilder private var livePreview: some View {
        VStack(alignment: .leading, spacing: Spacing.m) {
            switch model.parse {
            case .offline:
                AmberBanner("You\u{2019}re offline.", message: "Quick add needs your server. Use the full form instead.", kind: .neutral)
                    .padding(.horizontal, -Spacing.gutter)
            case .failed(let m):
                Text(m).font(CalFont.hint).foregroundStyle(Color.calRed)
            default:
                if let rows = model.previewRows {
                    Text(echo).font(CalFont.font(.sans, size: 19, relativeTo: .body)).foregroundStyle(Color.calInk).lineSpacing(6)
                        .accessibilityIdentifier("quickadd.echo")
                    VStack(alignment: .leading, spacing: 4) {
                        if let w = rows.when { Text(w).font(CalFont.sub).foregroundStyle(Color.calAmber) }
                        if let p = rows.place { Text(p).font(CalFont.sub).foregroundStyle(Color.calInk2) }
                    }
                } else {
                    Text("Keep typing, or tap Preview.").font(CalFont.hint).foregroundStyle(Color.calInk2)
                }
            }
        }
        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
    }

    // q-parse
    private var preview: some View {
        VStack(spacing: 0) {
            ScreenNav("Add an event", leading: .close, onLeading: { model.backToEntry() })
            if model.stage == .creating { ProgressRule().accessibilityLabel("Saving") }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    Text(echo).font(CalFont.font(.sans, size: 19, relativeTo: .body)).foregroundStyle(Color.calInk).lineSpacing(8)
                        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
                    if model.isOffline {
                        AmberBanner("You\u{2019}re offline.", message: "Nothing was created.", kind: .neutral).padding(.top, Spacing.l)
                    }
                    if let e = model.createError {
                        AmberBanner("Not created.", message: e, kind: .error).padding(.top, Spacing.l).accessibilityIdentifier("quickadd.error")
                    }
                    Text("I understood").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 28).padding(.bottom, 10)
                    if let r = model.previewRows { rows(r) }
                    Text("Nothing is created until you tap Create.").font(CalFont.hint).foregroundStyle(Color.calInk2)
                        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
                }
            }
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                CalButton(model.stage == .creating ? "Creating\u{2026}" : "Create") { Task { await model.create() } }
                    .disabled(!model.canCreate || model.stage == .creating).accessibilityIdentifier("quickadd.create")
                CalButton("Edit details", kind: .outline) { if let r = model.resolved { actions.editDetails(r) } }
                    .disabled(model.stage == .creating).accessibilityIdentifier("quickadd.edit")
            }
        }
    }

    @ViewBuilder private func rows(_ r: QuickAddViewModel.PreviewRows) -> some View {
        VStack(spacing: 0) {
            KeyValueRow("Title", r.title.isEmpty ? "Untitled" : r.title)
            if let w = r.when { whenRow(w) }
            if let p = r.place { KeyValueRow("Where", p) }
            if let c = r.calendar { KeyValueRow("Calendar", c) }
            if let p = r.people { KeyValueRow("People", p) }
        }
        .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
    }

    private func whenRow(_ w: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("When").calLabel()
            Text(w).font(CalFont.footer).foregroundStyle(Color.calAmber)
        }
        .padding(.horizontal, Spacing.gutter).padding(.vertical, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .accessibilityElement(children: .combine)
    }

    // q-ambig
    private var ambiguity: some View {
        VStack(spacing: 0) {
            ScreenNav("Add an event", leading: .close, onLeading: { model.backToEntry() })
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    Text(echo).font(CalFont.font(.sans, size: 19, relativeTo: .body)).foregroundStyle(Color.calInk).lineSpacing(8)
                        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
                    AmberBanner(model.requiredQuestions.count == 1 ? "One thing to check." : "Two things to check.",
                                message: "I won\u{2019}t guess.", kind: .neutral)
                        .padding(.top, Spacing.l).accessibilityIdentifier("quickadd.ambig")
                    ForEach(model.requiredQuestions) { q in
                        Text(q.question).calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 28).padding(.bottom, 10)
                        if q.options.isEmpty { freeform(q) }
                        ForEach(Array(q.options.enumerated()), id: \.offset) { n, o in
                            SheetRow(o.label, radio: model.answers[q.field] == o.value, isFirst: n == 0) { model.answer(q.field, o.value) }
                                .accessibilityIdentifier("quickadd.opt.\(q.field).\(n)")
                        }
                    }
                    ForEach(model.optionalQuestions.filter { $0.field == "when.time" || $0.field == "when.date" }) { q in
                        Text("Optional").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 28).padding(.bottom, 10)
                        SheetRow(q.question, detail: model.answerLabel(for: q) ?? "Pick", isFirst: true) { pickTime = true; pickedTime = Date() }
                            .accessibilityIdentifier("quickadd.pick")
                    }
                }
            }
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                CalButton("Continue") { model.continueAfterQuestions() }.disabled(!model.canContinue).accessibilityIdentifier("quickadd.continue")
            }
        }
        .sheet(isPresented: $pickTime) { timePicker }
    }

    private func freeform(_ q: DraftAmbiguity) -> some View {
        SheetRow(q.question, detail: model.answerLabel(for: q) ?? "Pick", isFirst: true) { pickTime = true; pickedTime = Date() }
    }

    private var timePicker: some View {
        VStack(spacing: 0) {
            ScreenNav("What time?", leading: .close, trailing: "Set", onLeading: { pickTime = false }, onTrailing: {
                var c = DateComponents(); let cal = Calendar.current
                c.hour = cal.component(.hour, from: pickedTime); c.minute = cal.component(.minute, from: pickedTime)
                model.answer("when.time", String(format: "%02d:%02d", c.hour ?? 9, c.minute ?? 0))
                pickTime = false
            })
            DatePicker("Time", selection: $pickedTime, displayedComponents: .hourAndMinute).datePickerStyle(.wheel).labelsHidden()
            Spacer()
        }
        .background(Color.calBg).presentationDetents([.height(320)]).presentationBackground(Color.calBg)
    }

    // q-down
    private var unreachable: some View {
        VStack(spacing: 0) {
            ScreenNav("Add an event", leading: .close, onLeading: { model.backToEntry() })
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    Text(echo).font(CalFont.font(.sans, size: 19, relativeTo: .body)).foregroundStyle(Color.calInk).lineSpacing(8)
                        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
                    AmberBanner("alfather isn\u{2019}t answering.", message: "Your words are kept. Nothing was created.", kind: .error)
                        .padding(.top, Spacing.l).accessibilityIdentifier("quickadd.down")
                    ListRow("Try again", sub: "alfather is on your home server", height: Spacing.rowHeightTall, isFirst: true) { EmptyView() }
                    ListRow("Fill in the form instead", sub: "Calendar can guess simple things like Fri 13:00 without alfather",
                            height: Spacing.rowHeightTall, subLineLimit: nil) { EmptyView() }
                    if let r = model.previewRows {
                        Text("The simple guess").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 28).padding(.bottom, 10)
                        rows(r)
                    }
                }
            }
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                CalButton("Try again") { Task { await model.retry() } }.accessibilityIdentifier("quickadd.retry")
                CalButton("New event form", kind: .outline) { if let r = model.resolved { actions.editDetails(r) } else { actions.fullForm() } }
                    .accessibilityIdentifier("quickadd.fallbackform")
            }
        }
    }

    // q-voice
    private var voiceSheet: some View {
        CalSheet(title: voice.state == .denied || voice.state == .unavailable || voice.state == .failed ? "Can\u{2019}t listen" : "Listening") {
            VStack(alignment: .leading, spacing: Spacing.l) {
                switch voice.state {
                case .denied:
                    AmberBanner("Allow the microphone in Settings, or type it instead.", actionTitle: "Settings",
                                action: { if let u = URL(string: UIApplication.openSettingsURLString) { openURL(u) } }, kind: .neutral)
                        .padding(.horizontal, -Spacing.gutter)
                case .unavailable:
                    AmberBanner("On-device dictation isn\u{2019}t available here.", message: "Type it instead. Nothing you say leaves this phone.", kind: .neutral)
                        .padding(.horizontal, -Spacing.gutter)
                case .failed:
                    AmberBanner("The microphone didn\u{2019}t start.", message: "Type it instead.", kind: .neutral).padding(.horizontal, -Spacing.gutter)
                default:
                    VoiceMeter(levels: voice.levels)
                    Text(voice.transcript.isEmpty ? "Say what you want to add\u{2026}" : voice.transcript)
                        .font(CalFont.font(.sans, size: 20, relativeTo: .title3))
                        .foregroundStyle(voice.transcript.isEmpty ? Color.calInk2 : Color.calInk)
                        .frame(maxWidth: .infinity, alignment: .leading).accessibilityIdentifier("voice.transcript")
                }
                HStack(spacing: 10) {
                    CalButton("Cancel", kind: .outline) { voice.cancel(); showVoice = false }.accessibilityIdentifier("voice.cancel")
                    if voice.state == .listening {
                        CalButton("Done") {
                            let words = voice.finish()
                            showVoice = false
                            if !words.isEmpty { model.text = words; Task { await model.submit() } }
                        }
                        .disabled(!voice.hasWords).accessibilityIdentifier("voice.done")
                    }
                }
            }
            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s).padding(.bottom, 24)
        }
    }
}
