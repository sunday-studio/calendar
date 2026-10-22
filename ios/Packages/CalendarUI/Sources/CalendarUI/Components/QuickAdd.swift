import SwiftUI

/// Colour-coded 2pt underlines for recognised words in quick add (canvas q-parse): who = cobalt, when = amber, where = moss.
/// Underlines only, never background fills.
public enum UnderlineSpan {
    public enum Kind: Sendable { case who, when, place }

    public static func attributed(_ text: String, spans: [(range: Range<String.Index>, kind: Kind)]) -> AttributedString {
        var out = AttributedString(text)
        for s in spans {
            guard let lo = AttributedString.Index(s.range.lowerBound, within: out),
                  let hi = AttributedString.Index(s.range.upperBound, within: out) else { continue }
            out[lo..<hi].underlineStyle = .single
            out[lo..<hi].underlineColor = UIColor { _ in
                switch s.kind {
                case .who: AccountColor.cobalt.token.uiColor
                case .when: Theme.accent.uiColor
                case .place: AccountColor.moss.token.uiColor
                }
            }
        }
        return out
    }
}

/// Voice level bars (canvas q-voice): 6pt wide, gap 4, max 48 high; recent bars amber, older `faint`.
public struct VoiceMeter: View {
    let levels: [Double]
    let activeCount: Int
    public init(levels: [Double], activeCount: Int? = nil) {
        self.levels = levels
        self.activeCount = activeCount ?? max(0, levels.count - 2)
    }
    public var body: some View {
        HStack(alignment: .bottom, spacing: 4) {
            ForEach(Array(levels.enumerated()), id: \.offset) { i, l in
                Rectangle().fill(i < activeCount ? Color.calAmber : Color.calInk3)
                    .frame(width: 6, height: max(6, 48 * min(max(l, 0), 1)))
            }
        }
        .frame(height: 48, alignment: .bottom)
        .accessibilityHidden(true)
    }
}

/// "At a glance" day bar (canvas q-slots): h36 surface with busy blocks in account colours and the proposed slot dashed amber.
public struct DayGlance: View {
    public struct Block: Identifiable, Sendable {
        public let id = UUID()
        public let startHour: Double
        public let endHour: Double
        public let color: AccountColor
        public init(startHour: Double, endHour: Double, color: AccountColor) {
            self.startHour = startHour; self.endHour = endHour; self.color = color
        }
    }

    let blocks: [Block]
    let proposed: (start: Double, end: Double)?
    let from: Double
    let to: Double

    public init(blocks: [Block], proposed: (start: Double, end: Double)? = nil, from: Double = 9, to: Double = 18) {
        self.blocks = blocks; self.proposed = proposed; self.from = from; self.to = to
    }

    public var body: some View {
        VStack(spacing: 6) {
            GeometryReader { geo in
                let x: (Double) -> CGFloat = { h in geo.size.width * CGFloat((h - from) / (to - from)) }
                ZStack(alignment: .leading) {
                    Rectangle().fill(Color.calSurface)
                    Rectangle().strokeBorder(Color.calLine, lineWidth: 1)
                    ForEach(blocks) { b in
                        Rectangle().fill(b.color.color).frame(width: max(2, x(b.endHour) - x(b.startHour)))
                            .offset(x: x(b.startHour))
                    }
                    if let p = proposed {
                        Rectangle().strokeBorder(Color.calAmber, style: StrokeStyle(lineWidth: 1, dash: [4, 3]))
                            .frame(width: x(p.end) - x(p.start)).offset(x: x(p.start))
                    }
                }
            }
            .frame(height: 36)
            HStack {
                ForEach([9, 12, 15, 18], id: \.self) { h in
                    Text(String(format: "%02d", h)).font(CalFont.label).foregroundStyle(Color.calInk2)
                    if h != 18 { Spacer() }
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Day overview, \(blocks.count) busy blocks")
    }
}

