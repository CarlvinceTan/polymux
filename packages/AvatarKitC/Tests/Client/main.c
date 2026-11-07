#include "AvatarKitC.h"
#include <assert.h>
#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static void assert_unchanged(AKEngineRef engine, AKFrameRef before, double now) {
    AKFrameRef after = ak_engine_sample(engine, now);
    assert(after != NULL);
    assert(CGPathEqualToPath(ak_frame_body_path(before), ak_frame_body_path(after)));
    assert(ak_frame_eye_count(before) == ak_frame_eye_count(after));
    for (size_t i = 0; i < ak_frame_eye_count(before); i++) {
        assert(CGPathEqualToPath(ak_frame_eye_path(before, i), ak_frame_eye_path(after, i)));
        assert(CGAffineTransformEqualToTransform(ak_frame_eye_transform(before, i), ak_frame_eye_transform(after, i)));
        assert(ak_frame_eye_alpha(before, i) == ak_frame_eye_alpha(after, i));
    }
    assert(ak_frame_part_count(before) == ak_frame_part_count(after));
    for (size_t i = 0; i < ak_frame_part_count(before); i++) {
        assert(CGPathEqualToPath(ak_frame_part_path(before, i), ak_frame_part_path(after, i)));
        assert(ak_frame_part_alpha(before, i) == ak_frame_part_alpha(after, i));
    }
    ak_frame_release(after);
}

int main(void) {
    AKEngineRef engine = ak_engine_create(100, "idle");
    assert(engine != NULL);
    assert(ak_engine_set_shape(engine, "cat", 0));
    assert(ak_engine_set_state(engine, "notify", 0.1, false));

    AKFrameRef frame = ak_engine_sample(engine, 0.2);
    assert(frame != NULL);
    CGPathRef body = ak_frame_body_path(frame);
    assert(body != NULL && !CGPathIsEmpty(body));
    assert(ak_frame_eye_count(frame) == 2);
    assert(ak_frame_part_count(frame) > 0);

    AKFrameDot notice;
    assert(ak_frame_notif(frame, &notice) && notice.r > 0);
    char *state = ak_engine_copy_state(engine);
    assert(state != NULL && strcmp(state, "notify") == 0);
    ak_string_free(state);

    CGColorSpaceRef space = CGColorSpaceCreateDeviceRGB();
    uint8_t *pixels = calloc(200 * 200, 4);
    CGContextRef context = CGBitmapContextCreate(pixels, 200, 200, 8, 200 * 4, space,
                                                 kCGImageAlphaPremultipliedLast);
    CGColorSpaceRelease(space);
    assert(pixels != NULL && context != NULL);
    CGContextTranslateCTM(context, 100, 100);
    CGContextSetRGBFillColor(context, 1, 1, 1, 1);
    CGContextAddPath(context, body);
    CGContextFillPath(context);
    size_t painted = 0;
    for (size_t i = 3; i < 200 * 200 * 4; i += 4) if (pixels[i] != 0) painted++;
    assert(painted > 0);

    CGContextRelease(context);
    free(pixels);

    double invalid[4097];
    for (size_t i = 0; i < sizeof(invalid) / sizeof(invalid[0]); i++) invalid[i] = 0.5;
    const size_t counts[] = {0, 1, AK_PROFILE_SAMPLES - 1, AK_PROFILE_SAMPLES + 1, 96, 4096, 4097, SIZE_MAX};
    for (size_t i = 0; i < sizeof(counts) / sizeof(counts[0]); i++) {
        assert(!ak_engine_set_profile(engine, invalid, counts[i], 0.2));
        assert_unchanged(engine, frame, 0.2);
    }
    assert(!ak_engine_set_profile(NULL, invalid, AK_PROFILE_SAMPLES, 0.2));
    assert(!ak_engine_set_profile(engine, NULL, AK_PROFILE_SAMPLES, 0.2));
    assert_unchanged(engine, frame, 0.2);

    double radii[AK_PROFILE_SAMPLES];
    for (size_t i = 0; i < AK_PROFILE_SAMPLES; i++) {
        radii[i] = 0.8 + 0.2 * cos((double)i * 2 * acos(-1) / AK_PROFILE_SAMPLES);
    }
    assert(ak_engine_set_profile(engine, radii, AK_PROFILE_SAMPLES, 0.2));
    AKFrameRef custom = ak_engine_sample(engine, 1);
    assert(custom != NULL);
    assert(!CGPathIsEmpty(ak_frame_body_path(custom)));
    assert(ak_frame_part_count(custom) == 0);
    ak_frame_release(custom);
    ak_frame_release(frame);
    ak_engine_release(engine);
    puts("AvatarKit C client: state, geometry, profile validation and Core Graphics drawing passed");
    return 0;
}
