import Foundation

private let FIT_RADIUS = 100.0
private let DERIVE_YAW = 5.5 + 1.6
private let DERIVE_PITCH = 4.2 + 1.3
private let FLOTTEMENT = hypot(0.006, 0.007) * FIT_RADIUS
private let DIRECTIONS = 12
private let DICHOTOMIE = 8

private struct Visage {
    var gaze: HeadGaze
    var split: Double
    var eyes: [EyeCfg]
}

private struct Empreinte {
    var x: Double
    var y: Double
    var ax: Double
    var ay: Double
    var r: Double
    var m: [Double]
}

private struct Epreuve {
    var empreintes: [Empreinte]
    var reference: [Empreinte]
    var contour: [Point]
    var calContour: [Point]
}

private func empreintes(_ visage: Visage, _ sil: Silhouette, _ radii: [Double]) -> [Empreinte] {
    var out: [Empreinte] = []
    let poses = eyePoses(visage.gaze, FIT_RADIUS, visage.split)
    for i in 0..<2 {
        let e = poses[i]
        if e.depth <= 0.02 { continue }
        let cfg = visage.eyes[i]
        let phi = cfg.tilt * .pi / 180
        let cp = cos(phi), sp = sin(phi)
        let ax = e.a * cp + e.c * sp, ay = e.b * cp + e.d * sp
        let cx = -e.a * sp + e.c * cp, cy = -e.b * sp + e.d * cp
        let hw = max(cfg.w * FIT_RADIUS, 0.01) / 2, hh = max(cfg.h * FIT_RADIUS, 0.01) / 2
        let r = min(hw, hh)
        let long = hh > hw
        let demi = long ? hh - r : hw - r
        let fit = radiusAtAngle(radii, atan2(e.y, e.x) - sil.rot)
        out.append(Empreinte(x: e.x * fit, y: e.y * fit, ax: (long ? cx : ax) * demi,
                             ay: (long ? cy : ay) * demi, r: r, m: [ax, ay, cx, cy]))
    }
    return out
}

private func approche(_ pts: [Point], _ x0: Double, _ y0: Double, _ x1: Double, _ y1: Double) -> (d: Double, ux: Double, uy: Double) {
    let sx = x1 - x0, sy = y1 - y0
    let len2 = sx * sx + sy * sy
    var best = Double.infinity, vx = 0.0, vy = 0.0
    for p in pts {
        let t = clamp(len2 > 0 ? ((p.x - x0) * sx + (p.y - y0) * sy) / len2 : 0)
        let ex = x0 + t * sx - p.x, ey = y0 + t * sy - p.y
        let d2 = ex * ex + ey * ey
        if d2 < best { best = d2; vx = ex; vy = ey }
    }
    let d = sqrt(best)
    return (d, d > 1e-9 ? vx / d : 0, d > 1e-9 ? vy / d : 0)
}

private func pire(_ pts: [Point], _ emps: [Empreinte], _ tx: Double, _ ty: Double) -> (marge: Double, ux: Double, uy: Double) {
    var marge = Double.infinity, ux = 0.0, uy = 0.0
    for e in emps {
        let x = e.x + tx, y = e.y + ty
        let a = approche(pts, x - e.ax, y - e.ay, x + e.ax, y + e.ay)
        let rayon = e.r * hypot(e.m[0] * a.ux + e.m[1] * a.uy, e.m[2] * a.ux + e.m[3] * a.uy) + FLOTTEMENT
        if a.d - rayon < marge { marge = a.d - rayon; ux = a.ux; uy = a.uy }
    }
    return (marge, ux, uy)
}

private func resous(_ epreuves: [Epreuve]) -> Point {
    guard let first = epreuves.first else { return Point() }
    func marge(_ tx: Double, _ ty: Double) -> Double {
        var m = Double.infinity
        for ep in epreuves { m = min(m, pire(ep.contour, ep.empreintes, tx, ty).marge) }
        return m
    }
    var requis = Double.infinity
    for ep in epreuves { requis = min(requis, pire(ep.calContour, ep.reference, 0, 0).marge) }
    var mx = 0.0, my = 0.0
    for e in first.empreintes { mx -= e.x / Double(first.empreintes.count); my -= e.y / Double(first.empreintes.count) }
    let course = max(0.35 * FIT_RADIUS, hypot(mx, my) * 1.25)
    requis = min(requis, marge(mx, my))
    let depart = marge(0, 0)
    if depart >= requis && depart >= 0 { return Point() }
    let cible = max(requis, 0)
    var meilleurX = 0.0, meilleurY = 0.0, meilleureNorme = Double.infinity
    var secoursX = 0.0, secoursY = 0.0, secours = depart
    for d in 0..<DIRECTIONS {
        let a = Double(d) / Double(DIRECTIONS) * TAU
        let ux = cos(a), uy = sin(a)
        if marge(ux * course, uy * course) < cible {
            for k in [0.3, 0.6, 1.0] {
                let m = marge(ux * course * k, uy * course * k)
                if m > secours { secours = m; secoursX = ux * course * k; secoursY = uy * course * k }
            }
            continue
        }
        var bas = 0.0, haut = course
        for _ in 0..<DICHOTOMIE {
            let mid = (bas + haut) / 2
            if marge(ux * mid, uy * mid) >= cible { haut = mid } else { bas = mid }
        }
        if haut < meilleureNorme { meilleureNorme = haut; meilleurX = ux * haut; meilleurY = uy * haut }
    }
    let x = meilleureNorme.isInfinite ? secoursX : meilleurX
    let y = meilleureNorme.isInfinite ? secoursY : meilleurY
    return Point(x: (x / FIT_RADIUS * 1e6).rounded() / 1e6, y: (y / FIT_RADIUS * 1e6).rounded() / 1e6)
}

private func visageDe(_ def: StateDef, _ pose: Pose, _ expr: BotExpression?) -> Visage {
    if def.baseFace, let expr { return Visage(gaze: expr.gaze, split: expr.split, eyes: expr.eyes) }
    return Visage(gaze: pose.gaze, split: pose.split, eyes: pose.eyes)
}

private func dates(_ def: StateDef) -> [Double] {
    let a = def.pose(0), b = def.pose(def.duration)
    if a.gaze == b.gaze && a.split == b.split && a.eyes == b.eyes && a.sil.rot == b.sil.rot
        && a.sil.cx == b.sil.cx && a.sil.cy == b.sil.cy && a.sil.sx == b.sil.sx && a.sil.sy == b.sil.sy { return [0] }
    return [0, def.duration / 2, def.duration]
}

private func decalagePour(_ def: StateDef, _ radii: [Double], _ expr: BotExpression?) -> Point {
    var epreuves: [Epreuve] = []
    for t in dates(def) {
        let pose = def.pose(t)
        var sil = pose.sil
        sil.radii = radii
        let contour = toPoints(sil, FIT_RADIUS), calContour = toPoints(pose.sil, FIT_RADIUS)
        let v = visageDe(def, pose, expr)
        for dy in [-DERIVE_YAW, DERIVE_YAW] {
            for dp in [-DERIVE_PITCH, DERIVE_PITCH] {
                var corner = v
                corner.gaze = HeadGaze(yaw: v.gaze.yaw + dy, pitch: v.gaze.pitch + dp, roll: v.gaze.roll)
                epreuves.append(Epreuve(empreintes: empreintes(corner, pose.sil, radii),
                                         reference: empreintes(corner, pose.sil, pose.sil.radii),
                                         contour: contour, calContour: calContour))
            }
        }
    }
    return resous(epreuves)
}

private func clef(_ state: StateID, _ expr: String?) -> String { state.rawValue + "|" + (expr ?? "") }

func buildEyeFits() -> [ShapeID: [String: Point]] {
    var table: [ShapeID: [String: Point]] = [:]
    for shape in SHAPES {
        var entries: [String: Point] = [:]
        for def in STATES where def.baseBody {
            let expressions: [BotExpression?] = def.baseFace ? [nil] + EXPRESSIONS.map { Optional($0) } : [nil]
            for expr in expressions { entries[clef(def.id, expr?.id.rawValue)] = decalagePour(def, shape.radii, expr) }
        }
        table[shape.id] = entries
    }
    return table
}

// Solve once; animation only interpolates constants from this table.
private let DECALAGES = buildEyeFits()

public func decalageDesYeux(_ radii: [Double]?, _ state: StateID, _ expr: String?) -> Point {
    guard let radii, let shape = SHAPES.first(where: { $0.radii == radii }), let entries = DECALAGES[shape.id] else { return Point() }
    return entries[clef(state, expr)] ?? entries[clef(state, nil)] ?? Point()
}
