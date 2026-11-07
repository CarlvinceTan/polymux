#ifndef AVATAR_KIT_C_H
#define AVATAR_KIT_C_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <CoreGraphics/CoreGraphics.h>

#ifdef __cplusplus
extern "C" {
#endif

#define AK_API_VERSION 1

typedef struct AKEngine *AKEngineRef;
typedef struct AKFrame *AKFrameRef;

typedef struct {
    double x, y, r;
} AKFrameDot;

typedef struct {
    double yaw, pitch, mix, spin, wander;
} AKLook;

typedef struct {
    double x1, y1, x2, y2;
} AKArcGradient;

typedef enum {
    AKSolidInflated = 0,
    AKSolidLoft,
    AKSolidSlab,
    AKSolidRevolved,
    AKSolidPill,
    AKSolidPumpkin
} AKSolid;

typedef enum {
    AKPokeSpin = 0,
    AKPokeBounce,
    AKPokeNod
} AKPoke;

// Engines and frames are thread-confined; separate instances can run concurrently.
AKEngineRef ak_engine_create(double scale, const char *initial_state);
void ak_engine_release(AKEngineRef engine);
char *ak_engine_copy_state(AKEngineRef engine);

// Shape identifiers match ShapeID.rawValue; setting one also selects its solid and character.
bool ak_engine_set_shape(AKEngineRef engine, const char *shape, double now);
#define AK_PROFILE_SAMPLES 64
// Requires exactly AK_PROFILE_SAMPLES (64) radii; success clears the built-in solid and character.
// A null engine, null radii or any other count returns false without changing the engine.
bool ak_engine_set_profile(AKEngineRef engine, const double *radii, size_t count, double now);
bool ak_engine_set_solid(AKEngineRef engine, AKSolid solid, double depth, double bevel);
bool ak_engine_set_expression(AKEngineRef engine, const char *expression, double now);
bool ak_engine_set_state(AKEngineRef engine, const char *state, double now, bool restart);
bool ak_engine_reset(AKEngineRef engine, const char *state, double now);
bool ak_engine_set_mood(AKEngineRef engine, const char *mood, bool interaction);
void ak_engine_set_look(AKEngineRef engine, AKLook look, double now, double morph);
bool ak_engine_set_gaze(AKEngineRef engine, const double *point, size_t count);
void ak_engine_set_turn(AKEngineRef engine, double yaw, double pitch, double now);
bool ak_engine_poke(AKEngineRef engine, AKPoke poke, int turns, double now);
void ak_engine_pet(AKEngineRef engine, bool active, double now);
void ak_engine_twirl(AKEngineRef engine, double now);
bool ak_engine_settled(AKEngineRef engine, double now);

void ak_engine_set_display_size(AKEngineRef engine, double size);
void ak_engine_set_reduced_motion(AKEngineRef engine, bool reduced);
void ak_engine_set_interactive(AKEngineRef engine, bool interactive);
void ak_engine_set_personality(AKEngineRef engine, uint32_t personality);
void ak_engine_set_resting(AKEngineRef engine, bool resting);
void ak_engine_set_live(AKEngineRef engine, bool live);

// Sampling retains one immutable frame; release it after all borrowed paths are no longer needed.
AKFrameRef ak_engine_sample(AKEngineRef engine, double now);
void ak_frame_release(AKFrameRef frame);
void ak_string_free(char *string);

CGPathRef ak_frame_body_path(AKFrameRef frame);
double ak_frame_body_alpha(AKFrameRef frame);
double ak_frame_bare(AKFrameRef frame);
bool ak_frame_dots_behind(AKFrameRef frame);
CGAffineTransform ak_frame_transform(AKFrameRef frame);
bool ak_frame_notif(AKFrameRef frame, AKFrameDot *dot);
bool ak_frame_notch(AKFrameRef frame, AKFrameDot *dot);

size_t ak_frame_eye_count(AKFrameRef frame);
CGPathRef ak_frame_eye_path(AKFrameRef frame, size_t index);
CGAffineTransform ak_frame_eye_transform(AKFrameRef frame, size_t index);
double ak_frame_eye_alpha(AKFrameRef frame, size_t index);

size_t ak_frame_part_count(AKFrameRef frame);
CGPathRef ak_frame_part_path(AKFrameRef frame, size_t index);
CGPathRef ak_frame_part_outline(AKFrameRef frame, size_t index);
char *ak_frame_part_copy_color(AKFrameRef frame, size_t index);
bool ak_frame_part_front(AKFrameRef frame, size_t index);
bool ak_frame_part_surface(AKFrameRef frame, size_t index);
double ak_frame_part_alpha(AKFrameRef frame, size_t index);

size_t ak_frame_dot_count(AKFrameRef frame);
AKFrameDot ak_frame_dot(AKFrameRef frame, size_t index);
CGPathRef ak_frame_dot_path(AKFrameRef frame, size_t index);
char *ak_frame_dot_copy_color(AKFrameRef frame, size_t index);
double ak_frame_dot_opacity(AKFrameRef frame, size_t index);
bool ak_frame_dot_depth(AKFrameRef frame, size_t index, double *depth);
bool ak_frame_dot_rotation(AKFrameRef frame, size_t index, double *rotation);

size_t ak_frame_arc_count(AKFrameRef frame);
char *ak_frame_arc_copy_id(AKFrameRef frame, size_t index);
CGPathRef ak_frame_arc_front(AKFrameRef frame, size_t index);
CGPathRef ak_frame_arc_back(AKFrameRef frame, size_t index);
double ak_frame_arc_width(AKFrameRef frame, size_t index);
double ak_frame_arc_opacity(AKFrameRef frame, size_t index);
AKArcGradient ak_frame_arc_gradient(AKFrameRef frame, size_t index);
size_t ak_frame_arc_stop_count(AKFrameRef frame, size_t index);
char *ak_frame_arc_copy_stop(AKFrameRef frame, size_t index, size_t stop);

#ifdef __cplusplus
}
#endif
#endif
