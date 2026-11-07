import AvatarKit
import AvatarKitCTypes
import CoreGraphics
import Foundation

private final class FrameBox {
    let value: BotFrame
    init(_ value: BotFrame) { self.value = value }
}

private func engine(_ ref: UnsafeMutableRawPointer?) -> BotEngine? {
    ref.map { Unmanaged<BotEngine>.fromOpaque($0).takeUnretainedValue() }
}

private func box(_ ref: UnsafeMutableRawPointer?) -> FrameBox? {
    ref.map { Unmanaged<FrameBox>.fromOpaque($0).takeUnretainedValue() }
}

private func path(_ value: CGPath?) -> UnsafeRawPointer? {
    value.map { UnsafeRawPointer(Unmanaged.passUnretained($0).toOpaque()) }
}

private func string(_ value: String) -> UnsafeMutablePointer<CChar>? {
    strdup(value)
}

private func item<T>(_ values: [T], _ index: Int) -> T? {
    values.indices.contains(index) ? values[index] : nil
}

@_cdecl("ak_engine_create")
func ak_engine_create(_ scale: Double, _ initial: UnsafePointer<CChar>?) -> UnsafeMutableRawPointer? {
    guard scale.isFinite, scale > 0, let initial,
          let state = StateID(rawValue: String(cString: initial)) else { return nil }
    let shape = ShapeID.circle
    let value = BotEngine(scale: scale, initial: state, shape: shape.radii,
                          expression: ExpressionID.neutral.expression)
    value.solid = shape.solid
    value.character = shape.character
    return Unmanaged.passRetained(value).toOpaque()
}

@_cdecl("ak_engine_release")
func ak_engine_release(_ ref: UnsafeMutableRawPointer?) {
    ref.map { Unmanaged<BotEngine>.fromOpaque($0).release() }
}

@_cdecl("ak_engine_copy_state")
func ak_engine_copy_state(_ ref: UnsafeMutableRawPointer?) -> UnsafeMutablePointer<CChar>? {
    engine(ref).flatMap { string($0.state.rawValue) }
}

@_cdecl("ak_engine_set_shape")
func ak_engine_set_shape(_ ref: UnsafeMutableRawPointer?, _ name: UnsafePointer<CChar>?, _ now: Double) -> Bool {
    guard let engine = engine(ref), let name,
          let shape = ShapeID(rawValue: String(cString: name)) else { return false }
    engine.setShape(shape.radii, now)
    engine.solid = shape.solid
    engine.character = shape.character
    return true
}

@_cdecl("ak_engine_set_profile")
func ak_engine_set_profile(_ ref: UnsafeMutableRawPointer?, _ radii: UnsafePointer<Double>?, _ count: Int, _ now: Double) -> Bool {
    guard let engine = engine(ref), let radii, count == PROFILE_SAMPLES else { return false }
    engine.setShape(Array(UnsafeBufferPointer(start: radii, count: count)), now)
    engine.solid = nil
    engine.character = nil
    return true
}

@_cdecl("ak_engine_set_solid")
func ak_engine_set_solid(_ ref: UnsafeMutableRawPointer?, _ solid: AKSolid, _ depth: Double, _ bevel: Double) -> Bool {
    guard let engine = engine(ref) else { return false }
    switch solid {
    case AKSolidInflated: engine.solid = .inflated(depth: depth)
    case AKSolidLoft: engine.solid = .loft(depth: depth)
    case AKSolidSlab: engine.solid = .slab(depth: depth, bevel: bevel)
    case AKSolidRevolved: engine.solid = .revolved
    case AKSolidPill: engine.solid = .pill
    case AKSolidPumpkin: engine.solid = .pumpkin
    default: return false
    }
    return true
}

@_cdecl("ak_engine_set_expression")
func ak_engine_set_expression(_ ref: UnsafeMutableRawPointer?, _ name: UnsafePointer<CChar>?, _ now: Double) -> Bool {
    guard let engine = engine(ref) else { return false }
    guard let name else {
        engine.setExpression(nil, now)
        return true
    }
    guard let expression = ExpressionID(rawValue: String(cString: name)) else { return false }
    engine.setExpression(expression.expression, now)
    return true
}

@_cdecl("ak_engine_set_state")
func ak_engine_set_state(_ ref: UnsafeMutableRawPointer?, _ name: UnsafePointer<CChar>?, _ now: Double, _ restart: Bool) -> Bool {
    guard let engine = engine(ref), let name,
          let state = StateID(rawValue: String(cString: name)) else { return false }
    engine.setState(state, now, restart: restart)
    return true
}

@_cdecl("ak_engine_reset")
func ak_engine_reset(_ ref: UnsafeMutableRawPointer?, _ name: UnsafePointer<CChar>?, _ now: Double) -> Bool {
    guard let engine = engine(ref), let name,
          let state = StateID(rawValue: String(cString: name)) else { return false }
    engine.reset(state, now)
    return true
}

@_cdecl("ak_engine_set_mood")
func ak_engine_set_mood(_ ref: UnsafeMutableRawPointer?, _ name: UnsafePointer<CChar>?, _ interaction: Bool) -> Bool {
    guard let engine = engine(ref) else { return false }
    guard let name else {
        engine.setMood(nil, interaction: interaction)
        return true
    }
    guard let mood = MoodID(rawValue: String(cString: name)) else { return false }
    engine.setMood(mood, interaction: interaction)
    return true
}

@_cdecl("ak_engine_set_look")
func ak_engine_set_look(_ ref: UnsafeMutableRawPointer?, _ look: AKLook, _ now: Double, _ morph: Double) {
    engine(ref)?.setLook(Look(yaw: look.yaw, pitch: look.pitch, mix: look.mix,
                              spin: look.spin, wander: look.wander), now, morph: morph)
}

@_cdecl("ak_engine_set_gaze")
func ak_engine_set_gaze(_ ref: UnsafeMutableRawPointer?, _ point: UnsafePointer<Double>?, _ count: Int) -> Bool {
    guard let engine = engine(ref) else { return false }
    guard let point else {
        engine.setGaze(nil)
        return true
    }
    guard count == 2 else { return false }
    engine.setGaze([point[0], point[1]])
    return true
}

@_cdecl("ak_engine_set_turn")
func ak_engine_set_turn(_ ref: UnsafeMutableRawPointer?, _ yaw: Double, _ pitch: Double, _ now: Double) {
    engine(ref)?.setTurn(Turn(yaw: yaw, pitch: pitch), now)
}

@_cdecl("ak_engine_poke")
func ak_engine_poke(_ ref: UnsafeMutableRawPointer?, _ poke: AKPoke, _ turns: Int32, _ now: Double) -> Bool {
    guard let engine = engine(ref) else { return false }
    switch poke {
    case AKPokeSpin:
        guard turns > 0 else { return false }
        engine.poke(.spin(turns: Int(turns)), now)
    case AKPokeBounce: engine.poke(.bounce, now)
    case AKPokeNod: engine.poke(.nod, now)
    default: return false
    }
    return true
}

@_cdecl("ak_engine_pet")
func ak_engine_pet(_ ref: UnsafeMutableRawPointer?, _ active: Bool, _ now: Double) {
    engine(ref)?.pet(active, now)
}

@_cdecl("ak_engine_twirl")
func ak_engine_twirl(_ ref: UnsafeMutableRawPointer?, _ now: Double) {
    engine(ref)?.twirl(now)
}

@_cdecl("ak_engine_settled")
func ak_engine_settled(_ ref: UnsafeMutableRawPointer?, _ now: Double) -> Bool {
    engine(ref)?.settled(now) ?? true
}

@_cdecl("ak_engine_set_display_size")
func ak_engine_set_display_size(_ ref: UnsafeMutableRawPointer?, _ size: Double) {
    engine(ref)?.displaySize = size
}

@_cdecl("ak_engine_set_reduced_motion")
func ak_engine_set_reduced_motion(_ ref: UnsafeMutableRawPointer?, _ reduced: Bool) {
    engine(ref)?.reducedMotion = reduced
}

@_cdecl("ak_engine_set_interactive")
func ak_engine_set_interactive(_ ref: UnsafeMutableRawPointer?, _ interactive: Bool) {
    engine(ref)?.interactive = interactive
}

@_cdecl("ak_engine_set_personality")
func ak_engine_set_personality(_ ref: UnsafeMutableRawPointer?, _ personality: UInt32) {
    engine(ref)?.personality = personality
}

@_cdecl("ak_engine_set_resting")
func ak_engine_set_resting(_ ref: UnsafeMutableRawPointer?, _ resting: Bool) {
    engine(ref)?.resting = resting
}

@_cdecl("ak_engine_set_live")
func ak_engine_set_live(_ ref: UnsafeMutableRawPointer?, _ live: Bool) {
    engine(ref)?.live = live
}

@_cdecl("ak_engine_sample")
func ak_engine_sample(_ ref: UnsafeMutableRawPointer?, _ now: Double) -> UnsafeMutableRawPointer? {
    engine(ref).map { Unmanaged.passRetained(FrameBox($0.sample(now))).toOpaque() }
}

@_cdecl("ak_frame_release")
func ak_frame_release(_ ref: UnsafeMutableRawPointer?) {
    ref.map { Unmanaged<FrameBox>.fromOpaque($0).release() }
}

@_cdecl("ak_string_free")
func ak_string_free(_ value: UnsafeMutablePointer<CChar>?) {
    free(value)
}

@_cdecl("ak_frame_body_path")
func ak_frame_body_path(_ ref: UnsafeMutableRawPointer?) -> UnsafeRawPointer? {
    path(box(ref)?.value.bodyPath)
}

@_cdecl("ak_frame_body_alpha")
func ak_frame_body_alpha(_ ref: UnsafeMutableRawPointer?) -> Double {
    box(ref)?.value.bodyAlpha ?? 0
}

@_cdecl("ak_frame_bare")
func ak_frame_bare(_ ref: UnsafeMutableRawPointer?) -> Double {
    box(ref)?.value.bare ?? 0
}

@_cdecl("ak_frame_dots_behind")
func ak_frame_dots_behind(_ ref: UnsafeMutableRawPointer?) -> Bool {
    box(ref)?.value.dotsBehind ?? false
}

@_cdecl("ak_frame_transform")
func ak_frame_transform(_ ref: UnsafeMutableRawPointer?) -> CGAffineTransform {
    box(ref)?.value.transform ?? .identity
}

private func frameDot(_ value: FrameDot?) -> AKFrameDot {
    AKFrameDot(x: value?.x ?? 0, y: value?.y ?? 0, r: value?.r ?? 0)
}

@_cdecl("ak_frame_notif")
func ak_frame_notif(_ ref: UnsafeMutableRawPointer?, _ dot: UnsafeMutablePointer<AKFrameDot>?) -> Bool {
    guard let value = box(ref)?.value.notif else { return false }
    dot?.pointee = frameDot(value)
    return true
}

@_cdecl("ak_frame_notch")
func ak_frame_notch(_ ref: UnsafeMutableRawPointer?, _ dot: UnsafeMutablePointer<AKFrameDot>?) -> Bool {
    guard let value = box(ref)?.value.notch else { return false }
    dot?.pointee = frameDot(value)
    return true
}

@_cdecl("ak_frame_eye_count")
func ak_frame_eye_count(_ ref: UnsafeMutableRawPointer?) -> Int {
    box(ref)?.value.eyes.count ?? 0
}

private func eye(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> RenderedEye? {
    box(ref).flatMap { item($0.value.eyes, index) }
}

@_cdecl("ak_frame_eye_path")
func ak_frame_eye_path(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> UnsafeRawPointer? {
    path(eye(ref, index)?.d)
}

@_cdecl("ak_frame_eye_transform")
func ak_frame_eye_transform(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> CGAffineTransform {
    eye(ref, index)?.matrix ?? .identity
}

@_cdecl("ak_frame_eye_alpha")
func ak_frame_eye_alpha(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> Double {
    eye(ref, index)?.alpha ?? 0
}

@_cdecl("ak_frame_part_count")
func ak_frame_part_count(_ ref: UnsafeMutableRawPointer?) -> Int {
    box(ref)?.value.parts.count ?? 0
}

private func part(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> CharacterLayer? {
    box(ref).flatMap { item($0.value.parts, index) }
}

@_cdecl("ak_frame_part_path")
func ak_frame_part_path(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> UnsafeRawPointer? {
    path(part(ref, index)?.path)
}

@_cdecl("ak_frame_part_outline")
func ak_frame_part_outline(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> UnsafeRawPointer? {
    path(part(ref, index)?.outline)
}

@_cdecl("ak_frame_part_copy_color")
func ak_frame_part_copy_color(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> UnsafeMutablePointer<CChar>? {
    part(ref, index).flatMap { string($0.color) }
}

@_cdecl("ak_frame_part_front")
func ak_frame_part_front(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> Bool {
    part(ref, index)?.front ?? false
}

@_cdecl("ak_frame_part_surface")
func ak_frame_part_surface(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> Bool {
    part(ref, index)?.surface ?? false
}

@_cdecl("ak_frame_part_alpha")
func ak_frame_part_alpha(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> Double {
    part(ref, index)?.alpha ?? 0
}

@_cdecl("ak_frame_dot_count")
func ak_frame_dot_count(_ ref: UnsafeMutableRawPointer?) -> Int {
    box(ref)?.value.dots.count ?? 0
}

private func dot(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> DotRender? {
    box(ref).flatMap { item($0.value.dots, index) }
}

@_cdecl("ak_frame_dot")
func ak_frame_dot(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> AKFrameDot {
    guard let value = dot(ref, index) else { return frameDot(nil) }
    return AKFrameDot(x: value.x, y: value.y, r: value.r)
}

@_cdecl("ak_frame_dot_path")
func ak_frame_dot_path(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> UnsafeRawPointer? {
    path(dot(ref, index)?.d)
}

@_cdecl("ak_frame_dot_copy_color")
func ak_frame_dot_copy_color(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> UnsafeMutablePointer<CChar>? {
    dot(ref, index)?.color.flatMap(string)
}

@_cdecl("ak_frame_dot_opacity")
func ak_frame_dot_opacity(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> Double {
    dot(ref, index)?.opacity ?? 0
}

@_cdecl("ak_frame_dot_depth")
func ak_frame_dot_depth(_ ref: UnsafeMutableRawPointer?, _ index: Int, _ depth: UnsafeMutablePointer<Double>?) -> Bool {
    guard let value = dot(ref, index)?.depth else { return false }
    depth?.pointee = value
    return true
}

@_cdecl("ak_frame_dot_rotation")
func ak_frame_dot_rotation(_ ref: UnsafeMutableRawPointer?, _ index: Int, _ rotation: UnsafeMutablePointer<Double>?) -> Bool {
    guard let value = dot(ref, index)?.rot else { return false }
    rotation?.pointee = value
    return true
}

@_cdecl("ak_frame_arc_count")
func ak_frame_arc_count(_ ref: UnsafeMutableRawPointer?) -> Int {
    box(ref)?.value.arcs.count ?? 0
}

private func arc(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> ArcRender? {
    box(ref).flatMap { item($0.value.arcs, index) }
}

@_cdecl("ak_frame_arc_copy_id")
func ak_frame_arc_copy_id(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> UnsafeMutablePointer<CChar>? {
    arc(ref, index).flatMap { string($0.id) }
}

@_cdecl("ak_frame_arc_front")
func ak_frame_arc_front(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> UnsafeRawPointer? {
    path(arc(ref, index)?.front)
}

@_cdecl("ak_frame_arc_back")
func ak_frame_arc_back(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> UnsafeRawPointer? {
    path(arc(ref, index)?.back)
}

@_cdecl("ak_frame_arc_width")
func ak_frame_arc_width(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> Double {
    arc(ref, index)?.width ?? 0
}

@_cdecl("ak_frame_arc_opacity")
func ak_frame_arc_opacity(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> Double {
    arc(ref, index)?.opacity ?? 0
}

@_cdecl("ak_frame_arc_gradient")
func ak_frame_arc_gradient(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> AKArcGradient {
    guard let gradient = arc(ref, index)?.grad else { return AKArcGradient(x1: 0, y1: 0, x2: 0, y2: 0) }
    return AKArcGradient(x1: gradient.x1, y1: gradient.y1, x2: gradient.x2, y2: gradient.y2)
}

@_cdecl("ak_frame_arc_stop_count")
func ak_frame_arc_stop_count(_ ref: UnsafeMutableRawPointer?, _ index: Int) -> Int {
    arc(ref, index)?.grad.stops.count ?? 0
}

@_cdecl("ak_frame_arc_copy_stop")
func ak_frame_arc_copy_stop(_ ref: UnsafeMutableRawPointer?, _ index: Int, _ stop: Int) -> UnsafeMutablePointer<CChar>? {
    guard let stops = arc(ref, index)?.grad.stops, let value = item(stops, stop) else { return nil }
    return string(value)
}
