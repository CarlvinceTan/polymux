import CoreGraphics
import Foundation
import Testing
@testable import AvatarKit

private struct Reference: Decodable {
    struct P: Decodable { var x: Double; var y: Double }
    struct Shape: Decodable { var id: String; var radii: [Double] }
    struct Gaze: Decodable { var yaw: Double; var pitch: Double; var roll: Double }
    struct Eye: Decodable { var x: Double; var y: Double; var a: Double; var b: Double; var c: Double; var d: Double; var depth: Double }
    struct Face: Decodable { var gaze: Gaze; var scale: Double; var split: Double; var poses: [Eye] }
    struct Life: Decodable {
        struct Value: Decodable { var dYaw: Double; var dPitch: Double; var dRoll: Double; var lid: Double; var driftX: Double; var driftY: Double; var breath: Double }
        var t: Double
        var value: Value
    }
    struct Fit: Decodable { var shape: String; var state: String; var expression: String?; var point: P }
    var commit: String
    var rng: [Double]
    var shapes: [Shape]
    var gazes: [Face]
    var liveliness: [Life]
    var fitting: [Fit]
    var points: [P]
    var path: String
}

private func reference() throws -> Reference {
    try JSONDecoder().decode(Reference.self, from: Data(ReferenceFixture.json.utf8))
}

@Suite
struct CoreTests {
    @Test func catalogsPreserveEveryUpstreamOption() {
        #expect(ShapeID.allCases.count == 8)
        #expect(ColorID.allCases.count == 12)
        #expect(ExpressionID.allCases.count == 16)
        #expect(SHAPES.map(\.id) == ShapeID.allCases)
        #expect(COLORS.map(\.id) == ColorID.allCases)
        #expect(EXPRESSIONS.map(\.id) == ExpressionID.allCases)
        #expect(Set(COLORS.map(\.hex)).count == 12)
        #expect(DEFAULT_SHAPE.rawValue == "cercle")
        #expect(DEFAULT_COLOR.rawValue == "encre")
        #expect(DEFAULT_EXPRESSION.rawValue == "neutre")
    }

    @Test func deterministicMathMatchesPinnedReference() throws {
        let fixture = try reference()
        let rng = createRng(0x5eed)
        for expected in fixture.rng { #expect(rng() == expected) }
        #expect(r2(-1.005) == floor(-1.005 * 100 + 0.5) / 100)
        #expect(mixHex("#0a0a0c", "#f1efe9", 0) == "#0a0a0c")
        #expect(mixHex("#0a0a0c", "#f1efe9", 1) == "#f1efe9")
    }

    @Test func fullRadialProfilesMatchPinnedReference() throws {
        for shape in try reference().shapes {
            let native = try #require(SHAPE_BY_ID[shape.id])
            #expect(native.radii.count == PROFILE_SAMPLES)
            for (actual, expected) in zip(native.radii, shape.radii) {
                #expect(abs(actual - expected) < 1e-12)
                #expect(actual.isFinite && actual > 0)
            }
        }
        let triangle = silhouette(.triangle).radii
        for angle in [-30.0, -12.5, -0.1, 7.3] {
            for turn in [-3.0, -1, 1, 5] {
                #expect(abs(radiusAtAngle(triangle, angle) - radiusAtAngle(triangle, angle + turn * TAU)) < 1e-10)
            }
        }
        let a = circle(1, rot: 170 * .pi / 180), b = circle(1, rot: -170 * .pi / 180)
        #expect(abs(blend(a, b, 0.5).rot - .pi) < 1e-12)
    }

    @Test func sphereProjectionMatchesMeasuredMatrices() throws {
        for sample in try reference().gazes {
            let gaze = HeadGaze(yaw: sample.gaze.yaw, pitch: sample.gaze.pitch, roll: sample.gaze.roll)
            let poses = eyePoses(gaze, sample.scale, sample.split)
            for (actual, expected) in zip(poses, sample.poses) {
                let a = [actual.x, actual.y, actual.a, actual.b, actual.c, actual.d, actual.depth]
                let b = [expected.x, expected.y, expected.a, expected.b, expected.c, expected.d, expected.depth]
                for (one, two) in zip(a, b) { #expect(abs(one - two) < 1e-12) }
                #expect(abs(actual.a * actual.d - actual.b * actual.c - actual.depth) < 1e-12)
            }
        }
    }

    @Test func blinkAndLivelinessMatchPinnedReference() throws {
        for sample in try reference().liveliness {
            let actual = liveliness(sample.t)
            let expected = sample.value
            let a = [actual.dYaw, actual.dPitch, actual.dRoll, actual.lid, actual.driftX, actual.driftY, actual.breath]
            let b = [expected.dYaw, expected.dPitch, expected.dRoll, expected.lid, expected.driftX, expected.driftY, expected.breath]
            for (one, two) in zip(a, b) { #expect(abs(one - two) < 1e-12) }
        }
        let still = liveliness(1.481, wander: 0, blink: false, float: false)
        #expect(still.dYaw == 0 && still.dPitch == 0 && still.dRoll == 0)
        #expect(still.lid == 1 && still.driftX == 0 && still.driftY == 0 && still.breath == 1)
        #expect(blinkScale(0) == 0.06)
    }

    @Test func nativePathKeepsReferenceControlPoints() throws {
        let fixture = try reference()
        let points = toPoints(silhouette(.triangle, rot: 0.27, cx: 0.02, cy: 0.08, sx: 0.93, sy: 1.08), 100)
        for (actual, expected) in zip(points, fixture.points) {
            #expect(abs(actual.x - expected.x) < 1e-12 && abs(actual.y - expected.y) < 1e-12)
        }
        var native: [Double] = []
        closedPath(points).applyWithBlock { pointer in
            let element = pointer.pointee
            let count: Int
            switch element.type {
            case .moveToPoint, .addLineToPoint: count = 1
            case .addQuadCurveToPoint: count = 2
            case .addCurveToPoint: count = 3
            case .closeSubpath: count = 0
            @unknown default: count = 0
            }
            for i in 0..<count { native.append(element.points[i].x); native.append(element.points[i].y) }
        }
        let regex = try NSRegularExpression(pattern: "-?\\d+(?:\\.\\d+)?")
        let numbers = regex.matches(in: fixture.path, range: NSRange(fixture.path.startIndex..., in: fixture.path)).compactMap {
            Range($0.range, in: fixture.path).flatMap { Double(fixture.path[$0]) }
        }
        #expect(native == numbers)
        let capsule = capsulePath(18.6, 41.2)
        #expect(abs(capsule.boundingBoxOfPath.width - 18.6) < 1e-9)
        #expect(abs(capsule.boundingBoxOfPath.height - 41.2) < 1e-9)
        #expect(capsule.contains(.zero))
    }

    @Test func everyEyeCorrectionMatchesPinnedReference() throws {
        let fixture = try reference()
        #expect(fixture.fitting.count == 296)
        for fit in fixture.fitting {
            let shape = try #require(SHAPE_BY_ID[fit.shape])
            let state = try #require(StateID(rawValue: fit.state))
            let actual = decalageDesYeux(shape.radii, state, fit.expression)
            #expect(abs(actual.x - fit.point.x) < 1e-9, "\(fit.shape)/\(fit.state)/\(fit.expression ?? "pose") x")
            #expect(abs(actual.y - fit.point.y) < 1e-9, "\(fit.shape)/\(fit.state)/\(fit.expression ?? "pose") y")
        }
        #expect(decalageDesYeux(nil, .idle, nil) == Point())
        #expect(decalageDesYeux([1, 0.5], .idle, nil) == Point())
        #expect(decalageDesYeux(ShapeID.circle.radii, .idle, nil) == Point())
    }

    @Test func expressionMorphInterpolatesEveryComponent() {
        let a = ExpressionID.angry.expression, b = ExpressionID.sad.expression
        let midpoint = blendExpression(a, b, 0.5)
        #expect(midpoint.eyes[0].tilt == (a.eyes[0].tilt + b.eyes[0].tilt) / 2)
        #expect(midpoint.split == (a.split + b.split) / 2)
        #expect(midpoint.gaze.pitch == (a.gaze.pitch + b.gaze.pitch) / 2)
        #expect(a.eyes[0].tilt == -a.eyes[1].tilt)
        #expect(b.eyes[0].tilt == -b.eyes[1].tilt)
        #expect(a.eyes[0].tilt * b.eyes[0].tilt < 0)
    }
}
