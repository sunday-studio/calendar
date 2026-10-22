import SwiftUI

#Preview("EventRow") {
    VStack(spacing: 0) {
        EventRow(.init(title: "Dentist", start: "11:00", color: .cobalt, status: .moved, statusDetail: "11:45"), isFirst: true)
        EventRow(.init(title: "Budget check-in", start: "11:30", color: .rose, status: .maybe, overlapWith: "Dentist"))
        EventRow(.init(title: "Standup", start: "09:00", color: .rose, isPast: true))
    }.background(Color.calBg)
}

#Preview("NextEventBlock dark") {
    NextEventBlock(time: "10:30", title: "Roadmap review", footer: "in 8 min", color: .rose, action: .join("Join"))
        .environment(\.colorScheme, .dark)
}

#Preview("Chrome") {
    VStack(spacing: 8) {
        DaySectionHeader(title: "Today")
        AmberBanner("[work email] needs you to sign in again.", message: "Its events are paused.", actionTitle: "Fix", action: {})
        BrassLine(label: "09:52")
        ToastView("Moved to 11:45", actionTitle: "Undo", action: {})
        SkeletonRow()
        EmptyState(title: "Clear until Thursday", message: "Next: Design review, Thu 14:00")
        CalButton("Primary", action: {})
        CalButton("Outline", kind: .outline, action: {})
    }.background(Color.calBg)
}
