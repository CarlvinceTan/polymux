import CoreGraphics
import Foundation
import XCTest
@testable import AvatarKit

private struct EngineReference: Decodable {
    struct Dot: Decodable {
        var x, y, r: Double
        var opacity, depth, rot: Double?
        var color: String?
        var d: [Double]?
    }
    struct Eye: Decodable { var matrix: [Double]; var alpha: Double }
    struct Gradient: Decodable { var x1, y1, x2, y2: Double; var stops: [String] }
    struct Arc: Decodable {
        var id: String
        var front, back: [Double]
        var width, opacity: Double
        var grad: Gradient
    }
    struct Frame: Decodable {
        var body: [Double]
        var bodyAlpha: Double
        var eyes: [Eye]
        var dots: [Dot]
        var arcs: [Arc]
        var notif, notch: Dot?
        var dotsBehind: Bool
    }
    struct Sample: Decodable {
        var label, initial, shape, expression: String?
        var at: Double
        var frame: Frame
    }
    struct Particles: Decodable { var at: Double; var dots: [Dot] }
    struct Seed: Decodable { var a, k, tilt, speed, phase, sweep, hue, hueSpan, width, cx, cy: Double }
    struct CycleReference: Decodable {
        struct Clamp: Decodable { var state: StateID; var duration: Double }
        var clamps: [Clamp]
    }
    var commit: String
    var frames, transition, morph, look: [Sample]
    var rings, comet: [Seed]
    var particles: [Particles]
    var cycle: CycleReference
}

private func engineReference() throws -> EngineReference {
    let url = try XCTUnwrap(Bundle.module.url(forResource: "upstream", withExtension: "json", subdirectory: "Fixtures"))
    return try JSONDecoder().decode(EngineReference.self, from: Data(contentsOf: url))
}

private func coordinates(_ path: CGPath) -> [Double] {
    var result: [Double] = []
    path.applyWithBlock { element in
        let count: Int
        switch element.pointee.type {
        case .moveToPoint, .addLineToPoint: count = 1
        case .addQuadCurveToPoint: count = 2
        case .addCurveToPoint: count = 3
        case .closeSubpath: count = 0
        @unknown default: count = 0
        }
        for i in 0..<count { result += [element.pointee.points[i].x, element.pointee.points[i].y] }
    }
    return result
}

final class EngineTests: XCTestCase {
    func testRepeatedMotifKeepsTransitionContinuous() {
        for state in [StateID.swirl, .orbit, .play, .notify, .comet, .wink] {
            let engine = BotEngine(initial: state)
            let at = STATE_BY_ID[state]!.duration
            let before = engine.sample(at)
            engine.setState(state, at, restart: true)
            let after = engine.sample(at)
            XCTAssertEqual(before.bodyPath, after.bodyPath)
            XCTAssertEqual(before.bodyAlpha, after.bodyAlpha)
            XCTAssertEqual(before.dots.count, after.dots.count)
            XCTAssertEqual(engine.state, state)
        }
    }
    private func numbers(_ actual: [Double], _ expected: [Double], _ label: String,
                         file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertEqual(actual.count, expected.count, label, file: file, line: line)
        for (a, b) in zip(actual, expected) {
            XCTAssertEqual(a, b, accuracy: 1e-8, label, file: file, line: line)
        }
    }

    private func dot(_ actual: FrameDot?, _ expected: EngineReference.Dot?, _ label: String) {
        XCTAssertEqual(actual == nil, expected == nil, label)
        if let a = actual, let b = expected { numbers([a.x, a.y, a.r], [b.x, b.y, b.r], label) }
    }

    private func particles(_ actual: [DotRender], _ expected: [EngineReference.Dot], _ label: String) {
        XCTAssertEqual(actual.count, expected.count, label)
        for (a, b) in zip(actual, expected) {
            numbers([a.x, a.y, a.r, a.opacity], [b.x, b.y, b.r, b.opacity!], label)
            XCTAssertEqual(a.depth == nil, b.depth == nil, label)
            if let depth = a.depth, let other = b.depth { numbers([depth], [other], label) }
            XCTAssertEqual(a.rot, b.rot, label)
            XCTAssertEqual(a.color, b.color, label)
            XCTAssertEqual(a.d == nil, b.d == nil, label)
            if let path = a.d, let other = b.d { numbers(coordinates(path), other, label) }
        }
    }

    private func frame(_ actual: BotFrame, _ expected: EngineReference.Frame, _ label: String) {
        numbers(coordinates(actual.bodyPath), expected.body, label + " body")
        numbers([actual.bodyAlpha], [expected.bodyAlpha], label + " alpha")
        XCTAssertEqual(actual.eyes.count, expected.eyes.count, label + " eyes")
        for (a, b) in zip(actual.eyes, expected.eyes) {
            let m = a.matrix
            numbers([m.a, m.b, m.c, m.d, m.tx, m.ty], b.matrix, label + " eye matrix")
            numbers([a.alpha], [b.alpha], label + " eye alpha")
            XCTAssertFalse(a.d.isEmpty)
        }
        particles(actual.dots, expected.dots, label + " dots")
        XCTAssertEqual(actual.dotsBehind, expected.dotsBehind, label + " depth")
        XCTAssertEqual(actual.arcs.count, expected.arcs.count, label + " arcs")
        for (a, b) in zip(actual.arcs, expected.arcs) {
            XCTAssertEqual(a.id, b.id, label)
            numbers(coordinates(a.front), b.front, label + " front arc")
            numbers(coordinates(a.back), b.back, label + " back arc")
            numbers([a.width, a.opacity, a.grad.x1, a.grad.y1, a.grad.x2, a.grad.y2],
                    [b.width, b.opacity, b.grad.x1, b.grad.y1, b.grad.x2, b.grad.y2], label + " gradient")
            XCTAssertEqual(a.grad.stops, b.grad.stops, label + " colors")
        }
        dot(actual.notif, expected.notif, label + " notification")
        dot(actual.notch, expected.notch, label + " notch")
    }

    func testEveryStateAndCustomFaceMatchesPinnedUpstream() throws {
        let reference = try engineReference()
        XCTAssertEqual(reference.commit, "b4bb3c1b5f93c7b87a2e8d620f667c4093d97749")
        XCTAssertEqual(STATES.count, 15)
        XCTAssertEqual(SEQUENCE.count, 14)
        for sample in reference.frames {
            let initial = try XCTUnwrap(StateID(rawValue: sample.initial!))
            // thinking/alert/exclaim/sleep are Polymux badge motifs; BadgeStatesTests covers them.
            if [.thinking, .alert, .exclaim, .sleep].contains(initial) { continue }
            let engine = BotEngine(initial: initial,
                                   shape: sample.shape.flatMap { SHAPE_BY_ID[$0]?.radii },
                                   expression: sample.expression.flatMap { EXPRESSION_BY_ID[$0] })
            frame(engine.sample(sample.at), sample.frame, sample.label!)
        }
    }

    func testInterruptedTransitionFreezesCompositeAndSamplingIsPure() throws {
        let engine = BotEngine()
        engine.setState(.wide, 0.5)
        engine.setState(.idle, 0.6)
        let expected = try engineReference().transition
        for sample in expected { frame(engine.sample(sample.at), sample.frame, "interrupted \(sample.at)") }
        for sample in expected.reversed() { frame(engine.sample(sample.at), sample.frame, "replay \(sample.at)") }
    }

    func testShapeExpressionAndAbsoluteLookMorphsMatchUpstream() throws {
        let reference = try engineReference()
        let engine = BotEngine(shape: SHAPE_BY_ID["cercle"]!.radii, expression: EXPRESSION_BY_ID["neutre"]!)
        engine.setShape(SHAPE_BY_ID["capsule"]!.radii, 1)
        engine.setExpression(EXPRESSION_BY_ID["colere"]!, 1)
        for sample in reference.morph { frame(engine.sample(sample.at), sample.frame, "morph \(sample.at)") }
        let look = BotEngine()
        look.setLook(Look(yaw: -26, pitch: 10, mix: 1, spin: 0, wander: 0), 1)
        for sample in reference.look { frame(look.sample(sample.at), sample.frame, "look \(sample.at)") }
    }

    func testSeededDecorAndDurationsMatchUpstream() throws {
        let reference = try engineReference()
        for (actual, expected) in [(RINGS, reference.rings), (COMET_RIBBONS, reference.comet)] {
            XCTAssertEqual(actual.count, expected.count)
            for (a, b) in zip(actual, expected) {
                numbers([a.a, a.k, a.tilt, a.speed, a.phase, a.sweep, a.hue, a.hueSpan, a.width, a.cx, a.cy],
                        [b.a, b.k, b.tilt, b.speed, b.phase, b.sweep, b.hue, b.hueSpan, b.width, b.cx, b.cy], "seed")
            }
        }
        for sample in reference.particles { particles(AvatarKit.particles(sample.at, 1), sample.dots, "particles") }
        for sample in reference.cycle.clamps { XCTAssertEqual(clampDuration(sample.state, 0.1), sample.duration) }
    }
}
