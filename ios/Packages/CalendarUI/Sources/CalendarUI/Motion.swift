import SwiftUI

/// Motion: one curve ("settle", cubic-bezier(.2,.8,.2,1)). Reduce Motion swaps movement for a fade.
public enum Motion {
    public static let settleDuration: Double = 0.35
    public static let settle = Animation.timingCurve(0.2, 0.8, 0.2, 1, duration: settleDuration)
    public static let fade = Animation.easeOut(duration: 0.15)

    /// Animation to use for state changes given the Reduce Motion setting.
    public static func animation(reduceMotion: Bool) -> Animation {
        reduceMotion ? fade : settle
    }

    /// Insertion/removal transition: slide-settle normally, plain fade under Reduce Motion.
    public static func transition(reduceMotion: Bool, edge: Edge = .bottom) -> AnyTransition {
        reduceMotion ? .opacity : .move(edge: edge).combined(with: .opacity)
    }
}

private struct SettleModifier<V: Equatable>: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let value: V
    func body(content: Content) -> some View {
        content.animation(Motion.animation(reduceMotion: reduceMotion), value: value)
    }
}

public extension View {
    /// Animate changes of `value` with the settle curve (fade under Reduce Motion).
    func settle<V: Equatable>(value: V) -> some View {
        modifier(SettleModifier(value: value))
    }
}
