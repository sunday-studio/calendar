import CalendarCore
import CalendarUI
import SwiftUI

/// New / edit event (boards e-new ... e-discard). Writes wait for the provider: the form dims, shows a rule,
/// and only closes once the hub confirms. Failures keep the form.
public struct EventEditorView: View {
    @State private var model: EventEditorViewModel
    private let accountNames: [String: String]
    private let onClose: (_ saved: Bool) -> Void

    private enum Sheet { case calendar, starts, ends, repeatRule, reminder, scope, discard }
    @State private var sheet: Sheet?
    @State private var showPeople = false
    @State private var showPlace = false
    @State private var showCustomRepeat = false
    @State private var showNotes = false
    @State private var scopeConfirmed = false
    @State private var toast: String?
    @FocusState private var titleFocus: Bool

    public init(model: EventEditorViewModel, accountNames: [String: String] = [:], onClose: @escaping (Bool) -> Void) {
        _model = State(initialValue: model)
        self.accountNames = accountNames
        self.onClose = onClose
    }

    private func isOpen(_ s: Sheet) -> Binding<Bool> {
        Binding(get: { sheet == s }, set: { if !$0, sheet == s { sheet = nil } })
    }

    public var body: some View {
        NavigationStack {
            ZStack {
                if case .conflict(let current) = model.saveState {
                    ConflictView(model: model, current: current, onClose: { onClose(false) })
                } else {
                    form
                }
            }
            .background(Color.calBg)
            .toolbar(.hidden, for: .navigationBar)
            .navigationDestination(isPresented: $showPeople) { PeopleEditor(model: model) }
            .navigationDestination(isPresented: $showCustomRepeat) { CustomRepeatView(model: model) }
        }
        .calSheet(isPresented: isOpen(.calendar)) { calendarSheet }
        .calSheet(isPresented: isOpen(.starts)) { whenSheet(ends: false) }
        .calSheet(isPresented: isOpen(.ends)) { whenSheet(ends: true) }
        .calSheet(isPresented: isOpen(.repeatRule)) { repeatSheet }
        .calSheet(isPresented: isOpen(.reminder)) { reminderSheet }
        .calSheet(isPresented: isOpen(.scope)) { scopeSheet }
        .calSheet(isPresented: isOpen(.discard)) { discardSheet }
        .onChange(of: sheet) { _, new in
            guard new != nil else { return }
            Haptics.shared.play(.sheetOpen)
            // The keyboard must go first: a picker sheet opened over it ends up half behind the keys.
            titleFocus = false
            UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
        }
        .onChange(of: model.saveState) { _, s in
            if s == .saved { Haptics.shared.play(.saved); EventEditorViewModel.clearDraft(); onClose(true) }
            if case .failed = s { Haptics.shared.play(.failure); titleFocus = true }
            if case .conflict = s { Haptics.shared.play(.conflict) }
        }
        .overlay(alignment: .bottom) {
            if let t = toast { ToastView(t, actionTitle: "OK") { toast = nil }.padding(.bottom, 40)
                .task(id: t) { try? await Task.sleep(for: .seconds(2.5)); toast = nil } }
        }
        .task { if !model.isEditing { titleFocus = true } }
        .presentationBackground(Color.calBg)
        .presentationCornerRadius(0)
        .presentationDragIndicator(.hidden)
        .interactiveDismissDisabled(model.shouldConfirmDiscard || model.isSaving)
        .accessibilityIdentifier("event.editor")
    }

    // MARK: Form

    private var form: some View {
        VStack(spacing: 0) {
            nav
            if model.isSaving { ProgressRule(0.6) }
            if model.isSaving {
                Text("Writing to \(model.providerName) first. The event shows on your board when \(model.providerName) confirms.")
                    .font(CalFont.hint).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s).frame(maxWidth: .infinity, alignment: .leading)
            }
            if case .failed(let message, _) = model.saveState {
                AmberBanner(model.failureHeadline, message: "Nothing was saved. Your changes are still here.", actionTitle: "Retry",
                            action: { attemptSave() }, kind: .error)
                    .padding(.bottom, 16)
                    .accessibilityIdentifier("editor.failed")
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    LabeledField("Title", error: model.message(for: "title") != nil && !model.title.isEmpty ? model.message(for: "title") : nil,
                                 isFocused: titleFocus) {
                        TextField("", text: Binding(get: { model.title }, set: { model.title = $0 }))
                            .accessibilityLabel("Title")
                            .focused($titleFocus).submitLabel(.next)
                            .onSubmit { sheet = .starts }
                            .accessibilityIdentifier("editor.title")
                    }
                    .padding(.horizontal, Spacing.gutter).padding(.top, 8)
                    rows.padding(.top, 20)
                    if !(showNotes || !model.notes.isEmpty) {
                        Button { showNotes = true } label: {
                            Text("Add notes").font(CalFont.status).foregroundStyle(Color.calAmber)
                                .frame(minHeight: Spacing.hit, alignment: .leading)
                        }
                        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
                    }
                    Color.clear.frame(height: 120)
                }
            }
            .scrollDismissesKeyboard(.interactively)
            .opacity(model.isSaving ? 0.5 : 1)
            .allowsHitTesting(!model.isSaving)
        }
        .safeAreaInset(edge: .bottom) {
            if case .failed = model.saveState {
                FooterBar {
                    CalButton("Try again") { attemptSave() }.accessibilityIdentifier("editor.retry")
                    CalButton("Keep as draft", kind: .outline) { model.saveDraft(); onClose(false) }
                }
            }
        }
    }

    private var nav: some View {
        ZStack(alignment: .trailing) {
            ScreenNav(model.isEditing ? "Edit event" : "New event", leading: .close, onLeading: close)
            if model.isSaving {
                Text("Saving").font(CalFont.body).foregroundStyle(Color.calInk2).padding(.trailing, Spacing.gutter)
            } else {
                Button { attemptSave() } label: {
                    Text("Save").font(CalFont.body).foregroundStyle(Color.calAmber)
                        .padding(.horizontal, Spacing.m).frame(minWidth: Spacing.hit, minHeight: Spacing.hit)
                }
                .padding(.trailing, Spacing.s)
                .opacity(model.canSave ? 1 : 0.35)
                .disabled(!model.canSave)
                .accessibilityLabel("Save").accessibilityIdentifier("editor.save")
            }
        }
    }

    private var rows: some View {
        VStack(spacing: 0) {
            Button { sheet = .calendar } label: {
                FormValueRow("Calendar", value: model.calendarName, color: model.calendarColor, changed: model.calendarChanged, isFirst: true)
            }.buttonStyle(.plain)
            ToggleRow("All day", isOn: Binding(get: { model.isAllDay }, set: { model.isAllDay = $0 }).haptic())
            Button { sheet = .starts } label: { FormValueRow("Starts", value: model.startText, changed: model.startChanged) }.buttonStyle(.plain)
            Button { sheet = .ends } label: { FormValueRow("Ends", value: model.endText, changed: model.endChanged) }.buttonStyle(.plain)
            Button { sheet = .repeatRule } label: { FormValueRow("Repeat", value: model.repeatText, changed: model.repeatChanged) }.buttonStyle(.plain)
            Button { sheet = .reminder } label: { FormValueRow("Reminder", value: model.reminderLabel, changed: model.reminderChanged) }.buttonStyle(.plain)
            Button { model.addsMeet.toggle() } label: {
                SettingsRow("Video call", sub: model.addsMeet ? "The link is made when you save." : nil) {
                    if model.addsMeet { CalTag("Google Meet") }
                    else { Text("Add Google Meet").font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calAmber) }
                }
            }.buttonStyle(.plain).accessibilityLabel(model.addsMeet ? "Video call, Google Meet, tap to remove" : "Video call, add Google Meet")
            if showPlace || !model.location.isEmpty {
                SettingsRow("Place") {
                    TextField("Add", text: Binding(get: { model.location }, set: { model.location = $0 }))
                        .accessibilityLabel("Place")
                        .multilineTextAlignment(.trailing).font(CalFont.font(.sans, size: 16, relativeTo: .body))
                        .frame(maxWidth: 200)
                }
            } else {
                Button { showPlace = true } label: { FormValueRow("Place", value: "Add") }.buttonStyle(.plain)
            }
            Button { showPeople = true } label: {
                FormValueRow("People", value: model.fields.attendees.isEmpty ? "Add" : peopleSummary)
            }.buttonStyle(.plain)
            if showNotes || !model.notes.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Notes").calLabel()
                    TextField("", text: Binding(get: { model.notes }, set: { model.notes = $0 }), axis: .vertical)
                        .accessibilityLabel("Notes")
                        .font(CalFont.font(.sans, size: 16, relativeTo: .body)).lineLimit(3...8)
                }
                .padding(.horizontal, Spacing.gutter).padding(.vertical, 14)
                .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
            }
        }
    }

    private var peopleSummary: String {
        let names = model.fields.attendees.map { $0.split(separator: "@").first.map(String.init) ?? $0 }
        return names.count <= 2 ? names.joined(separator: ", ") : names.prefix(2).joined(separator: ", ") + " +\(names.count - 2)"
    }

    // MARK: Actions

    private func close() {
        if model.shouldConfirmDiscard { sheet = .discard } else { onClose(false) }
    }

    private func attemptSave() {
        guard model.canSave else { return }
        if model.needsScopeChoice && !scopeConfirmed { sheet = .scope; return }
        titleFocus = false
        Haptics.shared.play(.primaryButton)
        Task { await model.save() }
    }

    // MARK: Sheets

    private var calendarSheet: some View {
        let groups = Dictionary(grouping: model.writableCalendars, by: \.accountId)
        return CalSheet(title: "Add to") {
            ForEach(groups.keys.sorted(), id: \.self) { acct in
                Text(accountNames[acct] ?? "Account").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 6).padding(.bottom, 8)
                ForEach(Array((groups[acct] ?? []).enumerated()), id: \.element.id) { n, c in
                    SheetRow(c.name, detail: c.id == model.defaultCalendarId ? "Default" : nil, radio: model.calendarId == c.id, isFirst: n == 0) {
                        model.calendarId = c.id; sheet = nil
                    }
                }
            }
        }
    }

    private func whenSheet(ends: Bool) -> some View {
        let anchor = ends ? model.fields.end : model.fields.start
        let day = DayKey(anchor, in: model.tz)
        let f = EventTimeFormat(tz: model.tz, locale: DisplayPrefs.load().locale())
        let strip = (-2...1).map { f.dayShort(day.adding(days: $0)) }
        return CalSheet(title: ends ? "Ends" : "Starts") {
            SegmentedControl(strip, selection: Binding(get: { 2 }, set: { i in
                let n = i - 2
                if ends { model.setEnd(model.fields.end.addingTimeInterval(TimeInterval(86_400 * n))) }
                else { model.setFirstDayFromStart(shiftDays: n) }
            }))
            .padding(.horizontal, Spacing.gutter).padding(.bottom, Spacing.l)
            if !model.isAllDay {
                TimeStepper(minutes: ends ? Binding(get: { model.endMinutes }, set: { model.endMinutes = $0 })
                                          : Binding(get: { model.startMinutes }, set: { model.startMinutes = $0 }))
            }
            if !ends && !model.isAllDay {
                Text("Length").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l).padding(.bottom, 10)
                HStack(spacing: 6) {
                    ForEach(EventEditorViewModel.lengthChoices, id: \.minutes) { c in
                        ChipToggle(c.title, isOn: Binding(get: { model.lengthMinutes == c.minutes }, set: { _ in model.setLength(c.minutes) }))
                    }
                }
                .padding(.horizontal, Spacing.gutter)
            }
            if let issue = model.message(for: "when") {
                Text(issue).font(CalFont.hint).foregroundStyle(Color.calRed).padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
            } else if !model.isAllDay {
                Text(model.endsHint).font(CalFont.hint).foregroundStyle(Color.calInk2).padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
            }
            CalButton("Done") { sheet = nil }.padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
        }
    }

    private var repeatSheet: some View {
        CalSheet(title: "Repeat") {
            ForEach(Array(model.repeatOptions.enumerated()), id: \.element.id) { n, o in
                SheetRow(o.title, radio: model.rrule == o.rrule, isFirst: n == 0) { model.rrule = o.rrule; sheet = nil }
            }
            SheetRow("Custom", detail: isCustomRepeat ? model.repeatText : nil, radio: isCustomRepeat) { sheet = nil; showCustomRepeat = true }
        }
    }

    private var isCustomRepeat: Bool { model.rrule != nil && !model.repeatOptions.contains { $0.rrule == model.rrule } }

    private var reminderSheet: some View {
        CalSheet(title: "Reminder") {
            ForEach(Array(ReminderText.choices.enumerated()), id: \.offset) { n, m in
                SheetRow(m.map(ReminderText.text) ?? "None", radio: model.reminderMin == m, isFirst: n == 0) { model.reminderMin = m; sheet = nil }
            }
        }
    }

    private var scopeSheet: some View {
        CalSheet(title: "Save changes to") {
            ForEach(Array(model.scopeChoices.enumerated()), id: \.element) { n, s in
                SheetRow(s == .this ? "This event" : s == .following ? "This and following" : "All events",
                         radio: model.scope == s, isFirst: n == 0) { model.scope = s }
            }
            VStack(spacing: 10) {
                CalButton("Save") { scopeConfirmed = true; sheet = nil; Task { await model.save() } }
                CalButton("Cancel", kind: .outline) { sheet = nil }
            }
            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
        }
    }

    private var discardSheet: some View {
        CalSheet(title: model.isEditing ? "Discard changes?" : "Discard this event?", message: model.isEditing ? nil : "You have not saved it yet.") {
            VStack(spacing: 10) {
                CalButton("Discard", kind: .danger) { sheet = nil; onClose(false) }
                CalButton("Keep editing", kind: .outline) { sheet = nil }
            }
            .padding(.horizontal, Spacing.gutter)
        }
    }
}

/// People (board e-guests): add by address, remove added guests.
struct PeopleEditor: View {
    let model: EventEditorViewModel
    @State private var text = ""
    @State private var error: String?
    @FocusState private var focus: Bool
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            ScreenNav("People", leading: .back, trailing: "Done", onLeading: { _ = model.commitPendingGuest(text); dismiss() }, onTrailing: finish)
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    LabeledField("Add", error: error, isFocused: focus) {
                        TextField("name@example.com", text: $text)
                            .accessibilityLabel("Guest email")
                            .keyboardType(.emailAddress).textInputAutocapitalization(.never).autocorrectionDisabled()
                            .focused($focus).submitLabel(.done).onSubmit(add)
                    }
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
                    if !model.fields.attendees.isEmpty {
                        Text("Added").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 32).padding(.bottom, 10)
                        ForEach(Array(model.fields.attendees.enumerated()), id: \.element) { n, email in
                            Button { model.removeAttendee(email) } label: {
                                CalendarUI.PersonRow(email, status: "Remove", isFirst: n == 0)
                            }.buttonStyle(.plain)
                        }
                    }
                    Text("They get an invitation from your \(model.calendarName) calendar when you save.")
                        .font(CalFont.hint).foregroundStyle(Color.calInk2).padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
                }
            }
        }
        .background(Color.calBg)
        .toolbar(.hidden, for: .navigationBar)
        .task { focus = true }
    }

    /// Done keeps an address that was typed but not submitted; one that is not an address stays on screen with the reason.
    private func finish() {
        if model.commitPendingGuest(text) { dismiss() } else { error = "That doesn\u{2019}t look like a new email address." }
    }

    private func add() {
        let t = text.trimmingCharacters(in: .whitespaces)
        guard !t.isEmpty else { return }
        if model.addAttendee(t) { text = ""; error = nil } else { error = "That doesn\u{2019}t look like a new email address." }
    }
}

/// Changed elsewhere (board e-conflict).
struct ConflictView: View {
    let model: EventEditorViewModel
    let current: EventDetail
    let onClose: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            ScreenNav(current.item.title, leading: .close, onLeading: onClose)
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    AmberBanner("This event changed while you were editing.", message: nil).padding(.top, Spacing.s)
                    ForEach(Array(model.conflictRows(current: current).enumerated()), id: \.offset) { _, r in
                        Text(r.label).calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 32).padding(.bottom, 10)
                        KeyValueRow("Yours", r.mine)
                        VStack(alignment: .leading, spacing: 6) {
                            Text("Now on Google").calLabel()
                            Text(r.theirs).font(CalFont.footer).foregroundStyle(Color.calAmber)
                        }
                        .padding(.horizontal, Spacing.gutter).padding(.vertical, 14).frame(maxWidth: .infinity, alignment: .leading)
                        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
                    }
                }
            }
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                CalButton("Use latest and re-apply my changes") { Task { await model.keepMine() } }.accessibilityIdentifier("conflict.reapply")
                CalButton("Use theirs", kind: .outline) { model.useTheirs() }
            }
        }
        .accessibilityIdentifier("event.conflict")
    }
}
