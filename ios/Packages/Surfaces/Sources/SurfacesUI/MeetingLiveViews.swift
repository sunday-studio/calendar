#if canImport(ActivityKit)
import CalendarCore
import CalendarUI
import SurfacesCore
import SwiftUI
import WidgetKit

/// Lock-screen Live Activity (spec w-live): bright account block with flip cells, title, live words, Join, 3pt progress line.
/// Meeting block for the Live Activity; takes live `Text` for the footer (a `String` cannot count down).
public struct MeetingLockView: View {
    let attributes: MeetingActivityAttributes
    let state: MeetingActivityAttributes.ContentState
    let isStale: Bool
    let now: Date
    let format: SurfaceFormat

    public init(attributes: MeetingActivityAttributes, state: MeetingActivityAttributes.ContentState, isStale: Bool = false,
                now: Date = Date(), tz: TimeZone = .current) {
        self.attributes = attributes; self.state = state; self.isStale = isStale; self.now = now
        self.format = SurfaceFormat(tz: tz)
    }

    private var running: Bool { state.running || isStale || now >= state.start }
    private var color: AccountColor { AccountColor(key: attributes.color) }
    private var joinURL: URL? { attributes.joinURL.flatMap(URL.init(string:)) }

    var footer: Text {
        let tail = running ? Text("") : Text(" \u{00B7} \(state.location ?? SurfaceFormat.joinLabel(attributes.joinKind))")
        return running
            ? Text("Now \u{00B7} ") + Text(state.end, style: .relative) + Text(" left")
            : Text("in ") + Text(state.start, style: .relative) + tail
    }

    private var ended: Bool { now >= state.end }

    /// Mono caps line: "IN 7:52 · GOOGLE MEET" before, "22:00 LEFT · MEET" while running. The timer ticks without updates.
    @ViewBuilder private var meta: some View {
        let tag = SurfaceFormat.joinBadge(attributes.joinKind)
        let hasLink = attributes.joinURL != nil
        HStack(spacing: 0) {
            if running {
                Text(timerInterval: now...max(state.end, now.addingTimeInterval(1)), countsDown: true).monospacedDigit().frame(maxWidth: 64, alignment: .leading)
                Text(" LEFT")
                if hasLink { Text(" \u{00B7} \(tag)") }
            } else {
                Text("IN ")
                Text(timerInterval: now...max(state.start, now.addingTimeInterval(1)), countsDown: true).monospacedDigit().frame(maxWidth: 64, alignment: .leading)
                if hasLink { Text(" \u{00B7} \(SurfaceFormat.joinLabel(attributes.joinKind).uppercased())") }
                else if let l = state.location { Text(" \u{00B7} \(l.uppercased())").lineLimit(1) }
            }
        }
        .font(CalFont.font(.mono, size: 12, relativeTo: .caption)).tracking(0.7).lineLimit(1)
    }

    /// Card radius is the system's (about 26): block inset 8 gives 18, Join inset 8 inside it gives 10.
    public var body: some View {
        VStack(spacing: 8) {
            if ended {
                HStack(spacing: 10) {
                    RoundedRectangle(cornerRadius: 2).fill(color.color).frame(width: 3, height: 22)
                    Text(state.title).font(CalFont.font(.sansMedium, size: 18, relativeTo: .headline)).foregroundStyle(Color.calInk).lineLimit(1)
                    Spacer(minLength: 4)
                    Text("Ended \(format.time(state.end))").font(CalFont.font(.sans, size: 14, relativeTo: .footnote)).foregroundStyle(Color.calInk2)
                    Text("DONE").font(CalFont.font(.mono, size: 10, relativeTo: .caption2)).tracking(1).foregroundStyle(Color.calInk2)
                }
                .padding(.horizontal, 16).padding(.vertical, 14)
            } else {
                HStack(spacing: 10) {
                    FlipCells(format.flipTime(state.start), size: .small, onBlock: true).scaleEffect(22.0 / 18.0, anchor: .leading).frame(width: 66)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(state.title).font(CalFont.font(.sansMedium, size: 20, relativeTo: .headline)).lineLimit(1)
                        meta.opacity(0.72)
                    }
                    .foregroundStyle(Color(red: 10 / 255, green: 10 / 255, blue: 11 / 255))
                    Spacer(minLength: 4)
                    if let joinURL {
                        Link(destination: joinURL) {
                            Text("Join").font(CalFont.font(.sansMedium, size: 15, relativeTo: .body)).foregroundStyle(Color(red: 244 / 255, green: 241 / 255, blue: 234 / 255))
                                .padding(.horizontal, 18).frame(height: 36)
                                .background(RoundedRectangle(cornerRadius: 10).fill(Color(red: 10 / 255, green: 10 / 255, blue: 11 / 255)))
                        }
                    }
                }
                .padding(.horizontal, 8).frame(height: 60)
                .background(RoundedRectangle(cornerRadius: 18).fill(color.bright))
                progressLine.padding(.horizontal, 16).padding(.bottom, 6)
            }
        }
        .padding(8)
        .widgetURL(SurfaceLink.event(attributes.occurrenceId).url)
        .accessibilityElement(children: .combine)
    }

    /// 4 pt rounded line: share of the 30 min lead, then of the event. Ink fill; amber is for needs-you.
    @ViewBuilder private var progressLine: some View {
        let span = running ? state.start...max(state.end, state.start.addingTimeInterval(1))
                           : state.start.addingTimeInterval(-LiveActivityPlanner.lead)...state.start
        ProgressView(timerInterval: span, countsDown: false, label: { EmptyView() }, currentValueLabel: { EmptyView() })
            .progressViewStyle(.linear)
            .tint(Color.calInk)
            .frame(height: 4).clipShape(Capsule())
    }
}
#endif
