import SwiftUI

// MARK: Glyphs (canvas CSS icons: 1.75pt strokes, no rounding)

/// Stroked glyphs matching the canvas icons `i-b` `i-d` `i-u` `i-r` `i-x` `i-p` `i-c`.
public struct CalGlyph: View {
    public enum Kind: Sendable { case back, forward, down, up, close, plus, check }
    let kind: Kind
    var size: CGFloat
    var weight: CGFloat

    public init(_ kind: Kind, size: CGFloat = 18, weight: CGFloat = 1.75) {
        self.kind = kind
        self.size = size
        self.weight = weight
    }

    public var body: some View {
        Canvas { ctx, sz in
            var p = Path()
            let w = sz.width, h = sz.height
            switch kind {
            case .back: p.move(to: CGPoint(x: w * 0.65, y: h * 0.12)); p.addLine(to: CGPoint(x: w * 0.25, y: h * 0.5)); p.addLine(to: CGPoint(x: w * 0.65, y: h * 0.88))
            case .forward: p.move(to: CGPoint(x: w * 0.35, y: h * 0.12)); p.addLine(to: CGPoint(x: w * 0.75, y: h * 0.5)); p.addLine(to: CGPoint(x: w * 0.35, y: h * 0.88))
            case .down: p.move(to: CGPoint(x: w * 0.15, y: h * 0.35)); p.addLine(to: CGPoint(x: w * 0.5, y: h * 0.7)); p.addLine(to: CGPoint(x: w * 0.85, y: h * 0.35))
            case .up: p.move(to: CGPoint(x: w * 0.15, y: h * 0.65)); p.addLine(to: CGPoint(x: w * 0.5, y: h * 0.3)); p.addLine(to: CGPoint(x: w * 0.85, y: h * 0.65))
            case .close: p.move(to: CGPoint(x: w * 0.08, y: h * 0.08)); p.addLine(to: CGPoint(x: w * 0.92, y: h * 0.92)); p.move(to: CGPoint(x: w * 0.92, y: h * 0.08)); p.addLine(to: CGPoint(x: w * 0.08, y: h * 0.92))
            case .plus: p.move(to: CGPoint(x: w * 0.5, y: 0)); p.addLine(to: CGPoint(x: w * 0.5, y: h)); p.move(to: CGPoint(x: 0, y: h * 0.5)); p.addLine(to: CGPoint(x: w, y: h * 0.5))
            case .check: p.move(to: CGPoint(x: w * 0.1, y: h * 0.55)); p.addLine(to: CGPoint(x: w * 0.4, y: h * 0.85)); p.addLine(to: CGPoint(x: w * 0.92, y: h * 0.2))
            }
            ctx.stroke(p, with: .foreground, style: StrokeStyle(lineWidth: weight, lineCap: .butt, lineJoin: .miter))
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

// MARK: Navigation

/// Bare nav bar (canvas `.nv`): h56, 44pt leading glyph button, centred 18 Medium title, optional 44pt trailing text
/// action in amber ("Add"). Hide the system navigation bar when you use this.
public struct ScreenNav: View {
    public enum Leading: Sendable { case back, close, none }

    let title: String
    let leading: Leading
    let trailing: String?
    let onLeading: () -> Void
    let onTrailing: (() -> Void)?

    public init(_ title: String, leading: Leading = .back, trailing: String? = nil,
                onLeading: @escaping () -> Void = {}, onTrailing: (() -> Void)? = nil) {
        self.title = title; self.leading = leading; self.trailing = trailing
        self.onLeading = onLeading; self.onTrailing = onTrailing
    }

    public var body: some View {
        HStack(spacing: 0) {
            Group {
                switch leading {
                case .back, .close:
                    Button(action: onLeading) { CalGlyph(leading == .back ? .back : .close).foregroundStyle(Color.calInk)
                        .frame(width: Spacing.hit, height: Spacing.hit) }
                        .accessibilityLabel(leading == .back ? "Back" : "Close")
                case .none:
                    Color.clear.frame(width: Spacing.hit, height: Spacing.hit)
                }
            }
            Text(title).font(CalFont.navTitle).foregroundStyle(Color.calInk).lineLimit(1)
                .frame(maxWidth: .infinity).accessibilityAddTraits(.isHeader)
            if let trailing, let onTrailing {
                Button(action: onTrailing) {
                    Text(trailing).font(CalFont.body).foregroundStyle(Color.calAmber)
                        .padding(.horizontal, Spacing.m).frame(minWidth: Spacing.hit, minHeight: Spacing.hit)
                }
            } else {
                Color.clear.frame(width: Spacing.hit, height: Spacing.hit)
            }
        }
        .padding(.horizontal, Spacing.s)
        .frame(height: Spacing.navHeight)
    }
}

/// Home header's trailing control (canvas `.tb`): 38pt square, 1pt hairline, surface fill, today's date in amber mono.
public struct TodayButton: View {
    let day: Int
    let action: () -> Void
    public init(day: Int, action: @escaping () -> Void) { self.day = day; self.action = action }

    public var body: some View {
        Button(action: action) {
            Text(String(day)).font(CalFont.timeMedium).foregroundStyle(Color.calAmber)
                .lineLimit(1).minimumScaleFactor(0.6)
                .dynamicTypeSize(...DynamicTypeSize.xLarge)   // fixed 38pt box
                .frame(width: Spacing.todayButton, height: Spacing.todayButton)
                .background(Color.calSurface)
                .overlay(Rectangle().strokeBorder(Color.calLine, lineWidth: 1))
                .frame(width: Spacing.hit, height: Spacing.hit)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Jump to today")
    }
}

/// Floating new-event button (canvas `.fab`): 52pt square, ink fill, bg-coloured plus, bottom-right at 24 / 28.
public struct Fab: View {
    let action: () -> Void
    @Environment(\.isEnabled) private var isEnabled
    public init(action: @escaping () -> Void) { self.action = action }

    public var body: some View {
        // Canvas draws the FAB 28pt from the screen edge (the home-indicator zone is part of that distance).
        // A flexible clear layer that ignores the bottom safe area lets the button align to the real edge.
        Color.clear
            .overlay(alignment: .bottomTrailing) {
                Button(action: action) {
                    // A shape (not .background) so the fill cannot stretch into the safe area.
                    Rectangle().fill(Color.calInk)
                        .frame(width: Spacing.fab, height: Spacing.fab)
                        .overlay { CalGlyph(.plus, size: 18).foregroundStyle(Color.calBg) }
                }
                .buttonStyle(.plain)
                .opacity(isEnabled ? 1 : 0.35)
                .padding(.trailing, Spacing.gutter).padding(.bottom, 28)
                .accessibilityLabel("New event")
                .accessibilityIdentifier("fab")
            }
            .ignoresSafeArea(.container, edges: .bottom)
    }
}

// MARK: Status, tags, bars

/// Status (canvas `.stt`): 8pt square + 13pt dim label. ok = moss, working = amber, bad = red, none = dim.
public struct StatusLabel: View {
    public enum Kind: Sendable { case none, ok, working, bad }
    let text: String
    let kind: Kind
    public init(_ text: String, kind: Kind = .none) { self.text = text; self.kind = kind }

    private var tint: Color {
        switch kind {
        case .none: .calInk2
        case .ok: .calGreen
        case .working: .calAmber
        case .bad: .calRed
        }
    }

    public var body: some View {
        HStack(spacing: 8) {
            Rectangle().fill(tint).frame(width: 8, height: 8)
            Text(text).font(CalFont.hint).foregroundStyle(Color.calInk2)
        }
        .accessibilityElement(children: .combine)
    }
}

/// Tag (canvas `.tag`): mono 11 caps, 1pt outline. `amber` variant for "Reply needed".
public struct CalTag: View {
    let text: String
    let amber: Bool
    public init(_ text: String, amber: Bool = false) { self.text = text; self.amber = amber }

    public var body: some View {
        Text(text).font(CalFont.label).tracking(0.9).textCase(.uppercase)
            .foregroundStyle(amber ? Color.calAmber : Color.calInk2)
            .padding(.horizontal, 8).padding(.vertical, 2)
            .overlay(Rectangle().strokeBorder(amber ? Color.calAmber : Color.calBorder, lineWidth: 1))
    }
}

/// Account colour bar (canvas `.ac`): 3pt wide, drawn flush to the screen's leading edge by the row.
public struct AccountBar: View {
    let color: AccountColor
    var height: CGFloat
    public init(_ color: AccountColor, height: CGFloat = 28) { self.color = color; self.height = height }
    public var body: some View {
        Rectangle().fill(color.color).frame(width: Spacing.colorBar, height: height).accessibilityHidden(true)
    }
}

/// 2pt progress line (canvas `.pg`): hairline track, amber fill. `fraction` nil = indeterminate-looking 38%.
public struct ProgressRule: View {
    let fraction: Double?
    public init(_ fraction: Double? = nil) { self.fraction = fraction }
    public var body: some View {
        GeometryReader { geo in
            Rectangle().fill(Color.calLine)
                .overlay(alignment: .leading) {
                    Rectangle().fill(Color.calAccent).frame(width: geo.size.width * (fraction ?? 0.38))
                }
        }
        .frame(height: 2)
        .accessibilityHidden(true)
    }
}

// MARK: Lists and forms

/// Two-line list row with an account bar (canvas `.r.r2`, 84pt for account lists): title 19, sub 14 dim, trailing view.
public struct ListRow<Trailing: View>: View {
    let title: String
    let sub: String?
    let color: AccountColor?
    let height: CGFloat
    let isFirst: Bool
    let subLineLimit: Int?
    let trailing: Trailing
    @Environment(\.dynamicTypeSize) private var typeSize

    /// `subLineLimit`: 1 by default; pass nil to let the second line wrap (canvas `.r2` rows such as "Check these").
    public init(_ title: String, sub: String? = nil, color: AccountColor? = nil, height: CGFloat = Spacing.accountRowHeight,
                isFirst: Bool = false, subLineLimit: Int? = 1, @ViewBuilder trailing: () -> Trailing) {
        self.title = title; self.sub = sub; self.color = color; self.height = height
        self.isFirst = isFirst; self.subLineLimit = subLineLimit; self.trailing = trailing()
    }

    public var body: some View {
        HStack(spacing: Spacing.l) {
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(CalFont.heading).foregroundStyle(Color.calInk)
                    .lineLimit(typeSize.isAccessibilitySize ? 3 : 1).truncationMode(.middle)
                if let sub { Text(sub).font(CalFont.sub).foregroundStyle(Color.calInk2).lineLimit(subLineLimit).fixedSize(horizontal: false, vertical: subLineLimit == nil) }
            }
            Spacer(minLength: Spacing.s)
            trailing
        }
        .padding(.horizontal, Spacing.gutter)
        .frame(maxWidth: .infinity, minHeight: height, alignment: .leading)
        .overlay(alignment: .leading) {
            if let color { Rectangle().fill(color.color).frame(width: Spacing.colorBar).padding(.vertical, 20) }
        }
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .overlay(alignment: .top) { if isFirst { Rectangle().fill(Color.calLine).frame(height: 1) } }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}

/// Labelled field (canvas `.fd` + `.in`): mono caps label 8pt above, h52 box with 1pt border, 17pt text.
/// Focus = ink border, error = red border. Put the `TextField` in `content`; pass `hint` / `error` below.
public struct LabeledField<Content: View>: View {
    let label: String
    let hint: String?
    let error: String?
    let isFocused: Bool
    let content: Content

    public init(_ label: String, hint: String? = nil, error: String? = nil, isFocused: Bool = false,
                @ViewBuilder content: () -> Content) {
        self.label = label; self.hint = hint; self.error = error; self.isFocused = isFocused; self.content = content()
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(label).calLabel()
            content
                .font(CalFont.body).foregroundStyle(Color.calInk)
                .padding(.horizontal, Spacing.l)
                .frame(maxWidth: .infinity, minHeight: Spacing.fieldHeight, alignment: .leading)
                .background(Color.calSurface)
                .overlay(Rectangle().strokeBorder(error != nil ? Color.calRed : (isFocused ? Color.calInk : Color.calControl), lineWidth: 1))
            if let error {
                Text(error).font(CalFont.hint).foregroundStyle(Color.calRed)
            } else if let hint {
                Text(hint).font(CalFont.hint).foregroundStyle(Color.calInk2)
            }
        }
    }
}

/// Square checkbox (canvas `.chk`): 22pt, 1pt border, ink fill with a bg check when on.
public struct Checkbox: View {
    let isOn: Bool
    public init(isOn: Bool) { self.isOn = isOn }
    public var body: some View {
        ZStack {
            Rectangle().fill(isOn ? Color.calInk : Color.clear)
            Rectangle().strokeBorder(isOn ? Color.calInk : Color.calControl, lineWidth: 1)
            if isOn { CalGlyph(.check, size: 11, weight: 1.75).foregroundStyle(Color.calBg) }
        }
        .frame(width: 22, height: 22)
        .accessibilityHidden(true)
    }
}

/// Key/value block (canvas `.kv`): mono caps key, 18pt value, hairline under.
public struct KeyValueRow: View {
    let key: String
    let value: String
    let isError: Bool
    public init(_ key: String, _ value: String, isError: Bool = false) { self.key = key; self.value = value; self.isError = isError }

    public var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(key).calLabel()
            Text(value).font(isError ? CalFont.body : CalFont.footer).foregroundStyle(isError ? Color.calRed : Color.calInk)
        }
        .padding(.horizontal, Spacing.gutter).padding(.vertical, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .accessibilityElement(children: .combine)
    }
}

/// Step row of a checklist (canvas pairing "Checking"): title left, status right. Waiting rows dim.
public struct ChecklistRow: View {
    let title: String
    let sub: String?
    let status: String
    let kind: StatusLabel.Kind
    let isFirst: Bool

    public init(_ title: String, sub: String? = nil, status: String, kind: StatusLabel.Kind, isFirst: Bool = false) {
        self.title = title; self.sub = sub; self.status = status; self.kind = kind; self.isFirst = isFirst
    }

    public var body: some View {
        ListRow(title, sub: sub, color: nil, height: Spacing.rowHeight, isFirst: isFirst) { StatusLabel(status, kind: kind) }
            .opacity(kind == .none ? 0.6 : 1)
    }
}
