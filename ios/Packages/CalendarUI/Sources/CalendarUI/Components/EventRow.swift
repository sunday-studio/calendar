import SwiftUI

/// Display model for one agenda row. UI-only: callers map from hub models.
public struct EventRowModel: Identifiable, Sendable, Equatable {
    public enum Status: String, Sendable { case moved, cancelled, new, needsReply = "needs reply", maybe, declined, stale }

    public let id: String
    public var title: String
    public var start: String          // already formatted, e.g. "10:00"
    public var end: String?           // e.g. "10:45"
    public var location: String?
    public var color: AccountColor
    public var status: Status?
    public var isAllDay: Bool
    public var isPast: Bool
    public var overlaps: Bool
    /// Extra word after the status ("Moved 11:45").
    public var statusDetail: String?
    /// Second line when the event overlaps another: "Overlaps with Roadmap review". nil + `overlaps` = generic text.
    public var overlapWith: String?

    public init(id: String = UUID().uuidString, title: String, start: String, end: String? = nil,
                location: String? = nil, color: AccountColor, status: Status? = nil,
                isAllDay: Bool = false, isPast: Bool = false, overlaps: Bool = false,
                statusDetail: String? = nil, overlapWith: String? = nil) {
        self.id = id; self.title = title; self.start = start; self.end = end; self.location = location
        self.color = color; self.status = status; self.isAllDay = isAllDay; self.isPast = isPast; self.overlaps = overlaps
        self.statusDetail = statusDetail; self.overlapWith = overlapWith
    }

    /// Canvas status word: "Moved 11:45", "Reply needed", "Maybe", "Declined", "Cancelled", "Stale", "New".
    public var statusWord: String? {
        guard let status else { return nil }
        let base: String
        switch status {
        case .moved: base = "Moved"
        case .cancelled: base = "Cancelled"
        case .new: base = "New"
        case .needsReply: base = "Reply needed"
        case .maybe: base = "Maybe"
        case .declined: base = "Declined"
        case .stale: base = "Stale"
        }
        if let statusDetail, !statusDetail.isEmpty { return base + " " + statusDetail }
        return base
    }

    /// The overlap second line, if any.
    public var overlapLine: String? {
        guard overlaps || overlapWith != nil else { return nil }
        return overlapWith.map { "Overlaps with \($0)" } ?? "Overlaps another event"
    }

    /// Cancelled and declined events stay visible but struck through and dim.
    var isDimStatus: Bool { status == .cancelled || status == .declined || status == .stale }

    var isQuiet: Bool { status == .cancelled || status == .declined }

    /// "Design review, 10:00 to 10:45, Room 4, moved, overlaps another event"
    public var accessibilityLabel: String {
        EventRowFormatter.accessibilityLabel(
            title: title, start: start, end: end, location: location,
            status: status?.rawValue, isAllDay: isAllDay, overlaps: overlaps, isPast: isPast)
    }
}

public enum EventRowFormatter {
    public static func accessibilityLabel(title: String, start: String, end: String?, location: String?,
                                          status: String?, isAllDay: Bool, overlaps: Bool, isPast: Bool = false) -> String {
        var parts = [title]
        if isAllDay {
            parts.append("all day")
        } else if let end, !end.isEmpty {
            parts.append("\(start) to \(end)")
        } else {
            parts.append(start)
        }
        if let location, !location.isEmpty { parts.append(location) }
        if let status, !status.isEmpty { parts.append(status) }
        if overlaps { parts.append("overlaps another event") }
        if isPast { parts.append("finished") }
        return parts.joined(separator: ", ")
    }
}

/// One agenda row (canvas `.r`): h58, hairline underneath (full width), 3pt account bar flush to the screen edge,
/// mono time (dim, 48pt), 19pt title, status word on the right. Overlap adds a second red line (h72).
/// Past rows fade to 45%; there is no hatch in the agenda (hatch is for timelines).
public struct EventRow: View {
    let model: EventRowModel
    var isFirst: Bool
    @Environment(\.dynamicTypeSize) private var typeSize
    /// Time column grows with Dynamic Type so "10:00" never clips at AX5.
    @ScaledMetric(relativeTo: .subheadline) private var timeWidth: CGFloat = Spacing.timeColumn
    public init(_ model: EventRowModel, isFirst: Bool = false) {
        self.model = model
        self.isFirst = isFirst
    }

    private var overlapLine: String? { model.overlapLine }

    public var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: Spacing.l) {
                Text(model.isAllDay ? "All day" : model.start).font(CalFont.time).foregroundStyle(Color.calInk2)
                    .lineLimit(1).minimumScaleFactor(0.7)
                    .frame(width: min(timeWidth, 110), alignment: .leading)
                Text(model.title)
                    .font(CalFont.heading)
                    .strikethrough(model.isQuiet, color: .calStrike)
                    .foregroundStyle(model.isQuiet ? Color.calStrike : Color.calInk)
                    .lineLimit(typeSize.isAccessibilitySize ? 3 : 1)
                    .fixedSize(horizontal: false, vertical: typeSize.isAccessibilitySize)
                Spacer(minLength: Spacing.s)
                if !typeSize.isAccessibilitySize, let word = model.statusWord {
                    Text(word).font(CalFont.status).lineLimit(1)
                        .foregroundStyle(model.isDimStatus ? Color.calInk2 : Color.calAmber)
                }
            }
            if typeSize.isAccessibilitySize, let word = model.statusWord {
                Text(word).font(CalFont.status)
                    .foregroundStyle(model.isDimStatus ? Color.calInk2 : Color.calAmber)
                    .padding(.leading, min(timeWidth, 110) + Spacing.l)
            }
            if let line = overlapLine {
                Text(line).font(CalFont.sub).foregroundStyle(Color.calRed).lineLimit(1)
                    .padding(.leading, min(timeWidth, 110) + Spacing.l)
            }
        }
        .padding(.horizontal, Spacing.gutter)
        .padding(.vertical, overlapLine == nil ? 0 : 12)
        .padding(.vertical, typeSize.isAccessibilitySize ? 10 : 0)
        .frame(maxWidth: .infinity, minHeight: overlapLine == nil ? Spacing.rowHeight : Spacing.rowHeightTall, alignment: .leading)
        .overlay(alignment: .leading) {
            Rectangle().fill(model.isQuiet ? Color.calStrike : model.color.color).frame(width: Spacing.colorBar)
                .padding(.vertical, 15)
        }
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .overlay(alignment: .top) { if isFirst { Rectangle().fill(Color.calLine).frame(height: 1) } }
        .opacity(model.isPast ? 0.45 : 1)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(model.accessibilityLabel)
    }
}
