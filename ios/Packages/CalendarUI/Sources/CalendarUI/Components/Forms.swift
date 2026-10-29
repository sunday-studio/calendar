import SwiftUI

/// Time stepper (canvas e-when): outline "−15" / "+15" buttons either side of xl flip cells. Wraps at midnight.
public struct TimeStepper: View {
    @Binding var minutes: Int
    let step: Int

    public init(minutes: Binding<Int>, step: Int = 15) {
        _minutes = minutes
        self.step = step
    }

    private var text: String {
        let m = ((minutes % 1440) + 1440) % 1440
        return String(format: "%02d:%02d", m / 60, m % 60)
    }

    public var body: some View {
        HStack {
            CalButton("−\(step)", kind: .outline, size: .small) { minutes -= step }
                .accessibilityLabel("\(step) minutes earlier")
            Spacer(minLength: Spacing.s)
            FlipCells(text, size: .xl)
            Spacer(minLength: Spacing.s)
            CalButton("+\(step)", kind: .outline, size: .small) { minutes += step }
                .accessibilityLabel("\(step) minutes later")
        }
        .padding(.horizontal, Spacing.gutter)
        .accessibilityElement(children: .contain)
    }
}

/// Reply bar (canvas r-detail): amber "Going?" label and Yes / Maybe / No. The pending choice is primary; Yes by default.
public struct ReplyBar: View {
    public enum Choice: String, CaseIterable, Sendable { case yes = "Yes", maybe = "Maybe", no = "No" }

    let selected: Choice?
    let isSending: Bool
    let onChoose: (Choice) -> Void

    public init(selected: Choice? = nil, isSending: Bool = false, onChoose: @escaping (Choice) -> Void) {
        self.selected = selected
        self.isSending = isSending
        self.onChoose = onChoose
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Going?").calLabel(.calAmber)
            HStack(spacing: 12) {
                ForEach(Choice.allCases, id: \.self) { c in
                    CalButton(c.rawValue, kind: c == (selected ?? .yes) ? .primary : .outline) { onChoose(c) }
                        .accessibilityAddTraits(c == selected ? .isSelected : [])
                }
            }
            .disabled(isSending)
            .opacity(isSending ? 0.35 : 1)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Going?")
    }
}

/// Form value row helper (canvas `.tg` with a colour square): label left, square + value + chevron right; amber when changed.
public struct FormValueRow: View {
    let title: String
    let value: String
    let color: AccountColor?
    let changed: Bool
    let showsChevron: Bool
    let isFirst: Bool

    public init(_ title: String, value: String, color: AccountColor? = nil, changed: Bool = false,
                showsChevron: Bool = true, isFirst: Bool = false) {
        self.title = title; self.value = value; self.color = color; self.changed = changed
        self.showsChevron = showsChevron; self.isFirst = isFirst
    }

    public var body: some View {
        SettingsRow(title, isFirst: isFirst) {
            HStack(spacing: 8) {
                if let color { Rectangle().fill(color.color).frame(width: 14, height: 14) }
                Text(value).font(CalFont.font(.sans, size: 16, relativeTo: .body))
                    .foregroundStyle(changed ? Color.calAmber : Color.calInk2)
                    .lineLimit(1).truncationMode(.tail)   // long calendar names end in an ellipsis
                if showsChevron { CalGlyph(.forward, size: 10).foregroundStyle(Color.calInk2) }
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(title), \(value)")
        .accessibilityAddTraits(.isButton)
    }
}
