import CoreGraphics
import Foundation
import Testing
@testable import AvatarKit

// Polymux keeps the body on screen for these motifs and moves the indicator to the
// top-right corner, so they diverge from the pinned upstream fixture.
@Suite
struct BadgeStatesTests {
    private let states: [StateID] = [.thinking, .alert, .exclaim, .sleep]
    private let faces: [StateID] = [.thinking, .alert, .exclaim]

    @Test func bodiesStayWholeWithMarksInTheCorner() {
        for state in states {
            let def = STATE_BY_ID[state]!
            #expect(def.baseBody, "\(state.rawValue) keeps the chosen shape")
            for t in stride(from: 0.0, through: 3.2, by: 0.08) {
                let pose = def.pose(t)
                #expect(pose.bodyAlpha == 1, "\(state.rawValue) body visible at \(t)")
                #expect(pose.eyeAlpha == 1, "\(state.rawValue) eyes visible at \(t)")
                #expect(pose.sil.radii == circle(1).radii, "\(state.rawValue) body is whole at \(t)")
                #expect(pose.notif == nil)
                #expect(!pose.dotsBehind, "body draws over the marks at \(t)")
                #expect(!pose.dots.isEmpty)
                for dot in pose.dots where dot.opacity > 0.02 {
                    #expect(dot.x > 0.3 && dot.y < -0.3, "\(state.rawValue) mark sits top-right at \(t)")
                }
            }
        }
    }

    @Test func markCountsAndMotionArePinned() {
        func marks(_ state: StateID, _ t: Double) -> [DotRender] {
            STATE_BY_ID[state]!.pose(t).dots
        }
        for t in stride(from: 0.0, through: 2.4, by: 0.2) {
            #expect(marks(.thinking, t).count == 3)
            let question = marks(.alert, t)
            #expect(question.count == 1 && question[0].d != nil, "alert shows the question glyph")
            let exclaim = marks(.exclaim, t)
            #expect(exclaim.count == 2 && exclaim.filter { $0.d != nil }.count == 1, "exclaim shows the bar and dot")
            #expect(marks(.sleep, t).count == 6)
            let row = marks(.thinking, t)
            #expect(row.allSatisfy { abs($0.y - row[0].y) < 1e-12 }, "thinking dots stay level")
            #expect(row[0].x < row[1].x && row[1].x < row[2].x, "thinking dots stay left to right")
        }
        for state in states {
            let a = marks(state, 0.35)
            let b = marks(state, 1.45)
            let moved = zip(a, b).contains { one, two in
                abs(one.x - two.x) > 1e-6 || abs(one.y - two.y) > 1e-6
                    || abs(one.opacity - two.opacity) > 1e-6 || abs(one.r - two.r) > 1e-6
            }
            #expect(moved, "\(state.rawValue) motif animates")
        }
        let sleep = (0..<24).flatMap { STATE_BY_ID[.sleep]!.pose(Double($0) * 0.1).dots.map(\.opacity) }
        #expect(sleep.contains { $0 < 0.05 } && sleep.contains { $0 > 0.9 }, "zZ rise and fade")
    }

    @Test func contextualFacesApplyAndIntrinsicSleepDoesNot() {
        for state in faces {
            #expect(STATE_BY_ID[state]!.baseFace, "\(state.rawValue) accepts an expression")
            let neutral = BotEngine(scale: 100, initial: state, expression: ExpressionID.neutral.expression)
            let sad = BotEngine(scale: 100, initial: state, expression: ExpressionID.sad.expression)
            let one = neutral.sample(1.4), two = sad.sample(1.4)
            #expect(one.eyes.count == 2 && two.eyes.count == 2)
            #expect(one.eyes[0].matrix != two.eyes[0].matrix, "\(state.rawValue) face follows the expression")
            #expect(one.bodyPath == two.bodyPath, "\(state.rawValue) body ignores the expression")
        }
        #expect(!STATE_BY_ID[.sleep]!.baseFace)
        let calm = BotEngine(scale: 100, initial: .sleep, expression: ExpressionID.neutral.expression).sample(1.4)
        let angry = BotEngine(scale: 100, initial: .sleep, expression: ExpressionID.angry.expression).sample(1.4)
        #expect(calm.eyes[0].matrix == angry.eyes[0].matrix)
        #expect(calm.bodyPath == angry.bodyPath)
    }

    @Test func bodyIsDrawnFullSizeAcrossShapes() {
        for shape in SHAPES {
            let resting = BotEngine(scale: 100, initial: .idle, shape: shape.radii,
                                    expression: ExpressionID.neutral.expression)
            for state in states {
                let engine = BotEngine(scale: 100, initial: state, shape: shape.radii,
                                       expression: ExpressionID.neutral.expression)
                for t in [0.5, 0.9, 1.8] {
                    let frame = engine.sample(t)
                    let box = frame.bodyPath.boundingBoxOfPath
                    #expect(box.width > 100 && box.height > 100,
                            "\(shape.id.rawValue)/\(state.rawValue) body full size at \(t)")
                    #expect(frame.bodyPath == resting.sample(t).bodyPath,
                            "\(shape.id.rawValue)/\(state.rawValue) keeps the exact body at \(t)")
                    #expect(frame.notif == nil)
                    #expect(!frame.dots.isEmpty)
                }
            }
        }
    }

    @Test func contextualEyesStayInsideEveryShape() {
        for shape in SHAPES {
            for state in faces {
                for expression in EXPRESSIONS {
                    let engine = BotEngine(scale: 100, initial: state, shape: shape.radii,
                                           expression: expression)
                    let frame = engine.sample(1.4)
                    for eye in frame.eyes {
                        for point in outlinePoints(eye.d) {
                            let unit = CGPoint(x: point.x, y: point.y).applying(eye.matrix)
                            let x = unit.x / 100, y = unit.y / 100
                            let limit = radiusAtAngle(shape.radii, atan2(y, x) - 0)
                            let radius = hypot(x, y)
                            #expect(radius < limit + 0.03,
                                    "\(shape.id.rawValue)/\(state.rawValue)/\(expression.id.rawValue) eye escapes body")
                        }
                    }
                }
            }
        }
    }

    @Test func everyTransitionRendersAValidFrame() {
        for from in StateID.allCases {
            for to in StateID.allCases {
                let engine = BotEngine(scale: 100, initial: from, shape: ShapeID.circle.radii,
                                       expression: ExpressionID.neutral.expression)
                _ = engine.sample(0.5)
                engine.setState(to, 0.5)
                for t in [0.5, 0.7, 1.2] {
                    let frame = engine.sample(t)
                    let box = frame.bodyPath.boundingBoxOfPath
                    #expect(!frame.bodyPath.isEmpty && box.width > 4 && box.width.isFinite,
                            "\(from.rawValue)->\(to.rawValue) body at \(t)")
                    for dot in frame.dots {
                        #expect(dot.x.isFinite && dot.y.isFinite && dot.r.isFinite && dot.opacity.isFinite,
                                "\(from.rawValue)->\(to.rawValue) marks at \(t)")
                    }
                }
            }
        }
    }

    private func outlinePoints(_ path: CGPath) -> [CGPoint] {
        var points: [CGPoint] = []
        path.applyWithBlock { element in
            let count: Int
            switch element.pointee.type {
            case .moveToPoint, .addLineToPoint: count = 1
            case .addQuadCurveToPoint: count = 2
            case .addCurveToPoint: count = 3
            case .closeSubpath: count = 0
            @unknown default: count = 0
            }
            if count > 0 { points.append(element.pointee.points[count - 1]) }
        }
        return points
    }
}
