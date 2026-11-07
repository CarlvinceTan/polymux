import SwiftUI

/// The app's motion. Every animation uses one of these, and none plays under Reduce Motion.
enum Motion {
    /// Column slides, kept in step with AppKit's sidebar animation.
    static let columnTime: TimeInterval = 0.22
    /// What arrives under the pointer: it must answer in the same frame.
    static let instant = Animation.linear(duration: 0)
    /// Hover, fades and small state changes.
    static let quick = Animation.easeOut(duration: 0.16)
    /// Panels, columns and anything that moves across the window.
    static let move = Animation.timingCurve(0.22, 1, 0.36, 1, duration: columnTime)
    /// Content arriving or leaving on its own, such as a note or a loaded image.
    static let fade = Animation.easeInOut(duration: 0.2)
}

extension Animation {
    func unless(_ reduced: Bool) -> Animation? { reduced ? nil : self }
}

private struct MotionValue<Value: Equatable>: ViewModifier {
    let animation: Animation
    let value: Value
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content.animation(animation.unless(reduceMotion), value: value)
    }
}

extension View {
    func motion<Value: Equatable>(_ animation: Animation = Motion.quick, value: Value) -> some View {
        modifier(MotionValue(animation: animation, value: value))
    }
}

/// Slides a view in from the trailing edge; glass inside only follows an offset recomputed each frame.
struct EdgeSlide: Transition {
    var width: CGFloat
    func body(content: Content, phase: TransitionPhase) -> some View {
        content.modifier(SlideOffset(progress: phase.isIdentity ? 1 : 0, width: width))
    }
}

private nonisolated struct SlideOffset: ViewModifier, Animatable {
    var progress: CGFloat
    let width: CGFloat

    var animatableData: CGFloat {
        get { progress }
        set { progress = newValue }
    }

    func body(content: Content) -> some View {
        content
            .offset(x: (1 - progress) * width)
            .transaction { $0.animation = nil }
    }
}
