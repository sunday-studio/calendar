import CalendarCore
import CalendarUI
import SwiftUI

/// Inline month (canvas h-month): surface card between hairlines, Monday-first weekday names, 17pt numerals,
/// 5pt square dots per account (max 3), today = amber number in an amber ring, selected = ink fill, legend below.
struct MonthGridView: View {
    let model: HomeViewModel
    private var showWeeks: Bool { model.prefs.showWeekNumbers }
    private var columns: [GridItem] {
        (showWeeks ? [GridItem(.fixed(28), spacing: 0)] : []) + Array(repeating: GridItem(.flexible(), spacing: 0), count: 7)
    }

    var body: some View {
        VStack(spacing: 0) {
            LazyVGrid(columns: columns, spacing: 0) {
                if showWeeks { Color.clear.frame(height: 30).accessibilityHidden(true) }
                ForEach(Array(model.weekdayNames.enumerated()), id: \.offset) { _, name in
                    Text(name).font(CalFont.font(.sans, size: 12, relativeTo: .caption)).foregroundStyle(Color.calInk2)
                        .frame(height: 30).accessibilityHidden(true)
                }
                ForEach(Array(model.gridCells.enumerated()), id: \.element) { n, day in
                    if showWeeks, n % 7 == 0, let w = model.weekNumber(row: n / 7) {
                        Text("\(w)").font(CalFont.label).foregroundStyle(Color.calInk3)
                            .frame(maxWidth: .infinity, minHeight: 50).accessibilityLabel("Week \(w)")
                    }
                    cell(day)
                }
            }
            .padding(.horizontal, Spacing.s).padding(.vertical, 6)
            legend
        }
        .background(Color.calSurface)
        .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .simultaneousGesture(
            DragGesture(minimumDistance: 30).onEnded { v in
                guard abs(v.translation.width) > abs(v.translation.height) * 1.5 else { return }
                model.shiftMonth(v.translation.width < 0 ? 1 : -1)
            }
        )
        .accessibilityElement(children: .contain)
        .accessibilityAdjustableAction { dir in model.shiftMonth(dir == .increment ? 1 : -1) }
        .accessibilityIdentifier("home.monthgrid")
    }

    private var legend: some View {
        HStack(spacing: Spacing.l) {
            ForEach(Array(model.legend.enumerated()), id: \.offset) { _, a in
                HStack(spacing: 6) {
                    Rectangle().fill(a.color.color).frame(width: 3, height: 12)
                    Text(a.name).font(CalFont.font(.sans, size: 12, relativeTo: .caption)).foregroundStyle(Color.calInk2).lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, Spacing.gutter).padding(.vertical, Spacing.m)
        .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .accessibilityHidden(true)
    }

    private func cell(_ day: DayKey) -> some View {
        let selected = day == model.selectedDay
        let isToday = day == model.today
        let inMonth = MonthGrid.inMonth(day, month: model.monthAnchor)
        let dots = model.dayDots[day] ?? []
        let number = String(day.rawValue.suffix(2).drop { $0 == "0" })
        return Button { Haptics.shared.play(.monthPick); model.select(day) } label: {
            VStack(spacing: 3) {
                Text(number)
                    .font(CalFont.font(.sans, size: 17, relativeTo: .body).monospacedDigit())
                    .foregroundStyle(selected ? Color.calBg : isToday ? Color.calAmber : inMonth ? Color.calInk : Color.calInk3)
                    .frame(width: 34, height: 34)
                    .background(selected ? Color.calInk : Color.clear)
                    .overlay { if isToday && !selected { Rectangle().strokeBorder(Color.calAmber, lineWidth: 1.5) } }
                HStack(spacing: 3) {
                    ForEach(Array(dots.prefix(3).enumerated()), id: \.offset) { _, c in
                        Rectangle().fill(c.color).frame(width: 5, height: 5)
                    }
                }
                .frame(height: 5)
            }
            .frame(maxWidth: .infinity, minHeight: 50)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(model.format.weekday(day)) \(model.format.dayMonth(day))\(isToday ? ", today" : "")\(dots.isEmpty ? "" : ", has events")")
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
    }
}
