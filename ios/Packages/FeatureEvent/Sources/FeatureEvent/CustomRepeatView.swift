import CalendarCore
import CalendarUI
import SwiftUI

/// Custom repeat (board e-repeat > Custom): interval stepper, unit, weekday chips, ends.
struct CustomRepeatView: View {
    let model: EventEditorViewModel
    @State private var rule: RepeatRule
    @State private var endsKind = 0           // 0 never, 1 on date, 2 after
    @State private var endDate: Date
    @State private var count = 10
    @Environment(\.dismiss) private var dismiss

    init(model: EventEditorViewModel) {
        self.model = model
        let existing = model.rrule.flatMap(RepeatRule.init(parsing:))
        let start = model.fields.start
        var r = existing ?? RepeatRule(unit: .week, interval: 1, weekdays: [])
        if r.unit == .week && r.weekdays.isEmpty {
            r.weekdays = [DayKey(start, in: model.tz).weekday % 7]
        }
        _rule = State(initialValue: r)
        switch r.ends {
        case .never: _endsKind = State(initialValue: 0); _endDate = State(initialValue: start.addingTimeInterval(30 * 86_400))
        case .on(let d): _endsKind = State(initialValue: 1); _endDate = State(initialValue: d.startOfDay(in: model.tz))
        case .after(let n): _endsKind = State(initialValue: 2); _endDate = State(initialValue: start.addingTimeInterval(30 * 86_400)); _count = State(initialValue: n)
        }
    }

    private var effective: RepeatRule {
        var r = rule
        switch endsKind { case 1: r.ends = .on(DayKey(endDate, in: model.tz)); case 2: r.ends = .after(count); default: r.ends = .never }
        return r
    }

    var body: some View {
        VStack(spacing: 0) {
            ScreenNav("Custom repeat", leading: .back, trailing: "Done", onLeading: { dismiss() }, onTrailing: { apply() })
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    Text(effective.summary(format: model.format)).font(CalFont.font(.sans, size: 16, relativeTo: .body))
                        .foregroundStyle(Color.calInk2).padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
                        .accessibilityIdentifier("repeat.summary")
                    GroupLabel("Every", first: true)
                    SettingsRow("Interval", isFirst: true) {
                        HStack(spacing: Spacing.m) {
                            CalButton("\u{2212}", kind: .outline, size: .small) { rule.interval = max(1, rule.interval - 1) }
                                .accessibilityLabel("One less")
                            Text("\(rule.interval)").font(CalFont.timeMedium).frame(minWidth: 28)
                            CalButton("+", kind: .outline, size: .small) { rule.interval = min(99, rule.interval + 1) }
                                .accessibilityLabel("One more")
                        }
                    }
                    SegmentedControl(RepeatRule.Unit.allCases.map { $0.title.capitalized + (rule.interval == 1 ? "" : "s") },
                                     selection: Binding(get: { RepeatRule.Unit.allCases.firstIndex(of: rule.unit) ?? 1 },
                                                        set: { rule.unit = RepeatRule.Unit.allCases[$0] }))
                        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
                    if rule.unit == .week {
                        GroupLabel("On")
                        HStack(spacing: 6) {
                            ForEach([1, 2, 3, 4, 5, 6, 0], id: \.self) { d in
                                ChipToggle(["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"][d], isOn: Binding(
                                    get: { rule.weekdays.contains(d) },
                                    set: { _ in if rule.weekdays.contains(d) { if rule.weekdays.count > 1 { rule.weekdays.remove(d) } } else { rule.weekdays.insert(d) } }))
                            }
                        }
                        .padding(.horizontal, Spacing.gutter)
                    }
                    GroupLabel("Ends")
                    SegmentedControl(["Never", "On date", "After"], selection: $endsKind).padding(.horizontal, Spacing.gutter)
                    if endsKind == 1 {
                        DatePicker("Last day", selection: $endDate, displayedComponents: .date)
                            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
                    }
                    if endsKind == 2 {
                        SettingsRow("Occurrences") {
                            HStack(spacing: Spacing.m) {
                                CalButton("\u{2212}", kind: .outline, size: .small) { count = max(1, count - 1) }.accessibilityLabel("One fewer")
                                Text("\(count)").font(CalFont.timeMedium).frame(minWidth: 28)
                                CalButton("+", kind: .outline, size: .small) { count = min(999, count + 1) }.accessibilityLabel("One more")
                            }
                        }
                    }
                    Color.clear.frame(height: Spacing.xxl)
                }
            }
        }
        .background(Color.calBg)
        .toolbar(.hidden, for: .navigationBar)
        .accessibilityIdentifier("event.customRepeat")
    }

    private func apply() { model.rrule = effective.rrule; dismiss() }
}

private struct GroupLabel: View {
    let text: String
    let first: Bool
    init(_ text: String, first: Bool = false) { self.text = text; self.first = first }
    var body: some View { Text(text).calLabel().padding(.horizontal, Spacing.gutter).padding(.top, first ? 12 : 32).padding(.bottom, 10) }
}
