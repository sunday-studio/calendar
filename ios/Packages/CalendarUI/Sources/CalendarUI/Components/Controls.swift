import SwiftUI

/// Segmented control (canvas `.sg`): one 1pt `border` box, equal segments h40, 15pt dim; selected = ink fill, bg text.
public struct SegmentedControl: View {
    let items: [String]
    @Binding var selection: Int
    let disabled: Set<Int>

    public init(_ items: [String], selection: Binding<Int>, disabled: Set<Int> = []) {
        self.items = items
        _selection = selection
        self.disabled = disabled
    }

    public var body: some View {
        HStack(spacing: 0) {
            ForEach(Array(items.enumerated()), id: \.offset) { i, title in
                Button { selection = i } label: {
                    Text(title).font(CalFont.font(.sans, size: 15, relativeTo: .subheadline))
                        .lineLimit(1).minimumScaleFactor(0.7)
                        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)   // 4 segments cannot grow past this
                        .foregroundStyle(i == selection ? Color.calBg : Color.calInk2)
                        .frame(maxWidth: .infinity, minHeight: Spacing.hit)
                        .background(i == selection ? Color.calInk : Color.clear)
                        .contentShape(Rectangle())
                        .opacity(disabled.contains(i) ? 0.4 : 1)
                }
                .buttonStyle(.plain)
                .disabled(disabled.contains(i))
                .accessibilityAddTraits(i == selection ? .isSelected : [])
            }
        }
        .overlay(Rectangle().strokeBorder(Color.calControl, lineWidth: 1))
        .frame(minHeight: Spacing.hit)   // 44pt hit area (canvas draws 40; HIG minimum wins)
    }
}

/// Switch (canvas `.sw`): 46×26 square, 1pt border; thumb 18×18 square. On = ink fill with a bg thumb.
public struct CalSwitch: View {
    @Binding var isOn: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    public init(isOn: Binding<Bool>) { _isOn = isOn }

    public var body: some View {
        ZStack(alignment: .leading) {
            Rectangle().fill(isOn ? Color.calInk : Color.clear)
            Rectangle().strokeBorder(isOn ? Color.calInk : Color.calControl, lineWidth: 1)
            Rectangle().fill(isOn ? Color.calBg : Color.calInk2).frame(width: 18, height: 18)
                .offset(x: isOn ? 23 : 3)
        }
        .frame(width: 46, height: 26)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.15), value: isOn)
        .accessibilityHidden(true)
    }
}

/// Settings row (canvas `.tg`): title 18, optional 13 dim second line, trailing content. Min h60, hairline under.
public struct SettingsRow<Trailing: View>: View {
    let title: String
    let sub: String?
    let isFirst: Bool
    let trailing: Trailing

    public init(_ title: String, sub: String? = nil, isFirst: Bool = false, @ViewBuilder trailing: () -> Trailing) {
        self.title = title; self.sub = sub; self.isFirst = isFirst; self.trailing = trailing()
    }

    public var body: some View {
        HStack(spacing: Spacing.l) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(CalFont.font(.sans, size: 18, relativeTo: .body)).foregroundStyle(Color.calInk)
                if let sub { Text(sub).font(CalFont.hint).foregroundStyle(Color.calInk2) }
            }
            Spacer(minLength: Spacing.s)
            trailing
        }
        .padding(.horizontal, Spacing.gutter).padding(.vertical, 10)
        .frame(maxWidth: .infinity, minHeight: 60, alignment: .leading)
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .overlay(alignment: .top) { if isFirst { Rectangle().fill(Color.calLine).frame(height: 1) } }
        .contentShape(Rectangle())
    }
}

/// Navigation row: title, optional value (16 dim) and a forward chevron.
public struct NavRow: View {
    let title: String
    let sub: String?
    let value: String?
    let isFirst: Bool
    public init(_ title: String, sub: String? = nil, value: String? = nil, isFirst: Bool = false) {
        self.title = title; self.sub = sub; self.value = value; self.isFirst = isFirst
    }
    public var body: some View {
        SettingsRow(title, sub: sub, isFirst: isFirst) {
            HStack(spacing: 8) {
                if let value { Text(value).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2).lineLimit(1).truncationMode(.tail) }
                CalGlyph(.forward, size: 10).foregroundStyle(Color.calInk2)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// Toggle row: title, optional sub, `CalSwitch`. Whole row toggles.
public struct ToggleRow: View {
    let title: String
    let sub: String?
    let isFirst: Bool
    @Binding var isOn: Bool
    public init(_ title: String, sub: String? = nil, isOn: Binding<Bool>, isFirst: Bool = false) {
        self.title = title; self.sub = sub; self.isFirst = isFirst; _isOn = isOn
    }
    public var body: some View {
        Button { isOn.toggle() } label: {
            SettingsRow(title, sub: sub, isFirst: isFirst) { CalSwitch(isOn: $isOn) }
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(title)
        .accessibilityValue(isOn ? "On" : "Off")
        .accessibilityAddTraits(.isButton)
    }
}

/// Day chips in working hours (canvas `.pill-row .tag`): mono 11 caps, 1pt border, pad 6/12; on = ink fill.
public struct ChipToggle: View {
    let title: String
    @Binding var isOn: Bool
    public init(_ title: String, isOn: Binding<Bool>) { self.title = title; _isOn = isOn }
    public var body: some View {
        Button { isOn.toggle() } label: {
            Text(title).font(CalFont.label).tracking(0.9).textCase(.uppercase)
                .foregroundStyle(isOn ? Color.calBg : Color.calInk2)
                .lineLimit(1).fixedSize(horizontal: true, vertical: false)   // never wrap "MON" into "MO / N"
                .padding(.horizontal, 10).padding(.vertical, 6)
                .background(isOn ? Color.calInk : Color.clear)
                .overlay(Rectangle().strokeBorder(isOn ? Color.calInk : Color.calControl, lineWidth: 1))
                .frame(minHeight: Spacing.hit)
        }
        .buttonStyle(.plain)
        .accessibilityValue(isOn ? "On" : "Off")
    }
}

/// Colour picker (canvas g-colour / a-pick): 44pt squares, gap 10, selected = 2pt bg gap + 1pt ink ring.
public struct ColourSwatchRow: View {
    let colours: [AccountColor]
    @Binding var selection: AccountColor?
    let size: CGFloat

    public init(_ colours: [AccountColor] = AccountColor.allCases, selection: Binding<AccountColor?>, size: CGFloat = 44) {
        self.colours = colours; _selection = selection; self.size = size
    }

    public var body: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: size), spacing: 10)], alignment: .leading, spacing: 10) {
            ForEach(colours) { c in
                Button { selection = c } label: {
                    Rectangle().fill(c.color).frame(width: size, height: size)
                        .padding(4)
                        .overlay { if selection == c { Rectangle().strokeBorder(Color.calInk, lineWidth: 1) } }
                }
                .buttonStyle(.plain)
                .accessibilityLabel(c.rawValue.capitalized)
                .accessibilityAddTraits(selection == c ? .isSelected : [])
            }
        }
        .padding(.horizontal, Spacing.gutter - 4)
    }
}

/// Join / Map bar on the event detail (canvas `.joinbar`): account bright colour, provider on the left, inline button.
public struct JoinBar: View {
    let text: String
    let buttonTitle: String
    let color: AccountColor
    let action: () -> Void

    public init(_ text: String, button: String, color: AccountColor, action: @escaping () -> Void) {
        self.text = text; self.buttonTitle = button; self.color = color; self.action = action
    }

    public var body: some View {
        HStack(spacing: Spacing.m) {
            Text(text).font(CalFont.footer).foregroundStyle(Theme.onAccountColor.color).lineLimit(1)
            Spacer(minLength: Spacing.s)
            Button(action: action) {
                Text(buttonTitle).font(CalFont.blockButton).foregroundStyle(Theme.flipInk.color)
                    .padding(.horizontal, 22).padding(.vertical, 9)
                    .background(Theme.onAccountColor.color)
            }
            .buttonStyle(.plain).frame(minHeight: Spacing.hit)
        }
        .padding(.horizontal, Spacing.gutter).padding(.vertical, 8)
        .frame(maxWidth: .infinity, minHeight: 60)
        .background(color.bright)
        .accessibilityElement(children: .contain)
    }
}

/// Person in a guest list (canvas `.who`): 36pt initials box, name 17, status word right.
public struct PersonRow: View {
    public enum Tone: Sendable { case dim, amber }
    let name: String
    let status: String
    let tone: Tone
    let isFirst: Bool
    public init(_ name: String, status: String, tone: Tone = .dim, isFirst: Bool = false) {
        self.name = name; self.status = status; self.tone = tone; self.isFirst = isFirst
    }

    private var initials: String {
        let parts = name.split(separator: " ").prefix(2)
        return parts.compactMap { $0.first.map(String.init) }.joined().uppercased()
    }

    public var body: some View {
        HStack(spacing: Spacing.m) {
            Text(initials).font(CalFont.sub).foregroundStyle(Color.calInk2)
                .frame(width: 36, height: 36)
                .background(Color.calSunk)
                .overlay(Rectangle().strokeBorder(Color.calBorder, lineWidth: 1))
            Text(name).font(CalFont.body).foregroundStyle(Color.calInk).lineLimit(1)
            Spacer(minLength: Spacing.s)
            Text(status).font(CalFont.status).foregroundStyle(tone == .amber ? Color.calAmber : Color.calInk2)
        }
        .padding(.horizontal, Spacing.gutter)
        .frame(maxWidth: .infinity, minHeight: 52, alignment: .leading)
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .overlay(alignment: .top) { if isFirst { Rectangle().fill(Color.calLine).frame(height: 1) } }
        .accessibilityElement(children: .combine)
    }
}

/// Select field (canvas `.sel`): compact header field beside today's date. h38 (44pt hit), control border, surface
/// fill, 16pt value + down chevron. Opens a list (`.calSheet` with `SheetRow`s). Replaces the view switch.
public struct SelectField: View {
    let label: String
    let value: String
    let action: () -> Void

    public init(label: String = "View", value: String, action: @escaping () -> Void) {
        self.label = label; self.value = value; self.action = action
    }

    public var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Text(value).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk)
                    .lineLimit(1).minimumScaleFactor(0.7)
                    .dynamicTypeSize(...DynamicTypeSize.xLarge)
                CalGlyph(.down, size: 12).foregroundStyle(Color.calInk2)
            }
            .padding(.horizontal, 12)
            .frame(height: Spacing.todayButton)
            .background(Color.calSurface)
            .overlay(Rectangle().strokeBorder(Color.calControl, lineWidth: 1))
            .frame(minHeight: Spacing.hit)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(label), \(value)")
        .accessibilityHint("Opens the list of views")
    }
}
