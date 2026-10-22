import CalendarUI
import SwiftUI

/// Home's motion, built on the design `Motion` tokens. Reduce Motion: cross-fade only, no zoom, no stagger.
enum HomeMotion {
    /// Interruptible spring for things that follow a finger and settle (drop into a slot).
    static let spring = Animation.spring(response: 0.32, dampingFraction: 0.84)

}

extension View {
    /// Source of the zoom transition into Event detail. Off under Reduce Motion.
    @ViewBuilder func zoomSource(_ id: String, in ns: Namespace.ID, enabled: Bool) -> some View {
        if enabled { self.matchedTransitionSource(id: id, in: ns) } else { self }
    }

    /// Destination of the zoom transition. Off under Reduce Motion.
    @ViewBuilder func zoomDestination(_ id: String, in ns: Namespace.ID, enabled: Bool) -> some View {
        if enabled { self.navigationTransition(.zoom(sourceID: id, in: ns)) } else { self }
    }
}

/// Rows answer a touch immediately: a quick dim, no movement. Reduce Motion: no change at all.
struct PressRowStyle: ButtonStyle {
    let reduceMotion: Bool
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed && !reduceMotion ? 0.55 : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}
