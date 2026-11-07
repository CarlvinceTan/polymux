import AvatarKit
import AvatarKitC
import AvatarKitCTypes
import CoreGraphics
import Foundation
import Testing

@Test func samplesNativeFrames() throws {
    let engine = try #require(ak_engine_create(100, "idle"))
    defer { ak_engine_release(engine) }

    #expect(ak_engine_set_shape(engine, "cat", 0))
    #expect(ak_engine_set_expression(engine, "heureux", 0))
    #expect(ak_engine_set_state(engine, "notify", 0.1, false))
    #expect(!ak_engine_set_state(engine, "missing", 0.1, false))

    let frame = try #require(ak_engine_sample(engine, 0.2))
    defer { ak_frame_release(frame) }
    let body = try #require(ak_frame_body_path(frame))
    #expect(!body.takeUnretainedValue().isEmpty)
    #expect(ak_frame_eye_count(frame) == 2)
    #expect(ak_frame_part_count(frame) > 0)

    var notice = AKFrameDot()
    #expect(ak_frame_notif(frame, &notice))
    #expect(notice.r > 0)

    let state = try #require(ak_engine_copy_state(engine))
    #expect(String(cString: state) == "notify")
    ak_string_free(state)
    let color = try #require(ak_frame_part_copy_color(frame, 0))
    #expect(String(cString: color).hasPrefix("#"))
    ak_string_free(color)
}

@Test func framesOutliveTheirEngine() throws {
    let engine = try #require(ak_engine_create(100, "idle"))
    let frame = try #require(ak_engine_sample(engine, 1))
    ak_engine_release(engine)
    defer { ak_frame_release(frame) }

    let body = try #require(ak_frame_body_path(frame))
    #expect(!body.takeUnretainedValue().isEmpty)
}

@Test func preservesProfileGeometry() throws {
    let engine = try #require(ak_engine_create(100, "idle"))
    defer { ak_engine_release(engine) }
    #expect(ak_engine_set_shape(engine, "cat", 0))
    ak_engine_set_turn(engine, 25, -15, 0)

    #expect(Int(AK_PROFILE_SAMPLES) == PROFILE_SAMPLES)
    let radii = (0..<Int(AK_PROFILE_SAMPLES)).map {
        0.8 + 0.2 * cos(Double($0) * 2 * .pi / Double(AK_PROFILE_SAMPLES))
    }
    #expect(ak_engine_set_profile(engine, radii, radii.count, 0))
    let frame = try #require(ak_engine_sample(engine, 1))
    defer { ak_frame_release(frame) }
    let expected = BotEngine(scale: 100, initial: .idle, shape: radii,
                             expression: ExpressionID.neutral.expression).sample(1)
    let body = try #require(ak_frame_body_path(frame)).takeUnretainedValue()
    #expect(body == expected.bodyPath)
    #expect(ak_frame_part_count(frame) == 0)
    #expect(ak_frame_eye_count(frame) == expected.eyes.count)
    for (index, eye) in expected.eyes.enumerated() {
        let path = try #require(ak_frame_eye_path(frame, index)).takeUnretainedValue()
        #expect(path == eye.d)
        #expect(ak_frame_eye_transform(frame, index) == eye.matrix)
        #expect(ak_frame_eye_alpha(frame, index) == eye.alpha)
    }
}

@Test func exposesCustomProfilesAndInteractions() throws {
    let engine = try #require(ak_engine_create(100, "idle"))
    defer { ak_engine_release(engine) }
    let radii = superellipseProfile(2, 0.7, 1.1)
    #expect(radii.count == Int(AK_PROFILE_SAMPLES))
    #expect(ak_engine_set_profile(engine, radii, radii.count, 0))
    #expect(ak_engine_set_solid(engine, AKSolidInflated, 1, 0))
    #expect(ak_engine_set_gaze(engine, [0.2, -0.1], 2))
    #expect(!ak_engine_set_gaze(engine, [0.2], 1))
    #expect(ak_engine_poke(engine, AKPokeBounce, 0, 0))
    #expect(ak_engine_set_mood(engine, "working", false))
    #expect(ak_engine_set_mood(engine, nil, false))
    #expect(ak_engine_set_expression(engine, nil, 0))

    ak_engine_set_reduced_motion(engine, true)
    ak_engine_set_display_size(engine, 80)
    ak_engine_set_interactive(engine, true)
    ak_engine_set_personality(engine, 42)
    ak_engine_set_resting(engine, true)
    ak_engine_set_live(engine, true)
    let frame = try #require(ak_engine_sample(engine, 0.1))
    defer { ak_frame_release(frame) }
    #expect(ak_frame_body_path(frame) != nil)
}
