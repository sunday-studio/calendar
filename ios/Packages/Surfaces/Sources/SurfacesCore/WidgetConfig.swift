import Foundation

/// What the widget settings (the App Intent) say, free of UI so the timeline builder can filter by it.
public struct WidgetConfig: Equatable, Sendable {
    public enum LargeLayout: String, Sendable, CaseIterable { case agenda, day }
    public enum MediumLayout: String, Sendable, CaseIterable { case list, nextList }

    public var largeLayout: LargeLayout
    public var mediumLayout: MediumLayout
    /// nil = every visible calendar.
    public var calendarIds: Set<String>?
    public var showDeclined: Bool
    /// Off by default: titles are hidden while the phone is locked.
    public var showLockTitles: Bool

    public init(largeLayout: LargeLayout = .agenda, mediumLayout: MediumLayout = .list, calendarIds: Set<String>? = nil,
                showDeclined: Bool = false, showLockTitles: Bool = false) {
        self.largeLayout = largeLayout; self.mediumLayout = mediumLayout; self.calendarIds = calendarIds
        self.showDeclined = showDeclined; self.showLockTitles = showLockTitles
    }

    public static let `default` = WidgetConfig()

    public func includes(_ calendarId: String) -> Bool { calendarIds?.contains(calendarId) ?? true }
}
