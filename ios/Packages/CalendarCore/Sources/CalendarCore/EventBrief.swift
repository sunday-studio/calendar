import Foundation

/// `GET /v1/events/{id}/brief`: just enough to name an event in a notification.
public struct EventBrief: Codable, Hashable, Sendable {
    public var title: String
    public var start: Date?
    public var allDay: Bool
    public var date: String?
    public var place: String?
    public var organizer: String?
    public var cancelled: Bool

    public init(title: String, start: Date? = nil, allDay: Bool = false, date: String? = nil, place: String? = nil,
                organizer: String? = nil, cancelled: Bool = false) {
        self.title = title; self.start = start; self.allDay = allDay; self.date = date; self.place = place
        self.organizer = organizer; self.cancelled = cancelled
    }
}
