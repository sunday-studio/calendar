#if canImport(ActivityKit)
import CalendarCore
import CalendarUI
import SurfacesCore
import SwiftUI
import WidgetKit

/// Lock-screen Live Activity (spec w-live): bright account block with flip cells, title, live words, Join, 3pt progress line.
/// Mirrors CalendarUI's `LiveActivityBlock` but takes live `Text` for the footer (a `String` cannot count down).
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

    public var body: some View {
        VStack(spacing: 0) {
            HStack(alignment: .top, spacing: 14) {
                FlipCells(format.flipTime(state.start), size: .small, onBlock: true)
                VStack(alignment: .leading, spacing: 4) {
                    Text(state.title).font(CalFont.font(.sansMedium, size: 20, relativeTo: .headline)).lineLimit(2)
                    footer.font(CalFont.font(.sans, size: 14, relativeTo: .footnote))
                }
                .foregroundStyle(Theme.onAccountColor.color)
                Spacer(minLength: 0)
                if let joinURL {
                    Link(destination: joinURL) {
                        Text("Join").font(CalFont.font(.sans, size: 15, relativeTo: .body)).foregroundStyle(Theme.flipInk.color)
                            .padding(.horizontal, 18).padding(.vertical, 8)
                            .background(Theme.onAccountColor.color)
                    }
                }
            }
            .padding(16)
            .background(color.bright)
            progressLine
        }
        .overlay(Rectangle().strokeBorder(Color(red: 0x23/255, green: 0x23/255, blue: 0x26/255), lineWidth: 1))
        .widgetURL(SurfaceLink.event(attributes.occurrenceId).url)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var progressLine: some View {
        let span = running ? state.start...max(state.end, state.start.addingTimeInterval(1))
                           : state.start.addingTimeInterval(-LiveActivityPlanner.lead)...state.start
        ProgressView(timerInterval: span, countsDown: false, label: { EmptyView() }, currentValueLabel: { EmptyView() })
            .progressViewStyle(.linear)
            .tint(Color.calAccent)
            .frame(height: 3)
            .background(Color(red: 0x22/255, green: 0x22/255, blue: 0x25/255))
    }
}
#endif
